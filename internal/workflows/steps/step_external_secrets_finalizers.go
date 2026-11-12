// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
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
	// whatever happens here, and removing the record lets
	// install --recover-partial-uninstall recover the cluster.
	if notDeployed && allDeleting {
		logx.As().Warn().Strs("objects", all).Str("status", string(releaseStatus(rel))).
			Msg("Uninstalling a stalled External Secrets Operator release whose objects are already being deleted; recover with eso operator install --recover-partial-uninstall")
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
	if kinds[pushSecretKind] {
		// The ClusterPushSecret controller rewrites its PushSecrets' spec and
		// recreates deleted ones.
		hints = append(hints,
			"A PushSecret created by a ClusterPushSecret comes back from it: change or delete that ClusterPushSecret instead, listed by:",
			"  kubectl get clusterpushsecrets.external-secrets.io")
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
