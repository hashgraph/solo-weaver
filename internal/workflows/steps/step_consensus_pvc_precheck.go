// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	PrecheckConsensusPVCsStepId  = "precheck-consensus-pvcs"
	ReconcileConsensusPVCsStepId = "reconcile-consensus-pvcs"
)

// pvcDeleteTimeout bounds the wait for a deleted PVC to be gone.
const pvcDeleteTimeout = 2 * time.Minute

// ConsensusPVCClient is the subset of *kube.Client used by the consensus PVC steps.
type ConsensusPVCClient interface {
	ResourceExists(ctx context.Context, apiVersion, kind, namespace, name string) (bool, error)
	List(ctx context.Context, kind kube.ResourceKind, namespace string, opts kube.WaitOptions) (*unstructured.UnstructuredList, error)
	GetResourceNestedBool(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (bool, bool, error)
	ResizePVC(ctx context.Context, namespace, name, size string) error
	DeletePVC(ctx context.Context, namespace, name string) error
	WaitForResource(ctx context.Context, kind kube.ResourceKind, namespace, name string, checkFn kube.CheckFunc, timeout time.Duration) error
}

// ConsensusPVCProvider resolves a ConsensusPVCClient at step-execution time. Tests inject a fake.
type ConsensusPVCProvider func(ctx context.Context) (ConsensusPVCClient, error)

// DefaultConsensusPVCProvider returns a live kube client.
func DefaultConsensusPVCProvider(context.Context) (ConsensusPVCClient, error) {
	return kube.NewClient()
}

// existingPVC is the part of a live PVC that the install request is compared with.
type existingPVC struct {
	name         string
	size         string
	storageClass string
	accessModes  []string
	phase        string
}

// pvcMismatch is one field of an existing PVC that differs from the request.
type pvcMismatch struct {
	field     string
	existing  string
	requested string
}

// pvcDrift is an existing pvc-backed volume whose PVC differs from the request.
type pvcDrift struct {
	volume     string
	pvc        existingPVC
	mismatches []pvcMismatch
	grow       string // requested size when it is larger than the existing one
}

// growOnly reports whether a larger size is the only difference.
func (d pvcDrift) growOnly() bool {
	return d.grow != "" && len(d.mismatches) == 1 && d.mismatches[0].field == "size"
}

// pvcStranded is an existing PVC whose volume the request backs with emptydir or hostpath.
type pvcStranded struct {
	volume  string
	backing string
	pvc     existingPVC
}

// pvcRemoval is a PVC that --recreate-pvcs deletes. recreated: the operator creates it again.
type pvcRemoval struct {
	volume    string
	pvc       existingPVC
	recreated bool
}

// pvcPlan sorts the existing PVCs into flag actions and the stops that no flag covers.
type pvcPlan struct {
	expand   []pvcDrift
	remove   []pvcRemoval
	drifts   []pvcDrift
	stranded []pvcStranded
}

func (p pvcPlan) hasActions() bool { return len(p.expand) > 0 || len(p.remove) > 0 }

// planPVCs applies the flags: --expand-pvcs takes grow-only drifts, --recreate-pvcs the rest.
func planPVCs(drifts []pvcDrift, stranded []pvcStranded, expand, recreate bool) pvcPlan {
	var p pvcPlan
	for _, d := range drifts {
		switch {
		case expand && d.growOnly():
			p.expand = append(p.expand, d)
		case recreate:
			p.remove = append(p.remove, pvcRemoval{volume: d.volume, pvc: d.pvc, recreated: true})
		default:
			p.drifts = append(p.drifts, d)
		}
	}
	for _, s := range stranded {
		if recreate {
			p.remove = append(p.remove, pvcRemoval{volume: s.volume, pvc: s.pvc})
			continue
		}
		p.stranded = append(p.stranded, s)
	}
	return p
}

// PrecheckConsensusPVCs stops an install that would silently reuse (drift: --force
// skips) or not mount (stranded: --force/--non-interactive skip) an existing PVC.
// It also validates the --expand-pvcs / --recreate-pvcs actions. It changes nothing.
func PrecheckConsensusPVCs(inputs models.ConsensusNodeInputs, force, nonInteractive bool, provider ConsensusPVCProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckConsensusPVCsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, plan, err := loadPVCPlan(ctx, inputs, provider)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if len(plan.drifts) > 0 && !force {
				return automa.StepFailureReport(stp.Id(), automa.WithError(pvcDriftError(inputs, plan.drifts)))
			}
			if len(plan.stranded) > 0 && !force && !nonInteractive {
				return automa.StepFailureReport(stp.Id(), automa.WithError(pvcStrandedError(inputs, plan.stranded)))
			}
			if err := validatePVCPlan(ctx, kc, inputs, plan); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			// The TUI sends logs only to the file, so the warnings also go to the end summary.
			var warnings []string
			for _, d := range plan.drifts {
				logx.As().Warn().Str("volume", d.volume).Str("pvc", d.pvc.name).
					Str("differences", formatMismatches(d.mismatches)).
					Msg("Reusing existing PVC as it is; the requested settings have no effect")
				warnings = append(warnings, fmt.Sprintf("PVC %s is reused as it is (%s); the requested settings have no effect",
					d.pvc.name, formatMismatches(d.mismatches)))
			}
			for _, s := range plan.stranded {
				logx.As().Warn().Str("volume", s.volume).Str("backing", s.backing).Str("pvc", s.pvc.name).
					Msg("Existing PVC with data will not be mounted; the PVC is kept")
				warnings = append(warnings, fmt.Sprintf("PVC %s has data but is not mounted (%s is %s); the PVC is kept",
					s.pvc.name, s.volume, s.backing))
			}
			for _, d := range plan.expand {
				logx.As().Info().Str("pvc", d.pvc.name).Str("from", d.pvc.size).Str("to", d.grow).
					Msg("PVC will be expanded (--expand-pvcs)")
			}
			for _, r := range plan.remove {
				logx.As().Warn().Str("volume", r.volume).Str("pvc", r.pvc.name).Str("size", r.pvc.size).
					Msg("PVC and its data will be deleted (--recreate-pvcs)")
			}

			if len(warnings) == 0 {
				return automa.StepSuccessReport(stp.Id())
			}
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(automa.StringMap{
				models.ReportMetaWarning: strings.Join(warnings, "\n"),
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking existing consensus node PVCs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Existing PVCs do not match the install request")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Existing PVC check passed")
		})
}

