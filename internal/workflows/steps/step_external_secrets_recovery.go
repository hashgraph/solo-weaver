// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/reality"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// checkPartialUninstallRecovery decides an install over a partial uninstall:
// every ESO CRD found is terminating and no ESO release exists. Only
// --recover-partial-uninstall goes ahead, and only once no Secret is still
// owned by an ExternalSecret, since a reinstalled controller would let
// Kubernetes delete those Secrets.
func checkPartialUninstallRecovery(ctx context.Context, k esoClusterReader, spec *helmChartSpec, terminating []string, recoverPartial bool) (warning string, err error) {
	if !recoverPartial {
		return "", recoveryNeededError(ctx, k, spec, fmt.Sprintf(
			"a previous External Secrets Operator uninstall stopped partway: no Helm release exists, and %s still being deleted, waiting on finalizers only its controller clears",
			crdsPhrase(terminating)))
	}

	return checkRecoveryKeepsData(ctx, k, terminating)
}

// esoRecoveryData is what a recovery would destroy or clean up, read from the
// cluster: the Secrets still owned by an ExternalSecret and the PushSecrets
// with deletionPolicy Delete, each with the patch that keeps it, and the
// GeneratorStates.
func esoRecoveryData(ctx context.Context, k esoClusterReader) (owned, ownedPatches, pushRefs, pushPatches, generatorStates []string, err error) {
	if owned, ownedPatches, err = ownedESOSecrets(ctx, k); err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if pushRefs, pushPatches, err = deletingPushSecrets(ctx, k); err != nil {
		return nil, nil, nil, nil, nil, err
	}
	if generatorStates, err = listGeneratorStates(ctx, k); err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return owned, ownedPatches, pushRefs, pushPatches, generatorStates, nil
}

// recoveryNeededError refuses an install that needs --recover-partial-uninstall,
// and shows what that recovery would refuse over and what it would clean up, so
// the operator sees it before choosing the flag.
func recoveryNeededError(ctx context.Context, k esoClusterReader, spec *helmChartSpec, state string) error {
	recoverHints := []string{
		"Then recover; see " + esoRecoveryDoc + ":",
		fmt.Sprintf("  sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace %s", spec.Namespace),
	}

	owned, ownedPatches, pushRefs, pushPatches, generatorStates, err := esoRecoveryData(ctx, k)
	if err != nil {
		// The state is the diagnosis; the failed read only hides the details.
		return errx.Decorate(
			errorx.IllegalState.Wrap(err, "%s; nothing was changed, and listing what a recovery would keep or clean up failed", state),
			reasons.PreconditionNotMet,
			append([]string{
				"Do not remove finalizers or delete the External Secrets Operator CRDs by hand",
				"List them yourself: kubectl get " + externalSecretKind.kubectlName() + "," + pushSecretKind.kubectlName() + "," + generatorStateKind.kubectlName() + " -A",
			}, recoverHints...)...,
		)
	}
	facts, keepHints := recoveryBlockers(owned, ownedPatches, pushRefs, pushPatches)
	if len(generatorStates) > 0 {
		facts = append(facts, generatorStateFact(generatorStates))
	}

	msg := state
	if len(facts) > 0 {
		msg += ". " + strings.Join(facts, "; ")
	}
	hints := []string{"Do not remove finalizers or delete the External Secrets Operator CRDs by hand"}
	hints = append(hints, keepHints...)
	hints = append(hints, recoverHints...)

	return errx.Decorate(
		errorx.IllegalState.New("%s; nothing was changed", msg),
		reasons.PreconditionNotMet,
		hints...,
	)
}

// checkRecoveryKeepsData lets a recovery go ahead only when the controller it
// brings back destroys no data, and warns about GeneratorStates.
func checkRecoveryKeepsData(ctx context.Context, k esoClusterReader, terminating []string) (warning string, err error) {
	owned, ownedPatches, pushRefs, pushPatches, generatorStates, err := esoRecoveryData(ctx, k)
	if err != nil {
		return "", err
	}
	if len(owned) > 0 || len(pushRefs) > 0 {
		return "", recoveryBlockedError(owned, ownedPatches, pushRefs, pushPatches)
	}

	logx.As().Warn().Strs("crds", terminating).
		Msg("Recovering External Secrets Operator from a partial uninstall (--recover-partial-uninstall)")
	if len(generatorStates) == 0 {
		return "", nil
	}
	logx.As().Warn().Strs("generatorStates", generatorStates).
		Msg("The reinstalled External Secrets Operator will clean up what these generators created")
	return generatorStateFact(generatorStates), nil
}

