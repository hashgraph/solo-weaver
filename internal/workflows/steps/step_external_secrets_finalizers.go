// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	FinishExternalSecretsRecoveryStepId = "finish-external-secrets-recovery"

	esoExternalSecretCRD = "externalsecrets.external-secrets.io"
	esoRecoveryDoc       = `docs/commands/alloy.md, "Recover from a partial uninstall"`

	// esoMaxListed caps the objects a refusal names; the log has them all.
	esoMaxListed = 10

	esoCRDPollInterval = 2 * time.Second
)

// esoFinalizedKind is an ESO kind whose objects carry a finalizer that only the
// ESO controller clears.
type esoFinalizedKind struct {
	group, kind, resource string
}

func (k esoFinalizedKind) groupKind() schema.GroupKind {
	return schema.GroupKind{Group: k.group, Kind: k.kind}
}

// kubectlName is the resource.group form kubectl accepts for any served version.
func (k esoFinalizedKind) kubectlName() string {
	return k.resource + "." + k.group
}

var (
	externalSecretKind        = esoFinalizedKind{"external-secrets.io", "ExternalSecret", "externalsecrets"}
	clusterExternalSecretKind = esoFinalizedKind{"external-secrets.io", "ClusterExternalSecret", "clusterexternalsecrets"}
	pushSecretKind            = esoFinalizedKind{"external-secrets.io", "PushSecret", "pushsecrets"}
	generatorStateKind        = esoFinalizedKind{"generators.external-secrets.io", "GeneratorState", "generatorstates"}
)

// esoFinalizedKinds are the kinds whose objects carry a finalizer that only the
// ESO controller clears: always for ExternalSecrets, ClusterExternalSecrets and
// GeneratorStates, and for PushSecrets with deletionPolicy Delete. The chart
// deletes the controller and the CRDs in one pass, so uninstalling while such
// objects exist leaves a partial uninstall: the release record purged and a CRD
// stuck Terminating. SecretStores and ClusterSecretStores are not listed: they
// carry one only while a PushSecret with deletionPolicy Delete references them,
// and that PushSecret is listed.
var esoFinalizedKinds = []esoFinalizedKind{externalSecretKind, clusterExternalSecretKind, pushSecretKind, generatorStateKind}

// esoClusterReader is the part of the kube client the ESO steps read through.
type esoClusterReader interface {
	CRDState(ctx context.Context, crdName string) (exists, terminating bool, err error)
	ListResources(ctx context.Context, gk schema.GroupKind, namespace string) ([]unstructured.Unstructured, error)
	GetResourceNestedMap(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (map[string]interface{}, bool, error)
}

// esoClusterReaderProvider resolves an esoClusterReader at step-execution time.
// Tests inject a fake.
type esoClusterReaderProvider func(ctx context.Context) (esoClusterReader, error)

func defaultESOClusterReader(context.Context) (esoClusterReader, error) {
	k, err := kube.NewClient()
	if err != nil {
		return nil, err
	}
	return k, nil
}

// resolveESOClusterReader returns the reader, or the error to report when the
// kube client cannot be built.
func resolveESOClusterReader(ctx context.Context, provider esoClusterReaderProvider) (esoClusterReader, error) {
	k, err := provider(ctx)
	if err != nil {
		return nil, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to initialise Kubernetes client"),
			reasons.K8sAPIFailed,
			"Verify the cluster is reachable: kubectl cluster-info",
		)
	}
	return k, nil
}

// esoObject is one finalized ESO object, for a refusal message.
type esoObject struct {
	kind      esoFinalizedKind
	namespace string
	name      string
	deleting  bool
}

func (o esoObject) String() string {
	ref := o.name
	if o.namespace != "" {
		ref = o.namespace + "/" + o.name
	}
	if o.deleting {
		return fmt.Sprintf("%s (%s, deleting)", ref, o.kind.kind)
	}
	return fmt.Sprintf("%s (%s)", ref, o.kind.kind)
}