// ReconcileConsensusPVCs runs the --expand-pvcs / --recreate-pvcs actions before the
// capsule is created. It plans and validates again, so it acts only on checked PVCs.
func ReconcileConsensusPVCs(inputs models.ConsensusNodeInputs, provider ConsensusPVCProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(ReconcileConsensusPVCsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, plan, err := loadPVCPlan(ctx, inputs, provider)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := validatePVCPlan(ctx, kc, inputs, plan); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			for _, d := range plan.expand {
				if err := kc.ResizePVC(ctx, inputs.Namespace, d.pvc.name, d.grow); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(err,
						reasons.K8sAPIFailed,
						fmt.Sprintf("Inspect the PVC: kubectl -n %s describe pvc %s", inputs.Namespace, d.pvc.name),
						"Verify RBAC for patching PersistentVolumeClaims")))
				}
				// No wait: many drivers finish the filesystem resize only when a pod mounts the volume.
				logx.As().Info().Str("pvc", d.pvc.name).Str("from", d.pvc.size).Str("to", d.grow).Msg("PVC expansion requested")
			}

			for _, r := range plan.remove {
				if err := deletePVCAndWait(ctx, kc, inputs.Namespace, r.pvc.name); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(err,
						reasons.K8sAPIFailed,
						fmt.Sprintf("See what still uses the PVC (Used By): kubectl -n %s describe pvc %s", inputs.Namespace, r.pvc.name),
						"Re-run install once the PVC is gone")))
				}
				logx.As().Warn().Str("volume", r.volume).Str("pvc", r.pvc.name).Str("size", r.pvc.size).
					Bool("recreatedByOperator", r.recreated).Msg("PVC and its data deleted (--recreate-pvcs)")
			}

			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Expanding or deleting existing consensus node PVCs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to change existing PVCs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Existing PVCs updated")
		})
}

