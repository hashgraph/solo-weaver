// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const PrecheckConsensusStorageClassesStepId = "precheck-consensus-storage-classes"

const (
	defaultStorageClassAnnotation     = "storageclass.kubernetes.io/is-default-class"
	betaDefaultStorageClassAnnotation = "storageclass.beta.kubernetes.io/is-default-class"
	noProvisionerStorageClass         = "kubernetes.io/no-provisioner"
)

// storageClassNeeds lists what the pvc-backed volumes ask of the cluster.
type storageClassNeeds struct {
	named        map[string][]string // StorageClass name -> volumes that name it
	defaultUsers []string            // volumes that rely on the cluster default
}

func (n storageClassNeeds) empty() bool {
	return len(n.named) == 0 && len(n.defaultUsers) == 0
}

// storageClassGaps is what the cluster lacks for the pvc-backed volumes.
type storageClassGaps struct {
	missing       []string // named StorageClasses that do not exist
	noDefault     bool     // a volume needs the default, but none is marked
	noProvisioner []string // StorageClasses in use that cannot create volumes
}

func (g storageClassGaps) ok() bool {
	return len(g.missing) == 0 && !g.noDefault
}

func pvcStorageClassNeeds(inputs models.ConsensusNodeInputs) (storageClassNeeds, error) {
	needs := storageClassNeeds{named: map[string][]string{}}
	for _, name := range models.ConsensusVolumeNames() {
		spec, err := inputs.Volumes.EffectiveSpec(name)
		if err != nil {
			return storageClassNeeds{}, err
		}
		if spec.Type != models.VolumeBackingPVC {
			continue
		}
		if spec.StorageClass == "" {
			needs.defaultUsers = append(needs.defaultUsers, name)
		} else {
			needs.named[spec.StorageClass] = append(needs.named[spec.StorageClass], name)
		}
	}
	return needs, nil
}

// ConsensusHasPVCVolumes reports whether any data volume resolves to a pvc backing,
// so the install schedules PrecheckConsensusStorageClasses.
func ConsensusHasPVCVolumes(inputs models.ConsensusNodeInputs) bool {
	needs, err := pvcStorageClassNeeds(inputs)
	return err == nil && !needs.empty()
}

func isDefaultStorageClass(annotations map[string]string) bool {
	return annotations[defaultStorageClassAnnotation] == "true" ||
		annotations[betaDefaultStorageClassAnnotation] == "true"
}

func findStorageClassGaps(needs storageClassNeeds, classes []unstructured.Unstructured) storageClassGaps {
	provisioners := map[string]string{}
	var defaults []string
	for _, sc := range classes {
		prov, _, _ := unstructured.NestedString(sc.Object, "provisioner")
		provisioners[sc.GetName()] = prov
		if isDefaultStorageClass(sc.GetAnnotations()) {
			defaults = append(defaults, sc.GetName())
		}
	}

	var gaps storageClassGaps
	inUse := map[string]bool{}
	for name := range needs.named {
		if _, ok := provisioners[name]; ok {
			inUse[name] = true
		} else {
			gaps.missing = append(gaps.missing, name)
		}
	}
	if len(needs.defaultUsers) > 0 {
		if len(defaults) == 0 {
			gaps.noDefault = true
		}
		for _, d := range defaults {
			inUse[d] = true
		}
	}
	for name := range inUse {
		if provisioners[name] == noProvisionerStorageClass {
			gaps.noProvisioner = append(gaps.noProvisioner, name)
		}
	}
	sort.Strings(gaps.missing)
	sort.Strings(gaps.noProvisioner)
	return gaps
}

func storageClassProblems(needs storageClassNeeds, gaps storageClassGaps) string {
	var problems []string
	for _, name := range gaps.missing {
		problems = append(problems, fmt.Sprintf("StorageClass %q does not exist (volumes: %s)",
			name, strings.Join(needs.named[name], ", ")))
	}
	if gaps.noDefault {
		problems = append(problems, fmt.Sprintf("no default StorageClass is set (volumes without storageClass: %s)",
			strings.Join(needs.defaultUsers, ", ")))
	}
	return "pvc volumes cannot get storage: " + strings.Join(problems, "; ")
}

func storageClassGapError(needs storageClassNeeds, gaps storageClassGaps) error {
	hints := []string{"List the StorageClasses in the cluster: kubectl get storageclass"}
	if gaps.noDefault {
		hints = append(hints, "Install a StorageClass and mark it as default (annotation "+defaultStorageClassAnnotation+"=true)")
	}
	hints = append(hints,
		"Or name a StorageClass that exists: --default-pvc-storage-class <name>, or storageClass=<name> on --volume or in --volumes-file",
		"Or use node-local storage: --default-volume-type hostpath, and remove type=pvc from --volume and --volumes-file")

	return errx.Decorate(
		errorx.IllegalState.New("%s", storageClassProblems(needs, gaps)),
		reasons.PreconditionNotMet,
		hints...)
}

// PrecheckConsensusStorageClasses fails the install before any resource is created
// when a pvc-backed volume needs a StorageClass that the cluster does not have.
// With --force it logs a warning and continues (existing PVCs can be bound already).
func PrecheckConsensusStorageClasses(inputs models.ConsensusNodeInputs, force bool, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(PrecheckConsensusStorageClassesStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			needs, err := pvcStorageClassNeeds(inputs)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(err,
					reasons.InvalidArgument,
					"Check --default-volume-type, --volume and --volumes-file")))
			}
			if needs.empty() {
				return automa.StepSuccessReport(stp.Id())
			}

			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.IllegalState.Wrap(err, "cannot connect to Kubernetes cluster"),
					reasons.PreconditionNotMet,
					"Verify your kubeconfig and that 'kubectl get nodes' works")))
			}

			list, err := kc.List(ctx, kube.KindStorageClass, "", kube.WaitOptions{})
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to list StorageClasses"),
					reasons.K8sAPIFailed,
					"Verify cluster connectivity and RBAC for listing StorageClasses: kubectl get storageclass")))
			}

			gaps := findStorageClassGaps(needs, list.Items)
			for _, name := range gaps.noProvisioner {
				msg := fmt.Sprintf("StorageClass %q has no provisioner; PVCs bind only to matching PersistentVolumes", name)
				logx.As().Warn().Msg(msg)
				notify.As().StepDetail(ctx, stp, msg)
			}
			if gaps.ok() {
				return automa.StepSuccessReport(stp.Id())
			}

			if force {
				msg := "continuing because --force was passed: " + storageClassProblems(needs, gaps)
				logx.As().Warn().Msg(msg)
				notify.As().StepDetail(ctx, stp, msg)
				return automa.StepSuccessReport(stp.Id())
			}
			return automa.StepFailureReport(stp.Id(), automa.WithError(storageClassGapError(needs, gaps)))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking StorageClasses for pvc volumes")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "StorageClass missing for pvc volumes")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "StorageClass check passed")
		})
}