// listFinalizedESOObjects lists the objects of esoFinalizedKinds in the cluster
// that hold a finalizer; one without a finalizer does not block a CRD deletion.
func listFinalizedESOObjects(ctx context.Context, k esoClusterReader) ([]esoObject, error) {
	var objs []esoObject
	for _, kd := range esoFinalizedKinds {
		items, err := k.ListResources(ctx, kd.groupKind(), "")
		if err != nil {
			return nil, errx.Decorate(
				errorx.ExternalError.Wrap(err, "failed to list External Secrets Operator %s objects", kd.kind),
				reasons.K8sAPIFailed,
				"Verify the cluster is reachable: kubectl cluster-info",
				"List them yourself: kubectl get "+kd.kubectlName()+" -A",
			)
		}
		for _, item := range items {
			if len(item.GetFinalizers()) == 0 {
				continue
			}
			objs = append(objs, esoObject{
				kind:      kd,
				namespace: item.GetNamespace(),
				name:      item.GetName(),
				deleting:  item.GetDeletionTimestamp() != nil,
			})
		}
	}
	return objs, nil
}

// summarizeESOObjects names up to esoMaxListed objects and counts the rest.
func summarizeESOObjects(objs []esoObject) string {
	names := make([]string, 0, len(objs))
	for _, o := range objs {
		names = append(names, o.String())
	}
	return strings.Join(capList(names, ""), ", ")
}

// checkNoFinalizedESOObjects refuses an uninstall while finalized ESO objects
// exist, before anything is deleted. Nothing bypasses it: going ahead can only
// end in a partial uninstall.
func checkNoFinalizedESOObjects(ctx context.Context, k esoClusterReader, rel *release.Release, spec *helmChartSpec) error {
	objs, err := listFinalizedESOObjects(ctx, k)
	if err != nil {
		return err
	}
	if len(objs) == 0 {
		return nil
	}

	all := make([]string, 0, len(objs))
	notDeployed := releaseStatus(rel) != release.StatusDeployed
	stuck := notDeployed
	allDeleting := true
	kinds := map[esoFinalizedKind]bool{}
	for _, o := range objs {
		all = append(all, o.String())
		stuck = stuck || o.deleting
		allDeleting = allDeleting && o.deleting
		kinds[o.kind] = true
	}

	// A release left failed, pending or uninstalling with every object already
	// deleting is a partial uninstall in all but its record: the objects go
	// whatever happens here, and removing the record lets install --force
	// recover the cluster.
	if notDeployed && allDeleting {
		logx.As().Warn().Strs("objects", all).Str("status", string(releaseStatus(rel))).
			Msg("Uninstalling a stalled External Secrets Operator release whose objects are already being deleted; recover with eso operator install --force")
		return nil
	}

	logx.As().Warn().Strs("objects", all).
		Msg("External Secrets Operator objects still hold finalizers, refusing to uninstall")

	return errx.Decorate(
		errorx.IllegalState.New(
			"%d External Secrets Operator %s finalizers that only its controller clears: %s. "+
				"Uninstalling now would delete the controller and the CRDs together and leave a partial uninstall, so nothing was changed. "+
				"SecretStores and ClusterSecretStores are deleted with the CRDs when the uninstall runs",
			len(objs), plural(len(objs), "object still holds", "objects still hold"), summarizeESOObjects(objs)),
		reasons.PreconditionNotMet,
		finalizedObjectHints(kinds, stuck, spec)...,
	)
}

// finalizedObjectHints tells the operator how to clear the objects, or where to
// look when the controller is not clearing them.
func finalizedObjectHints(kinds map[esoFinalizedKind]bool, stuck bool, spec *helmChartSpec) []string {
	if stuck {
		return []string{
			"Objects already being deleted wait for the External Secrets Operator controller; check it is running: kubectl get pods -n " + spec.Namespace,
			"If it is not running, there is no CLI repair for this release yet: do not uninstall over it; see " + esoRecoveryDoc,
			"Do not remove finalizers or delete the External Secrets Operator CRDs by hand",
		}
	}

	var names []string
	for _, kd := range esoFinalizedKinds {
		if kinds[kd] {
			names = append(names, kd.kubectlName())
		}
	}
	hints := []string{"See what still uses them: kubectl get " + strings.Join(names, ",") + " -A"}
	if kinds[externalSecretKind] {
		hints = append(hints,
			"Delete each ExternalSecret you no longer need, which also deletes the Secret it synced:",
			"  sudo solo-provisioner eso secret delete --name <name> --namespace <namespace>")
	}
	if len(kinds) > 1 || !kinds[externalSecretKind] {
		hints = append(hints,
			"Delete the other objects; a PushSecret with deletionPolicy Delete also deletes its pushed secret in the remote store:",
			"  kubectl delete <resource.group> <name> [-n <namespace>]")
	}
	return append(hints,
		"When none are left, uninstall again:",
		fmt.Sprintf("  sudo solo-provisioner eso operator uninstall --namespace %s", spec.Namespace))
}