// loadPVCPlan lists the namespace PVCs and plans the actions. Errors are decorated.
func loadPVCPlan(ctx context.Context, inputs models.ConsensusNodeInputs, provider ConsensusPVCProvider) (ConsensusPVCClient, pvcPlan, error) {
	kc, err := provider(ctx)
	if err != nil {
		return nil, pvcPlan{}, errx.Decorate(
			errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
			reasons.PreconditionNotMet,
			"Verify your kubeconfig and that 'kubectl get nodes' works")
	}

	list, err := kc.List(ctx, kube.KindPVC, inputs.Namespace, kube.WaitOptions{})
	if err != nil {
		return nil, pvcPlan{}, errx.Decorate(
			errorx.ExternalError.Wrap(err, "failed to list PVCs in namespace %s", inputs.Namespace),
			reasons.K8sAPIFailed,
			"Verify cluster connectivity and RBAC for listing PersistentVolumeClaims")
	}

	drifts, stranded, err := comparePVCs(inputs, list.Items)
	if err != nil {
		return nil, pvcPlan{}, errx.Decorate(err,
			reasons.InvalidArgument,
			"Fix the --volume / --volumes-file entry for this volume")
	}
	return kc, planPVCs(drifts, stranded, inputs.ExpandPVCs, inputs.RecreatePVCs), nil
}

// validatePVCPlan checks that each planned action can run. A failed expand never
// falls back to a delete.
func validatePVCPlan(ctx context.Context, kc ConsensusPVCClient, inputs models.ConsensusNodeInputs, plan pvcPlan) error {
	var problems, keep []string
	for _, d := range plan.expand {
		reason, err := pvcExpandBlocker(ctx, kc, d.pvc)
		if err != nil {
			return errx.Decorate(err, reasons.K8sAPIFailed,
				"Verify cluster connectivity and RBAC for reading StorageClasses")
		}
		if reason != "" {
			problems = append(problems, fmt.Sprintf("%s: %s", d.pvc.name, reason))
			keep = append(keep, "--volume "+keepVolumeArg(d.volume, d.pvc, d.mismatches))
		}
	}
	if len(problems) > 0 {
		return errx.Decorate(
			errorx.IllegalState.New("consensus node %d: --expand-pvcs cannot grow these PVCs: %s",
				inputs.NodeId, strings.Join(problems, "; ")),
			reasons.PreconditionNotMet,
			"To keep the PVCs, install with their current size: "+strings.Join(keep, " "),
			"Run install with --recreate-pvcs and without --expand-pvcs to delete these PVCs and create them again (this deletes their data)")
	}

	if len(plan.remove) > 0 {
		capsuleName := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)
		exists, err := kc.ResourceExists(ctx, kube.SoloOperatorAPIVersion, string(kube.KindConsensusCapsule), inputs.Namespace, capsuleName)
		if err != nil {
			return errx.Decorate(
				errorx.ExternalError.Wrap(err, "failed to check ConsensusCapsule %s", capsuleName),
				reasons.K8sAPIFailed,
				"Verify cluster connectivity and RBAC for reading ConsensusCapsule resources")
		}
		if exists {
			return errx.Decorate(
				errorx.IllegalState.New("consensus node %d: --recreate-pvcs cannot delete PVCs while ConsensusCapsule %q exists, because its pod uses them",
					inputs.NodeId, capsuleName),
				reasons.PreconditionNotMet,
				fmt.Sprintf("Uninstall the node first: sudo solo-provisioner consensus node uninstall --experimental --namespace %s --node-id %d", inputs.Namespace, inputs.NodeId),
				"Then run install again with --recreate-pvcs")
		}
	}
	return nil
}

