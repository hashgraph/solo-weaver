// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"slices"
	"strings"

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

const PrecheckConsensusPVCsStepId = "precheck-consensus-pvcs"

// existingPVC is the part of a live PVC spec that the install request is compared with.
type existingPVC struct {
	name         string
	size         string
	storageClass string
	accessModes  []string
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

// pvcStranded is an existing PVC whose volume the request backs with emptydir or hostpath.
type pvcStranded struct {
	volume  string
	backing string
	pvc     existingPVC
}

// PrecheckConsensusPVCs stops an install that would silently reuse (drift: --force
// skips) or not mount (stranded: --force/--non-interactive skip) an existing PVC.
func PrecheckConsensusPVCs(inputs models.ConsensusNodeInputs, force, nonInteractive bool, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckConsensusPVCsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			list, err := kc.List(ctx, kube.KindPVC, inputs.Namespace, kube.WaitOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to list PVCs in namespace %s", inputs.Namespace),
					reasons.K8sAPIFailed,
					"Verify cluster connectivity and RBAC for listing PersistentVolumeClaims")))
			}

			drifts, stranded, err := comparePVCs(inputs, list.Items)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(err,
					reasons.InvalidArgument,
					"Fix the --volume / --volumes-file entry for this volume")))
			}

			if len(drifts) > 0 && !force {
				return automa.StepFailureReport(stp.Id(), automa.WithError(pvcDriftError(inputs, drifts)))
			}
			if len(stranded) > 0 && !force && !nonInteractive {
				return automa.StepFailureReport(stp.Id(), automa.WithError(pvcStrandedError(inputs, stranded)))
			}

			for _, d := range drifts {
				logx.As().Warn().Str("volume", d.volume).Str("pvc", d.pvc.name).
					Str("differences", formatMismatches(d.mismatches)).
					Msg("Reusing existing PVC as it is; the requested settings have no effect")
			}
			for _, s := range stranded {
				logx.As().Warn().Str("volume", s.volume).Str("backing", s.backing).Str("pvc", s.pvc.name).
					Msg("Existing PVC with data will not be mounted; the PVC is kept")
			}

			return automa.StepSuccessReport(stp.Id())
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
	var hints []string
	for _, d := range drifts {
		lines = append(lines, fmt.Sprintf("%s (PVC %s): %s", d.volume, d.pvc.name, formatMismatches(d.mismatches)))
		keep = append(keep, "--volume "+keepVolumeArg(d.volume, d.pvc, d.mismatches))
		if d.grow != "" {
			hints = append(hints, fmt.Sprintf(
				"To grow %s (only if its StorageClass allows volume expansion): kubectl -n %s patch pvc %s -p '{\"spec\":{\"resources\":{\"requests\":{\"storage\":\"%s\"}}}}'",
				d.pvc.name, inputs.Namespace, d.pvc.name, d.grow))
		}
	}
	hints = append([]string{"To keep the existing PVCs, install with their current values: " + strings.Join(keep, " ")}, hints...)
	hints = append(hints,
		fmt.Sprintf("To start with an empty volume, delete its PVC (this deletes its data): kubectl -n %s delete pvc <name>", inputs.Namespace),
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
		fmt.Sprintf("To discard the data, delete the PVC (this deletes its data): kubectl -n %s delete pvc <name>", inputs.Namespace),
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