// decorateESOUninstallFailure describes a failed helm uninstall by what it left
// behind. A purged release with a terminating ExternalSecret CRD is a partial
// uninstall, where the Helm hints would mislead.
func decorateESOUninstallFailure(ctx context.Context, hm helm.Manager, k esoClusterReader, rel *release.Release, spec *helmChartSpec, err error) error {
	if leftPartialUninstall(ctx, hm, k, spec) {
		return errx.Decorate(
			errorx.ExternalError.Wrap(err,
				"failed to uninstall External Secrets Operator release %q in namespace %q: Helm removed the release record, but the ExternalSecret CRD is still being deleted, so this is a partial uninstall",
				rel.Name, spec.Namespace),
			reasons.PreconditionNotMet,
			"Do not remove finalizers or delete the External Secrets Operator CRDs by hand",
			"Recover with "+esoRecoveryDoc,
		)
	}
	return errx.Decorate(
		errorx.ExternalError.Wrap(err,
			"failed to uninstall External Secrets Operator release %q in namespace %q",
			rel.Name, spec.Namespace),
		reasons.PreconditionNotMet,
		"Check the release state:",
		// -a: plain list hides the stalled states this step removes.
		fmt.Sprintf("  helm list -a -n %s", spec.Namespace),
		"Remove it manually if needed:",
		fmt.Sprintf("  helm uninstall %s -n %s", rel.Name, spec.Namespace),
	)
}

// leftPartialUninstall re-reads the cluster after a failed uninstall. A read
// that fails counts as not partial, so the generic hints are used.
func leftPartialUninstall(ctx context.Context, hm helm.Manager, k esoClusterReader, spec *helmChartSpec) bool {
	rel, err := resolveESORelease(hm, spec)
	if err != nil || rel != nil {
		return false
	}
	terminating, err := terminatingESOCRDs(ctx, k)
	return err == nil && len(terminating) > 0
}

// terminatingESOCRDs lists the ESO CRDs that are being deleted.
func terminatingESOCRDs(ctx context.Context, k esoClusterReader) ([]string, error) {
	var names []string
	for _, name := range esoCRDs {
		_, terminating, err := k.CRDState(ctx, name)
		if err != nil {
			return nil, err
		}
		if terminating {
			names = append(names, name)
		}
	}
	return names, nil
}

// checkPartialUninstallRecovery decides an install over a partial uninstall:
// every ESO CRD found is terminating and no ESO release exists. Only --force
// goes ahead, and only once no Secret is still owned by an ExternalSecret, since
// a reinstalled controller would let Kubernetes delete those Secrets.
func checkPartialUninstallRecovery(ctx context.Context, k esoClusterReader, spec *helmChartSpec, terminating []string, force bool) (warning string, err error) {
	if !force {
		return "", errx.Decorate(
			errorx.IllegalState.New(
				"a previous External Secrets Operator uninstall stopped partway: no Helm release exists, and %s still being deleted, waiting on finalizers only its controller clears; nothing was changed",
				crdsPhrase(terminating)),
			reasons.PreconditionNotMet,
			"Do not remove finalizers or delete the External Secrets Operator CRDs by hand",
			"Follow "+esoRecoveryDoc+": detach the Secrets to keep, set PushSecrets to deletionPolicy None, then reinstall:",
			fmt.Sprintf("  sudo solo-provisioner eso operator install --force --namespace %s", spec.Namespace),
		)
	}

	return checkRecoveryKeepsData(ctx, k, terminating)
}

// checkRecoveryKeepsData lets a forced recovery go ahead only when the
// controller it brings back destroys no data, and warns about GeneratorStates.
func checkRecoveryKeepsData(ctx context.Context, k esoClusterReader, terminating []string) (warning string, err error) {
	owned, err := ownedESOSecrets(ctx, k)
	if err != nil {
		return "", err
	}
	pushRefs, pushPatches, err := deletingPushSecrets(ctx, k)
	if err != nil {
		return "", err
	}
	if len(owned) > 0 || len(pushRefs) > 0 {
		return "", recoveryBlockedError(owned, pushRefs, pushPatches)
	}

	logx.As().Warn().Strs("crds", terminating).
		Msg("Recovering External Secrets Operator from a partial uninstall (--force)")
	return generatorStateWarning(ctx, k)
}