// releaseCRDs lists the CRDs rel's manifest declares: the ones the release
// installed, so a healthy install that left an optional CRD out does not lack it.
func releaseCRDs(rel *release.Release) ([]string, error) {
	if rel == nil {
		return nil, nil
	}
	docs, err := reality.UnmarshalManifest(rel.Manifest)
	if err != nil {
		return nil, errx.Decorate(
			errorx.IllegalState.Wrap(err, "failed to read the manifest of release %q", rel.Name),
			reasons.PreconditionNotMet,
			fmt.Sprintf("Check the release: helm get manifest %s -n %s", rel.Name, rel.Namespace),
		)
	}
	var names []string
	for _, doc := range docs {
		if doc.GetKind() == "CustomResourceDefinition" {
			names = append(names, doc.GetName())
		}
	}
	return names, nil
}

// esoCRDGaps reports the ESO CRDs being deleted, and the CRDs rel's manifest
// declares that are missing. A nil rel has nothing to miss.
func esoCRDGaps(ctx context.Context, k esoClusterReader, rel *release.Release) (terminating, missing []string, err error) {
	owned, err := releaseCRDs(rel)
	if err != nil {
		return nil, nil, err
	}
	inRelease := map[string]bool{}
	names := append([]string{}, esoCRDs...)
	for _, name := range owned {
		inRelease[name] = true
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	for _, name := range names {
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
		if !exists && inRelease[name] {
			missing = append(missing, name)
		}
	}
	return terminating, missing, nil
}

// checkInstalledESOCRDs checks that an existing ESO release still has its CRDs.
// A recovery that failed or was interrupted leaves a release whose old CRDs are
// still deleting, or already gone; installing over it would only time out.
// --recover-partial-uninstall picks such a recovery back up.
func checkInstalledESOCRDs(ctx context.Context, k esoClusterReader, rel *release.Release, spec *helmChartSpec, recoverPartial bool) (warning string, err error) {
	terminating, missing, err := esoCRDGaps(ctx, k, rel)
	if err != nil {
		return "", err
	}
	if len(terminating) == 0 && len(missing) == 0 {
		return "", nil
	}
	if recoverPartial {
		return checkRecoveryKeepsData(ctx, k, append(terminating, missing...))
	}

	var gaps []string
	if len(terminating) > 0 {
		gaps = append(gaps, crdsPhrase(terminating)+" still being deleted")
	}
	if len(missing) > 0 {
		gaps = append(gaps, crdsPhrase(missing)+" missing")
	}
	return "", recoveryNeededError(ctx, k, spec, fmt.Sprintf(
		"External Secrets Operator release %q is installed, but %s: a recovery from a partial uninstall has not finished, and External Secrets Operator cannot sync secrets",
		rel.Name, strings.Join(gaps, ", and ")))
}

// recoveryBlockedError refuses a recovery while the reinstalled controller
// would destroy data.
func recoveryBlockedError(owned, ownedPatches, pushRefs, pushPatches []string) error {
	facts, hints := recoveryBlockers(owned, ownedPatches, pushRefs, pushPatches)
	hints = append(hints, "Then run the install again; see "+esoRecoveryDoc)

	return errx.Decorate(
		errorx.IllegalState.New("reinstalling External Secrets Operator would destroy data: %s; nothing was changed",
			strings.Join(facts, "; ")),
		reasons.PreconditionNotMet,
		hints...,
	)
}

// recoveryBlockers describes what a reinstalled controller would destroy, and
// the patches that keep it: Secrets still owned by an ExternalSecret, which
// Kubernetes deletes once the ExternalSecret goes, and PushSecrets with
// deletionPolicy Delete, which delete their pushed secret in the remote store.
func recoveryBlockers(owned, ownedPatches, pushRefs, pushPatches []string) (facts, hints []string) {
	if len(owned) > 0 {
		logx.As().Warn().Strs("secrets", owned).
			Msg("Secrets are still owned by ExternalSecrets, refusing to reinstall over a partial uninstall")
		facts = append(facts, plural(len(owned), "Secret ", "Secrets ")+strings.Join(capList(owned, ""), ", ")+
			plural(len(owned), " is", " are")+" still owned by an ExternalSecret, and Kubernetes deletes "+
			plural(len(owned), "it", "them")+" once the controller finishes deleting that ExternalSecret")
		hints = append(hints, "Detach each Secret you need to keep:")
		hints = append(hints, capList(ownedPatches, "  ")...)
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
	return facts, hints
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

// listGeneratorStates lists the GeneratorStates as namespace/name. A recovery
// cleans them up, and they do not block it: ESO has no setting that keeps what
// their generators made.
func listGeneratorStates(ctx context.Context, k esoClusterReader) ([]string, error) {
	items, err := k.ListResources(ctx, generatorStateKind.groupKind(), "")
	if err != nil {
		return nil, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list GeneratorStates"),
			reasons.K8sAPIFailed,
			"Verify the cluster is reachable: kubectl cluster-info",
			"List them yourself: kubectl get "+generatorStateKind.kubectlName()+" -A",
		)
	}
	refs := make([]string, 0, len(items))
	for _, gs := range items {
		refs = append(refs, gs.GetNamespace()+"/"+gs.GetName())
	}
	return refs, nil
}

// generatorStateFact names the GeneratorStates a recovery will clean up.
func generatorStateFact(refs []string) string {
	return fmt.Sprintf(
		"the reinstalled External Secrets Operator controller will finish deleting %s %s, which cleans up what %s created",
		plural(len(refs), "GeneratorState", "GeneratorStates"), strings.Join(capList(refs, ""), ", "),
		plural(len(refs), "its generator", "their generators"))
}

// ownedESOSecrets lists, as namespace/name, the target Secrets of the
// ExternalSecrets that still have an ExternalSecret owner reference, with the
// patch that removes only that reference. The patch tests the reference's uid
// first, so kubectl refuses it if the owner references have changed since.
func ownedESOSecrets(ctx context.Context, k esoClusterReader) (refs, patches []string, err error) {
	externalSecrets, err := k.ListResources(ctx, externalSecretKind.groupKind(), "")
	if err != nil {
		return nil, nil, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list ExternalSecrets"),
			reasons.K8sAPIFailed,
			"Verify the cluster is reachable: kubectl cluster-info",
			"List them yourself: kubectl get "+externalSecretKind.kubectlName()+" -A",
		)
	}

	for _, es := range externalSecrets {
		target, _, _ := unstructured.NestedString(es.Object, "spec", "target", "name")
		if target == "" {
			target = es.GetName()
		}
		metadata, found, err := k.GetResourceNestedMap(ctx, "v1", "Secret", es.GetNamespace(), target, "metadata")
		if err != nil {
			return nil, nil, errx.Decorate(
				errorx.ExternalError.Wrap(err, "failed to read Secret %s/%s", es.GetNamespace(), target),
				reasons.K8sAPIFailed,
				fmt.Sprintf("Check the Secret: kubectl get secret %s -n %s -o yaml", target, es.GetNamespace()),
			)
		}
		if !found {
			continue
		}
		index, uid, owned := externalSecretOwnerRef(metadata)
		if !owned {
			continue
		}
		refs = append(refs, es.GetNamespace()+"/"+target)
		patches = append(patches, fmt.Sprintf(
			`  kubectl patch secret %s -n %s --type=json -p '[{"op":"test","path":"/metadata/ownerReferences/%d/uid","value":"%s"},{"op":"remove","path":"/metadata/ownerReferences/%d"}]'`,
			target, es.GetNamespace(), index, uid, index))
	}
	return refs, patches, nil
}

// externalSecretOwnerRef finds the first ExternalSecret owner reference in a
// metadata map, and reports its index and uid.
func externalSecretOwnerRef(metadata map[string]interface{}) (index int, uid string, ok bool) {
	refs, _ := metadata["ownerReferences"].([]interface{})
	for i, r := range refs {
		ref, _ := r.(map[string]interface{})
		apiVersion, _ := ref["apiVersion"].(string)
		kind, _ := ref["kind"].(string)
		if kind == externalSecretKind.kind && strings.HasPrefix(apiVersion, externalSecretKind.group+"/") {
			uid, _ = ref["uid"].(string)
			return i, uid, true
		}
	}
	return 0, "", false
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

// recoverESOInstall reinstalls ESO over a partial uninstall. While ESO CRDs are
// terminating, it installs the chart without Helm's wait, so the returning
// controller clears the old objects' finalizers and those CRDs finish deleting,
// taking the release's adopted copies with them. With them gone no ESO object
// is left to strand, so it then uninstalls and installs again to restore every
// CRD. With nothing terminating (a rerun after the drain finished) it goes
// straight to the reinstall. The warning tells the operator to recreate what
// the partial uninstall deleted.
func recoverESOInstall(ctx context.Context, hm helm.Manager, k esoClusterReader, spec *helmChartSpec, terminating, missing []string, timeout, interval time.Duration) (warning string, err error) {
	rerun := fmt.Sprintf("  sudo solo-provisioner eso operator install --recover-partial-uninstall --namespace %s", spec.Namespace)

	if len(terminating) > 0 {
		if err := drainESOCRDs(ctx, hm, k, spec, terminating, missing, rerun, timeout, interval); err != nil {
			return "", err
		}
	}

	if _, err := uninstallESOChart(ctx, hm, k, spec); err != nil {
		return "", err
	}
	if _, err := installESOChart(ctx, hm, spec, false); err != nil {
		// Helm's atomic rollback has removed the release; the recovery path
		// handles whatever CRDs that left, deleting or gone.
		return "", errx.Decorate(err, reasons.PreconditionNotMet,
			"The partial uninstall has finished and the old release is removed; run the recovery again:",
			rerun,
			"Then recreate the stores and ExternalSecrets: see "+esoRecoveryDoc)
	}

	logx.As().Warn().Strs("crds", terminating).
		Msg("Recovered External Secrets Operator from a partial uninstall")
	return "Recovered External Secrets Operator from a partial uninstall. The partial uninstall deleted the SecretStores, " +
		"ClusterSecretStores and ExternalSecrets: recreate them, and each ExternalSecret takes its detached Secret back; see " +
		esoRecoveryDoc, nil
}

// drainESOCRDs installs the chart without Helm's wait and waits for the
// terminating CRDs to finish deleting. On a resumed recovery the release is
// already there; when it lacks some of its CRDs, it is re-applied first, since
// the controller may not start without them and never drain the rest.
func drainESOCRDs(ctx context.Context, hm helm.Manager, k esoClusterReader, spec *helmChartSpec, terminating, missing []string, rerun string, timeout, interval time.Duration) error {
	installed, err := installESOChart(ctx, hm, spec, true)
	if err != nil {
		return errx.Decorate(err, reasons.PreconditionNotMet,
			"Check what is left: kubectl get crd | grep external-secrets",
			"Remove the failed release, then run the recovery again:",
			fmt.Sprintf("  sudo solo-provisioner eso operator uninstall --namespace %s", spec.Namespace),
			rerun)
	}
	if !installed && len(missing) > 0 {
		if err := reapplyESOChart(ctx, hm, spec); err != nil {
			return errx.Decorate(err, reasons.PreconditionNotMet,
				"Check the release: helm list -a -n "+spec.Namespace,
				"Then run the recovery again:",
				rerun)
		}
	}

	if err := waitESOCRDsGone(ctx, k, terminating, timeout, interval); err != nil {
		if errorx.IsOfType(err, errorx.Interrupted) {
			return errx.Decorate(err, reasons.PreconditionNotMet,
				"Run the install again to finish the recovery:",
				rerun)
		}
		return errx.Decorate(
			errorx.IllegalState.Wrap(err, "the reinstalled External Secrets Operator controller has not cleared the old objects' finalizers"),
			reasons.PreconditionNotMet,
			"Check the controller is running: kubectl get pods -n "+spec.Namespace,
			"Check what is left: kubectl get "+externalSecretKind.kubectlName()+" -A",
			"Then run the install again:",
			rerun,
		)
	}
	return nil
}

// esoRecoveryGaps reports, when the install asks to recover from a partial
// uninstall, the ESO CRDs still being deleted and the CRDs the ESO release in
// spec.Namespace declares but lacks, with the reader it read them through.
// Neither means a plain install: the precheck has already refused a recovery
// that needs the flag. It reads the state itself, so a drain that finished
// since the precheck still counts.
func esoRecoveryGaps(ctx context.Context, hm helm.Manager, spec *helmChartSpec, recoverPartial bool, provider esoClusterReaderProvider) (k esoClusterReader, terminating, missing []string, err error) {
	if !recoverPartial {
		return nil, nil, nil, nil
	}
	if k, err = resolveESOClusterReader(ctx, provider); err != nil {
		return nil, nil, nil, err
	}
	rel, err := resolveESORelease(hm, spec)
	if err != nil {
		return nil, nil, nil, err
	}
	if terminating, missing, err = esoCRDGaps(ctx, k, rel); err != nil {
		return nil, nil, nil, err
	}
	return k, terminating, missing, nil
}

// reapplyESOChart upgrades the ESO release to the catalog chart without Helm's
// wait and atomic rollback, so the CRDs it lacks come back while others are
// still deleting, as installESOChart does for a recovery install.
func reapplyESOChart(ctx context.Context, hm helm.Manager, spec *helmChartSpec) error {
	localChart, err := pullESOChart(ctx, hm, spec)
	if err != nil {
		return err
	}
	_, err = hm.UpgradeChart(ctx, spec.Release, localChart, "", spec.Namespace, helm.UpgradeChartOptions{
		ValueOpts: esoChartValues(),
		Timeout:   helm.DefaultTimeout,
	})
	return err
}