// pvcExpandBlocker returns why a PVC cannot be expanded, or "" when it can.
func pvcExpandBlocker(ctx context.Context, kc ConsensusPVCClient, pvc existingPVC) (string, error) {
	if pvc.phase != "Bound" {
		return fmt.Sprintf("the PVC is %s, not Bound", orNone(pvc.phase)), nil
	}
	if pvc.storageClass == "" {
		return "the PVC has no StorageClass", nil
	}
	exists, err := kc.ResourceExists(ctx, "storage.k8s.io/v1", "StorageClass", "", pvc.storageClass)
	if err != nil {
		return "", errorx.ExternalError.Wrap(err, "failed to read StorageClass %s", pvc.storageClass)
	}
	if !exists {
		return fmt.Sprintf("StorageClass %s not found", pvc.storageClass), nil
	}
	allowed, _, err := kc.GetResourceNestedBool(ctx, "storage.k8s.io/v1", "StorageClass", "", pvc.storageClass, "allowVolumeExpansion")
	if err != nil {
		return "", errorx.ExternalError.Wrap(err, "failed to read StorageClass %s", pvc.storageClass)
	}
	if !allowed {
		return fmt.Sprintf("StorageClass %s does not allow volume expansion", pvc.storageClass), nil
	}
	return "", nil
}

func deletePVCAndWait(ctx context.Context, kc ConsensusPVCClient, namespace, name string) error {
	if err := kc.DeletePVC(ctx, namespace, name); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to delete PVC %s/%s", namespace, name)
	}
	if err := kc.WaitForResource(ctx, kube.KindPVC, namespace, name, kube.IsDeleted, pvcDeleteTimeout); err != nil {
		return errorx.ExternalError.Wrap(err, "PVC %s/%s is not gone after %s", namespace, name, pvcDeleteTimeout)
	}
	return nil
}

// comparePVCs compares each data volume's resolved backing with the PVC of
// the same name, if one exists. Volumes without a PVC are a fresh install.
func comparePVCs(inputs models.ConsensusNodeInputs, items []unstructured.Unstructured) ([]pvcDrift, []pvcStranded, error) {
	capsuleName := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)
	byName := make(map[string]*unstructured.Unstructured, len(items))
	for i := range items {
		byName[items[i].GetName()] = &items[i]
	}

	var drifts []pvcDrift
	var stranded []pvcStranded
	for _, volume := range models.ConsensusVolumeNames() {
		obj, ok := byName[capsuleName+"-"+volume]
		if !ok {
			continue
		}
		pvc := readExistingPVC(obj)

		spec, err := inputs.Volumes.EffectiveSpec(volume)
		if err != nil {
			return nil, nil, errorx.IllegalArgument.Wrap(err, "resolve volume %q", volume)
		}

		if spec.Type != models.VolumeBackingPVC {
			stranded = append(stranded, pvcStranded{volume: volume, backing: spec.Type, pvc: pvc})
			continue
		}

		d, err := diffPVC(volume, pvc, spec)
		if err != nil {
			return nil, nil, err
		}
		if len(d.mismatches) > 0 {
			drifts = append(drifts, d)
			continue
		}
		logx.As().Info().Str("volume", volume).Str("pvc", pvc.name).Msg("Reusing existing PVC")
	}
	return drifts, stranded, nil
}

func readExistingPVC(obj *unstructured.Unstructured) existingPVC {
	pvc := existingPVC{name: obj.GetName()}
	pvc.size, _, _ = unstructured.NestedString(obj.Object, "spec", "resources", "requests", "storage")
	pvc.storageClass, _, _ = unstructured.NestedString(obj.Object, "spec", "storageClassName")
	pvc.accessModes, _, _ = unstructured.NestedStringSlice(obj.Object, "spec", "accessModes")
	pvc.phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")
	return pvc
}

// diffPVC compares sizes as quantities (1Ti == 1024Gi). An empty requested
// storage class means "cluster default", so the class the cluster set is accepted.
func diffPVC(volume string, pvc existingPVC, spec models.ConsensusVolumeSpec) (pvcDrift, error) {
	d := pvcDrift{volume: volume, pvc: pvc}

	want, err := resource.ParseQuantity(spec.Size)
	if err != nil {
		return d, errorx.IllegalArgument.Wrap(err, "invalid size %q for volume %q", spec.Size, volume)
	}
	have, err := resource.ParseQuantity(pvc.size)
	switch {
	case err != nil:
		d.mismatches = append(d.mismatches, pvcMismatch{"size", orNone(pvc.size), spec.Size})
	case have.Cmp(want) != 0:
		d.mismatches = append(d.mismatches, pvcMismatch{"size", pvc.size, spec.Size})
		if want.Cmp(have) > 0 {
			d.grow = spec.Size
		}
	}

	if spec.StorageClass != "" && pvc.storageClass != spec.StorageClass {
		d.mismatches = append(d.mismatches, pvcMismatch{"storageClass", orNone(pvc.storageClass), spec.StorageClass})
	}

	if !slices.Equal(pvc.accessModes, []string{spec.AccessMode}) {
		d.mismatches = append(d.mismatches, pvcMismatch{"accessMode", orNone(strings.Join(pvc.accessModes, ",")), spec.AccessMode})
	}
	return d, nil
}