// esoCRDGaps reports the esoCRDs being deleted and the esoRequiredCRDs missing.
func esoCRDGaps(ctx context.Context, k esoClusterReader) (terminating, missing []string, err error) {
	required := map[string]bool{}
	for _, name := range esoRequiredCRDs {
		required[name] = true
	}
	for _, name := range esoCRDs {
		exists, term, err := k.CRDState(ctx, name)
		if err != nil {
			return nil, nil, errx.Decorate(
				errorx.ExternalError.Wrap(err, "failed to check CRD %s", name),
				reasons.K8sAPIFailed,
				"Verify the cluster is reachable: kubectl cluster-info",
				"Check the CRD: kubectl get crd "+name,
			)
		}
		if term {
			terminating = append(terminating, name)
		}
		if !exists && required[name] {
			missing = append(missing, name)
		}
	}
	return terminating, missing, nil
}

// checkInstalledESOCRDs checks that an existing ESO release still has its CRDs.
// A recovery that failed or was interrupted leaves a release whose old CRDs are
// still deleting, or already gone; installing over it would only time out.
func checkInstalledESOCRDs(ctx context.Context, k esoClusterReader, spec *helmChartSpec, force bool) (warning string, err error) {
	terminating, missing, err := esoCRDGaps(ctx, k)
	if err != nil {
		return "", err
	}

	if len(terminating) > 0 {
		if force {
			return checkRecoveryKeepsData(ctx, k, terminating)
		}
		return "", errx.Decorate(
			errorx.IllegalState.New(
				"External Secrets Operator release %q is installed, but %s still being deleted: a recovery from a partial uninstall has not finished; nothing was changed",
				spec.Release, crdsPhrase(terminating)),
			reasons.PreconditionNotMet,
			"Finish the recovery:",
			fmt.Sprintf("  sudo solo-provisioner eso operator install --force --namespace %s", spec.Namespace),
			"See "+esoRecoveryDoc,
		)
	}

	if len(missing) > 0 {
		return "", errx.Decorate(
			errorx.IllegalState.New(
				"External Secrets Operator release %q is installed, but %s missing, so it cannot sync secrets; nothing was changed",
				spec.Release, plural(len(missing), "CRD ", "CRDs ")+strings.Join(missing, ", ")+plural(len(missing), " is", " are")),
			reasons.PreconditionNotMet,
			"Reinstall to restore every CRD:",
			fmt.Sprintf("  sudo solo-provisioner eso operator uninstall --namespace %s", spec.Namespace),
			fmt.Sprintf("  sudo solo-provisioner eso operator install --namespace %s", spec.Namespace),
			"Then recreate the stores and ExternalSecrets: see "+esoRecoveryDoc,
		)
	}
	return "", nil
}

// recoveryBlockedError refuses a forced recovery while the reinstalled
// controller would destroy data: Secrets still owned by an ExternalSecret,
// which Kubernetes deletes once the ExternalSecret goes, and PushSecrets with
// deletionPolicy Delete, which delete their pushed secret in the remote store.
func recoveryBlockedError(owned, pushRefs, pushPatches []string) error {
	var facts, hints []string
	if len(owned) > 0 {
		logx.As().Warn().Strs("secrets", owned).
			Msg("Secrets are still owned by ExternalSecrets, refusing to reinstall over a partial uninstall")
		facts = append(facts, plural(len(owned), "Secret ", "Secrets ")+strings.Join(capList(owned, ""), ", ")+
			plural(len(owned), " is", " are")+" still owned by an ExternalSecret, and Kubernetes deletes "+
			plural(len(owned), "it", "them")+" once the controller finishes deleting that ExternalSecret")
		patches := make([]string, 0, len(owned))
		for _, ref := range owned {
			ns, name, _ := strings.Cut(ref, "/")
			patches = append(patches, fmt.Sprintf(
				`  kubectl patch secret %s -n %s --type=json -p '[{"op":"remove","path":"/metadata/ownerReferences"}]'`, name, ns))
		}
		hints = append(hints, "Detach each Secret you need to keep:")
		hints = append(hints, capList(patches, "  ")...)
	}
	if len(pushRefs) > 0 {
		logx.As().Warn().Strs("pushSecrets", pushRefs).
			Msg("PushSecrets would delete their remote secrets, refusing to reinstall over a partial uninstall")
		facts = append(facts, plural(len(pushRefs), "PushSecret ", "PushSecrets ")+strings.Join(capList(pushRefs, ""), ", ")+
			plural(len(pushRefs), " has", " have")+" deletionPolicy Delete, and the controller deletes each pushed secret in its remote store as it finishes deleting "+
			plural(len(pushRefs), "it", "them"))
		hints = append(hints, "Keep each remote secret by setting its deletionPolicy to None:")
		hints = append(hints, capList(pushPatches, "  ")...)
	}
	hints = append(hints, "Then run the install again; see "+esoRecoveryDoc)

	return errx.Decorate(
		errorx.IllegalState.New("reinstalling External Secrets Operator would destroy data: %s; nothing was changed",
			strings.Join(facts, "; ")),
		reasons.PreconditionNotMet,
		hints...,
	)
}

// deletingPushSecrets lists the PushSecrets with deletionPolicy Delete, as
// namespace/name, with the patch that sets each to None. With None the
// controller drops the finalizer without touching the remote store.
func deletingPushSecrets(ctx context.Context, k esoClusterReader) (refs, patches []string, err error) {
	items, err := k.ListResources(ctx, pushSecretKind.groupKind(), "")
	if err != nil {
		return nil, nil, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list PushSecrets"),
			reasons.K8sAPIFailed,
			"Verify the cluster is reachable: kubectl cluster-info",
			"List them yourself: kubectl get "+pushSecretKind.kubectlName()+" -A",
		)
	}
	for _, ps := range items {
		policy, _, _ := unstructured.NestedString(ps.Object, "spec", "deletionPolicy")
		if policy != "Delete" {
			continue
		}
		refs = append(refs, ps.GetNamespace()+"/"+ps.GetName())
		patches = append(patches, fmt.Sprintf(
			`  kubectl patch %s %s -n %s --type=merge -p '{"spec":{"deletionPolicy":"None"}}'`,
			pushSecretKind.kubectlName(), ps.GetName(), ps.GetNamespace()))
	}
	return refs, patches, nil
}

// generatorStateWarning names the GeneratorStates a reinstall will clean up.
// They do not block: ESO has no setting that keeps what their generators made.
func generatorStateWarning(ctx context.Context, k esoClusterReader) (string, error) {
	items, err := k.ListResources(ctx, generatorStateKind.groupKind(), "")
	if err != nil {
		return "", errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list GeneratorStates"),
			reasons.K8sAPIFailed,
			"Verify the cluster is reachable: kubectl cluster-info",
			"List them yourself: kubectl get "+generatorStateKind.kubectlName()+" -A",
		)
	}
	if len(items) == 0 {
		return "", nil
	}

	refs := make([]string, 0, len(items))
	for _, gs := range items {
		refs = append(refs, gs.GetNamespace()+"/"+gs.GetName())
	}
	logx.As().Warn().Strs("generatorStates", refs).
		Msg("The reinstalled External Secrets Operator will clean up what these generators created")
	return fmt.Sprintf(
		"The reinstalled External Secrets Operator controller will finish deleting GeneratorStates %s, which cleans up what their generators created",
		strings.Join(capList(refs, ""), ", ")), nil
}

// checkRollbackLeavesNoFinalizedObjects refuses an install rollback that would
// leave a partial uninstall, as checkNoFinalizedESOObjects does for the
// uninstall command.
func checkRollbackLeavesNoFinalizedObjects(ctx context.Context, k esoClusterReader, spec *helmChartSpec) error {
	objs, err := listFinalizedESOObjects(ctx, k)
	if err != nil {
		return err
	}
	if len(objs) == 0 {
		return nil
	}
	return errx.Decorate(
		errorx.IllegalState.New(
			"rollback left release %q in namespace %q installed: %d External Secrets Operator objects still hold finalizers (%s), and removing the controller now would leave a partial uninstall",
			spec.Release, spec.Namespace, len(objs), summarizeESOObjects(objs)),
		reasons.PreconditionNotMet,
		"Check the controller is running: kubectl get pods -n "+spec.Namespace,
		"See "+esoRecoveryDoc,
	)
}