func pvcDriftError(inputs models.ConsensusNodeInputs, drifts []pvcDrift) error {
	lines := make([]string, 0, len(drifts))
	keep := make([]string, 0, len(drifts))
	var grow []string
	for _, d := range drifts {
		lines = append(lines, fmt.Sprintf("%s (PVC %s): %s", d.volume, d.pvc.name, formatMismatches(d.mismatches)))
		keep = append(keep, "--volume "+keepVolumeArg(d.volume, d.pvc, d.mismatches))
		if d.growOnly() {
			grow = append(grow, d.pvc.name)
		}
	}
	hints := []string{"To keep the existing PVCs, install with their current values: " + strings.Join(keep, " ")}
	if len(grow) > 0 {
		hints = append(hints, fmt.Sprintf(
			"Pass --expand-pvcs to grow %s (the StorageClass must allow volume expansion; the data is kept)",
			strings.Join(grow, ", ")))
	}
	hints = append(hints,
		"Pass --recreate-pvcs to delete these PVCs and create them with the requested settings (this deletes their data)",
		"Pass --force to reuse the existing PVCs as they are")

	return errx.Decorate(
		errorx.IllegalState.New(
			"consensus node %d: existing PVCs differ from the requested volume settings, and the operator does not change an existing PVC: %s",
			inputs.NodeId, strings.Join(lines, "; ")),
		reasons.PreconditionNotMet, hints...)
}

func pvcStrandedError(inputs models.ConsensusNodeInputs, stranded []pvcStranded) error {
	lines := make([]string, 0, len(stranded))
	keep := make([]string, 0, len(stranded))
	for _, s := range stranded {
		lines = append(lines, fmt.Sprintf("%s is %s, but PVC %s exists", s.volume, s.backing, s.pvc.name))
		keep = append(keep, "--volume "+keepVolumeArg(s.volume, s.pvc, nil))
	}

	return errx.Decorate(
		errorx.IllegalState.New(
			"consensus node %d: existing PVCs hold data that this install will not mount: %s",
			inputs.NodeId, strings.Join(lines, "; ")),
		reasons.PreconditionNotMet,
		"To keep the data, back the volumes with their PVCs again: "+strings.Join(keep, " "),
		"Pass --recreate-pvcs to delete these PVCs (this deletes their data)",
		"Pass --force or --non-interactive to install without this data (the PVCs are kept)")
}

// keepVolumeArg builds a --volume value that matches the existing PVC. With
// mismatches set, it names only the fields that differ.
func keepVolumeArg(volume string, pvc existingPVC, mismatches []pvcMismatch) string {
	parts := []string{"name=" + volume}
	if mismatches == nil {
		parts = append(parts, "type=pvc")
	}
	has := func(field string) bool {
		return mismatches == nil || slices.ContainsFunc(mismatches, func(m pvcMismatch) bool { return m.field == field })
	}
	if has("size") && pvc.size != "" {
		parts = append(parts, "size="+pvc.size)
	}
	if has("storageClass") && pvc.storageClass != "" {
		parts = append(parts, "storageClass="+pvc.storageClass)
	}
	if has("accessMode") && len(pvc.accessModes) == 1 {
		parts = append(parts, "accessMode="+pvc.accessModes[0])
	}
	return strings.Join(parts, ",")
}

func formatMismatches(ms []pvcMismatch) string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, fmt.Sprintf("%s is %s, requested %s", m.field, m.existing, m.requested))
	}
	return strings.Join(out, ", ")
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