// capList keeps esoMaxListed entries and replaces the rest with one
// prefix-indented "and N more" entry; the log carries the full list.
func capList(items []string, prefix string) []string {
	if len(items) <= esoMaxListed {
		return items
	}
	out := append([]string{}, items[:esoMaxListed]...)
	return append(out, fmt.Sprintf("%sand %d more (full list in the log)", prefix, len(items)-esoMaxListed))
}

// plural picks one or many by n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// crdsPhrase is "CRD a is" or "CRDs a, b are".
func crdsPhrase(names []string) string {
	return plural(len(names), "CRD ", "CRDs ") + strings.Join(names, ", ") + plural(len(names), " is", " are")
}

// ownedESOSecrets lists, as namespace/name, the target Secrets of the
// ExternalSecrets that still have an ExternalSecret owner reference.
func ownedESOSecrets(ctx context.Context, k esoClusterReader) ([]string, error) {
	externalSecrets, err := k.ListResources(ctx, externalSecretKind.groupKind(), "")
	if err != nil {
		return nil, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list ExternalSecrets"),
			reasons.K8sAPIFailed,
			"Verify the cluster is reachable: kubectl cluster-info",
			"List them yourself: kubectl get "+externalSecretKind.kubectlName()+" -A",
		)
	}

	var owned []string
	for _, es := range externalSecrets {
		target, _, _ := unstructured.NestedString(es.Object, "spec", "target", "name")
		if target == "" {
			target = es.GetName()
		}
		metadata, found, err := k.GetResourceNestedMap(ctx, "v1", "Secret", es.GetNamespace(), target, "metadata")
		if err != nil {
			return nil, errx.Decorate(
				errorx.ExternalError.Wrap(err, "failed to read Secret %s/%s", es.GetNamespace(), target),
				reasons.K8sAPIFailed,
				fmt.Sprintf("Check the Secret: kubectl get secret %s -n %s -o yaml", target, es.GetNamespace()),
			)
		}
		if found && ownedByExternalSecret(metadata) {
			owned = append(owned, es.GetNamespace()+"/"+target)
		}
	}
	return owned, nil
}

// ownedByExternalSecret reports whether a metadata map has an ExternalSecret
// owner reference.
func ownedByExternalSecret(metadata map[string]interface{}) bool {
	refs, _ := metadata["ownerReferences"].([]interface{})
	for _, r := range refs {
		ref, _ := r.(map[string]interface{})
		apiVersion, _ := ref["apiVersion"].(string)
		kind, _ := ref["kind"].(string)
		if kind == externalSecretKind.kind && strings.HasPrefix(apiVersion, externalSecretKind.group+"/") {
			return true
		}
	}
	return false
}

// waitESOCRDsGone polls until none of the CRDs exists. A failed read is retried
// until the timeout, which then wraps the last read error. A cancelled ctx
// returns an Interrupted error.
func waitESOCRDsGone(ctx context.Context, k esoClusterReader, crdNames []string, timeout, interval time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	names := strings.Join(crdNames, ", ")
	for {
		left := false
		var lastErr error
		for _, name := range crdNames {
			exists, _, err := k.CRDState(waitCtx, name)
			if err != nil {
				lastErr = err
			}
			left = left || err != nil || exists
		}
		if !left {
			return nil
		}
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return errorx.Interrupted.Wrap(ctx.Err(), "stopped waiting for CRDs %s to finish deleting", names)
			}
			if lastErr != nil {
				return errorx.TimeoutElapsed.Wrap(lastErr, "could not confirm CRDs %s finished deleting within %s", names, timeout)
			}
			return errorx.TimeoutElapsed.New("CRDs %s were still being deleted after %s", names, timeout)
		case <-time.After(interval):
		}
	}
}

// finishESORecovery ends a forced install. After a partial uninstall the
// reinstalled controller drains the old ExternalSecrets, and the old
// ExternalSecret CRD then finishes deleting, taking the release's copy with it.
// The step waits for that and fails with the next step, since ESO cannot sync
// secrets until a reinstall restores the CRD.
func finishESORecovery(spec *helmChartSpec, provider esoClusterReaderProvider, timeout time.Duration) automa.Builder {
	return automa.NewStepBuilder().WithId(FinishExternalSecretsRecoveryStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, err := resolveESOClusterReader(ctx, provider)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := finishPartialUninstall(ctx, k, spec, timeout, esoCRDPollInterval); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking for an unfinished partial uninstall")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "External Secrets Operator needs a reinstall to finish recovery")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "No partial uninstall left to finish")
		})
}

// finishPartialUninstall returns nil when no ESO CRD is deleting and every
// required one is present; otherwise it waits for the deleting ones to go and
// returns the next step.
func finishPartialUninstall(ctx context.Context, k esoClusterReader, spec *helmChartSpec, timeout, interval time.Duration) error {
	terminating, missingRequired, err := esoCRDGaps(ctx, k)
	if err != nil {
		return err
	}
	missing := len(missingRequired) > 0
	if len(terminating) == 0 && !missing {
		return nil
	}

	if len(terminating) > 0 {
		if err := waitESOCRDsGone(ctx, k, terminating, timeout, interval); err != nil {
			if errorx.IsOfType(err, errorx.Interrupted) {
				return errx.Decorate(err, reasons.PreconditionNotMet,
					"Run the install again to finish the recovery:",
					fmt.Sprintf("  sudo solo-provisioner eso operator install --force --namespace %s", spec.Namespace))
			}
			return errx.Decorate(
				errorx.IllegalState.Wrap(err, "the reinstalled External Secrets Operator controller has not cleared the old objects' finalizers"),
				reasons.PreconditionNotMet,
				"Check the controller is running: kubectl get pods -n "+spec.Namespace,
				"Check what is left: kubectl get "+externalSecretKind.kubectlName()+" -A",
				"Then run the install again:",
				fmt.Sprintf("  sudo solo-provisioner eso operator install --force --namespace %s", spec.Namespace),
			)
		}
	}

	return errx.Decorate(
		errorx.IllegalState.New(
			"the partial uninstall has finished: the old External Secrets Operator CRDs are gone, so release %q lacks them and External Secrets Operator cannot sync secrets yet",
			spec.Release),
		reasons.PreconditionNotMet,
		"Reinstall to restore every CRD:",
		fmt.Sprintf("  sudo solo-provisioner eso operator uninstall --namespace %s", spec.Namespace),
		fmt.Sprintf("  sudo solo-provisioner eso operator install --namespace %s", spec.Namespace),
		"Then recreate the stores and ExternalSecrets: see "+esoRecoveryDoc,
	)
}

// uninstallSkipMetadata is the skipped uninstall report's metadata: the partial
// uninstall warning when there is one, so the run summary shows it.
func uninstallSkipMetadata(ctx context.Context, k esoClusterReader) map[string]string {
	warning := partialUninstallWarning(ctx, k)
	if warning == "" {
		return nil
	}
	logx.As().Warn().Msg(warning)
	return map[string]string{models.ReportMetaWarning: warning}
}

// partialUninstallWarning returns the operator warning for an uninstall that
// finds no ESO release while ESO CRDs are still being deleted, or
// "" when that is not the case or cannot be read.
func partialUninstallWarning(ctx context.Context, k esoClusterReader) string {
	terminating, err := terminatingESOCRDs(ctx, k)
	if err != nil {
		logx.As().Debug().Err(err).Msg("Could not check the External Secrets Operator CRDs for a partial uninstall")
		return ""
	}
	if len(terminating) == 0 {
		return ""
	}
	return "External Secrets Operator has no Helm release, but " + crdsPhrase(terminating) +
		" still being deleted: a previous uninstall stopped partway. See " + esoRecoveryDoc
}

// isESORecovery reports whether a forced install is recovering from a partial
// uninstall: the precheck has let it through, and ESO CRDs are still deleting.
func isESORecovery(ctx context.Context, force bool, provider esoClusterReaderProvider) (bool, error) {
	if !force {
		return false, nil
	}
	k, err := resolveESOClusterReader(ctx, provider)
	if err != nil {
		return false, err
	}
	terminating, err := terminatingESOCRDs(ctx, k)
	if err != nil {
		return false, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to check the External Secrets Operator CRDs"),
			reasons.K8sAPIFailed,
			"Verify the cluster is reachable: kubectl cluster-info",
			"Check the CRDs: kubectl get crd | grep external-secrets",
		)
	}
	return len(terminating) > 0, nil
}
