// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	EnsureOrbitStepId            = "ensure-orbit"
	EnsureConfigCRsStepId        = "ensure-config-crs"
	CreateConsensusCapsuleStepId = "create-consensus-capsule"
	ReportCapsuleStatusStepId    = "report-consensus-capsule-status"
)

// CapsuleKubeClient is the subset of *kube.Client used by the consensus capsule
// steps. Depending on this narrow interface (rather than *kube.Client) lets the
// steps be unit-tested with a fake, mirroring reality.ConsensusKubeClient.
type CapsuleKubeClient interface {
	ResourceExists(ctx context.Context, apiVersion, kind, namespace, name string) (bool, error)
	GetResourceNestedString(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (string, error)
	ApplyTyped(ctx context.Context, obj runtime.Object) error
	List(ctx context.Context, kind kube.ResourceKind, namespace string, opts kube.WaitOptions) (*unstructured.UnstructuredList, error)
}

// CapsuleKubeProvider resolves a CapsuleKubeClient at step-execution time,
// following the step_cluster_* provider convention. Tests inject a fake.
type CapsuleKubeProvider func(ctx context.Context) (CapsuleKubeClient, error)

// DefaultCapsuleKubeProvider returns a live kube client. It ignores ctx (the
// client is not context-scoped) but keeps the provider signature uniform.
func DefaultCapsuleKubeProvider(context.Context) (CapsuleKubeClient, error) {
	return kube.NewClient()
}

// EnsureOrbit creates or updates the Orbit CR (cluster-scoped).
func EnsureOrbit(inputs models.ConsensusNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(EnsureOrbitStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			exists, err := kc.ResourceExists(ctx,
				kube.SoloOperatorGroup+"/"+kube.SoloOperatorVersion,
				string(kube.KindOrbit), "", inputs.OrbitName)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			if exists {
				l.Info().Str("orbit", inputs.OrbitName).Msg("Orbit already exists, skipping creation")
				return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
					AlreadyInstalled: "true",
				}))
			}

			orbit := &operatorv1alpha1.Orbit{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion,
					Kind:       string(kube.KindOrbit),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: inputs.OrbitName,
				},
				Spec: operatorv1alpha1.OrbitSpec{
					Consensus: operatorv1alpha1.OrbitConsensus{
						Genesis: operatorv1alpha1.OrbitGenesis{
							AddressBook: operatorv1alpha1.OrbitAddressBook{
								LedgerId: inputs.LedgerId,
								ChainId:  inputs.ChainId,
							},
						},
					},
				},
			}

			if err := kc.ApplyTyped(ctx, orbit); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			l.Info().Str("orbit", inputs.OrbitName).Msg("Orbit created successfully")
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				InstalledByThisStep: "true",
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Ensuring Orbit CR")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to ensure Orbit CR")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Orbit CR ensured")
		})
}

// EnsureConfigCRs reconciles all 11 config CRs required by a ConsensusCapsule.
// Config contents and their per-file source (package vs embedded) are resolved
// by the BLL layer. Apply policy: create when missing; update when the source is
// the deployment package; preserve deployed content when an embedded default
// would otherwise overwrite it, unless force resets it to defaults.
func EnsureConfigCRs(inputs models.ConsensusNodeInputs, force bool, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(EnsureConfigCRsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			scope := models.ConsensusNodeScope(inputs.NodeId)

			type configEntry struct {
				key     string
				content string
				crName  string
				kind    string
				builder func(scope, content, orbit, ns, name string) runtime.Object
			}

			entries := []configEntry{
				{models.ConfigKeyLog4j2, inputs.ConfigLog4j2, fmt.Sprintf("%s-log4j2", scope), "Log4j2Config",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.Log4j2Config{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "Log4j2Config"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.Log4j2ConfigSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeySettings, inputs.ConfigSettings, fmt.Sprintf("%s-settings", scope), "NodeSettings",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.NodeSettings{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "NodeSettings"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.NodeSettingsSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeyAppProperties, inputs.ConfigAppProperties, fmt.Sprintf("%s-appprops", scope), "ApplicationProperties",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.ApplicationProperties{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "ApplicationProperties"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.ApplicationPropertiesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeyAppOverride, inputs.ConfigAppOverrideProperties, fmt.Sprintf("%s-appoverride", scope), "ApplicationOverrideProperties",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.ApplicationOverrideProperties{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "ApplicationOverrideProperties"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.ApplicationOverridePropertiesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeyApiPermission, inputs.ConfigApiPermission, fmt.Sprintf("%s-apiperm", scope), "ApiPermissionProperties",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.ApiPermissionProperties{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "ApiPermissionProperties"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.ApiPermissionPropertiesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeyBootstrap, inputs.ConfigBootstrap, fmt.Sprintf("%s-bootstrap", scope), "BootstrapProperties",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.BootstrapProperties{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "BootstrapProperties"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.BootstrapPropertiesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeyNodeProperties, inputs.ConfigNodeProperties, fmt.Sprintf("%s-nodeprops", scope), "NodePropertiesConfig",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.NodePropertiesConfig{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "NodePropertiesConfig"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.NodePropertiesConfigSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeyFeeSchedules, inputs.ConfigFeeSchedules, fmt.Sprintf("%s-feeschedules", scope), "FeeSchedules",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.FeeSchedules{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "FeeSchedules"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.FeeSchedulesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeySimpleFeesSchedules, inputs.ConfigSimpleFeesSchedules, fmt.Sprintf("%s-simplefeesschedules", scope), "SimpleFeesSchedules",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.SimpleFeesSchedules{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "SimpleFeesSchedules"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.SimpleFeesSchedulesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeyThrottles, inputs.ConfigThrottles, fmt.Sprintf("%s-throttles", scope), "ThrottlesConfig",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.ThrottlesConfig{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "ThrottlesConfig"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.ThrottlesConfigSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{models.ConfigKeyBlockNodes, inputs.ConfigBlockNodes, fmt.Sprintf("%s-blocknodes", scope), "BlockNodesConfig",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.BlockNodesConfig{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "BlockNodesConfig"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.BlockNodesConfigSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
			}

			// Derive every CR name from the shared source of truth so the config CR
			// names always match the capsule's *Ref names (see ConsensusConfigCRName).
			for i := range entries {
				entries[i].crName = models.ConsensusConfigCRName(scope, entries[i].key)
			}

			l := logx.As()
			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion

			apply := func(e configEntry) error {
				obj := e.builder(scope, e.content, inputs.OrbitName, inputs.Namespace, e.crName)
				if err := kc.ApplyTyped(ctx, obj); err != nil {
					return errx.Decorate(
						errorx.IllegalState.Wrap(err, "failed to apply %s %s", e.kind, e.crName),
						reasons.PreconditionNotMet,
						"Verify cluster connectivity and that your kubeconfig has RBAC to create/update solo-operator config CRs",
						"Check the solo-operator is installed and running (it is provisioned by 'kube cluster install' with soloOperator.enabled)")
				}
				return nil
			}

			for _, e := range entries {
				if e.content == "" {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.New("config content for %s is empty — check deployment package or embedded defaults", e.kind),
						reasons.InvalidArgument,
						"Verify --deployment-package-dir contains the expected config file for this kind, or omit it to use embedded defaults")))
				}

				exists, err := kc.ResourceExists(ctx, apiVersion, e.kind, inputs.Namespace, e.crName)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(err, "failed to check %s %s", e.kind, e.crName),
						reasons.PreconditionNotMet,
						"Verify cluster connectivity and that your kubeconfig has RBAC to read solo-operator config CRs")))
				}

				if !exists {
					if err := apply(e); err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(err))
					}
					l.Info().Str("name", e.crName).Str("kind", e.kind).Msg("Created config CR")
					continue
				}

				deployed, err := kc.GetResourceNestedString(ctx, apiVersion, e.kind, inputs.Namespace, e.crName, "spec", "content")
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalState.Wrap(err, "failed to read deployed %s %s", e.kind, e.crName),
						reasons.PreconditionNotMet,
						"Verify cluster connectivity and that your kubeconfig has RBAC to read solo-operator config CRs")))
				}

				if deployed == e.content {
					l.Debug().Str("name", e.crName).Str("kind", e.kind).Msg("Config CR unchanged; skipping")
					continue
				}

				// The resolved content differs from what is deployed. An explicit
				// package source is authoritative and applies. An implicit embedded
				// default must not silently overwrite operator config — refuse unless
				// forced (which resets to defaults on purpose).
				if inputs.ConfigSources[e.key] == models.ConfigSourcePackage {
					if err := apply(e); err != nil {
						return automa.StepFailureReport(stp.Id(), automa.WithError(err))
					}
					l.Info().Str("name", e.crName).Str("kind", e.kind).Msg("Updated config CR from deployment package")
					continue
				}

				if !force {
					return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
						errorx.IllegalArgument.New(
							"refusing to overwrite deployed config %q with embedded defaults — "+
								"re-run with --deployment-package-dir to keep it, or --force to reset to defaults",
							e.crName),
						reasons.InvalidArgument,
						"Re-run with --deployment-package-dir to keep the deployed config",
						"Or pass --force to reset this config CR to embedded defaults")))
				}

				if err := apply(e); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}
				l.Warn().Str("name", e.crName).Str("kind", e.kind).Msg("Reset config CR to embedded defaults (--force)")
			}

			logx.As().Info().Str("scope", scope).Int("count", len(entries)).Msg("All config CRs reconciled")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Reconciling all 11 config CRs")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create config CRs")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Config CRs created")
		})
}

// CreateConsensusCapsule creates the ConsensusCapsule CR.
func CreateConsensusCapsule(inputs models.ConsensusNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(CreateConsensusCapsuleStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			scope := models.ConsensusNodeScope(inputs.NodeId)
			capsuleName := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)

			// The operator requires a non-empty imageName and assembles the image as
			// repository/imageName:tag (see solo-operator docs/example). The resolved
			// ConsensusImageRepo is the full "registry/path/name", so split off the
			// last segment as the image name.
			imageRepository, imageName := splitConsensusImage(inputs.ConsensusImageRepo)

			// Resolve container sizing/JVM from inputs, falling back to defaults.
			resources, err := buildConsensusResources(inputs)
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(errx.Decorate(
					err, reasons.InvalidArgument,
					"Provide valid Kubernetes quantities for --cpu-limit/--cpu-request/--memory-limit/--memory-request (e.g. 2, 250m, 5Gi, 1Gi)")))
			}

			capsule := &operatorv1alpha1.ConsensusCapsule{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion,
					Kind:       string(kube.KindConsensusCapsule),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      capsuleName,
					Namespace: inputs.Namespace,
				},
				Spec: operatorv1alpha1.ConsensusCapsuleSpec{
					Orbit:                            inputs.OrbitName,
					NodeId:                           inputs.NodeId,
					AccountId:                        inputs.AccountId,
					Weight:                           inputs.Weight,
					Log4j2ConfigRef:                  models.ConsensusConfigCRName(scope, models.ConfigKeyLog4j2),
					SettingsConfigRef:                models.ConsensusConfigCRName(scope, models.ConfigKeySettings),
					ApplicationPropertiesRef:         models.ConsensusConfigCRName(scope, models.ConfigKeyAppProperties),
					ApplicationOverridePropertiesRef: models.ConsensusConfigCRName(scope, models.ConfigKeyAppOverride),
					ApiPermissionPropertiesRef:       models.ConsensusConfigCRName(scope, models.ConfigKeyApiPermission),
					BootstrapPropertiesRef:           models.ConsensusConfigCRName(scope, models.ConfigKeyBootstrap),
					NodePropertiesRef:                models.ConsensusConfigCRName(scope, models.ConfigKeyNodeProperties),
					FeeSchedulesRef:                  models.ConsensusConfigCRName(scope, models.ConfigKeyFeeSchedules),
					SimpleFeesSchedulesRef:           models.ConsensusConfigCRName(scope, models.ConfigKeySimpleFeesSchedules),
					ThrottlesConfigRef:               models.ConsensusConfigCRName(scope, models.ConfigKeyThrottles),
					BlockNodesConfigRef:              models.ConsensusConfigCRName(scope, models.ConfigKeyBlockNodes),
					PodProperties: operatorv1alpha1.ConsensusPodProperties{
						Containers: operatorv1alpha1.ConsensusContainers{
							ConsensusNode: operatorv1alpha1.ConsensusNodeContainer{
								Name: valueOrDefault(inputs.ContainerName, models.ConsensusDefaultContainerName),
								SoftwareVersion: &operatorv1alpha1.SoftwareVersion{
									Repository: imageRepository,
									ImageName:  imageName,
									ImageTag:   inputs.ConsensusImageTag,
									// The operator merges ImagePullSecrets from every container's
									// SoftwareVersion onto the pod (and the node's ServiceAccount),
									// so setting it here covers every container in the pod — the UC
									// sidecar included — without giving UC its own SoftwareVersion
									// (which would override the operator's default UC image).
									ImagePullSecrets: consensusImagePullSecrets(inputs.ImagePullSecret),
								},
								JavaHeapMin: valueOrDefault(inputs.JavaHeapMin, models.ConsensusDefaultJavaHeapMin),
								JavaHeapMax: valueOrDefault(inputs.JavaHeapMax, models.ConsensusDefaultJavaHeapMax),
								JavaOpts:    valueOrDefault(inputs.JavaOpts, models.ConsensusDefaultJavaOpts),
								Resources:   resources,
							},
							// The UC (Update Coordinator) sidecar is mandatory: it is the
							// sole writer of status.platformStatus, so the operator rejects
							// a capsule that does not enable it (UCSidecarRequired).
							UC: &operatorv1alpha1.UcSidecar{Enabled: true},
						},
					},
				},
			}

			if inputs.GrpcTlsSecret != "" || inputs.SigningSecret != "" {
				sr := &operatorv1alpha1.ConsensusSecretResources{}
				if inputs.GrpcTlsSecret != "" {
					sr.GrpcTlsPrivateKey = operatorv1alpha1.KeyReference{
						SecretName: inputs.GrpcTlsSecret,
						KeyName:    fmt.Sprintf("hedera-%s.key", scope),
					}
					sr.GrpcTlsPublicCertificate = operatorv1alpha1.KeyReference{
						SecretName: inputs.GrpcTlsSecret,
						KeyName:    fmt.Sprintf("hedera-%s.crt", scope),
					}
				}
				if inputs.SigningSecret != "" {
					sr.SigningPrivateKey = operatorv1alpha1.KeyReference{
						SecretName: inputs.SigningSecret,
						KeyName:    "private.pem",
					}
					sr.SigningPublicCertificate = operatorv1alpha1.KeyReference{
						SecretName: inputs.SigningSecret,
						KeyName:    "public.pem",
					}
				}
				capsule.Spec.Secrets = &operatorv1alpha1.ConsensusSecrets{
					SecretResources: sr,
				}
			}

			if err := kc.ApplyTyped(ctx, capsule); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			logx.As().Info().
				Str("name", capsuleName).
				Int64("nodeId", inputs.NodeId).
				Msg("ConsensusCapsule created successfully")

			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				InstalledByThisStep: "true",
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Creating ConsensusCapsule for node %d", inputs.NodeId))
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to create ConsensusCapsule")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "ConsensusCapsule created")
		})
}

// ReportConsensusCapsuleStatus reads the ConsensusCapsule's current status once
// and reports it — it does NOT wait or block. Install provisions the CR; whether
// the node then reaches Running depends on the network genesis (fresh networks are
// unblocked out-of-band by `consensus network genesis`) and the start policy
// (Manual nodes stay Stopped until `consensus node start`), so blocking here would
// wrongly time out. The step surfaces the phase plus the right next-step hint and
// always succeeds — a live readiness check is the job of `consensus node status`.
func ReportConsensusCapsuleStatus(inputs models.ConsensusNodeInputs, provider CapsuleKubeProvider) automa.Builder {
	return automa.NewStepBuilder().WithId(ReportCapsuleStatusStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := provider(ctx)
			if err != nil {
				// Reporting is best-effort; a read failure must not fail install.
				notify.As().StepDetail(ctx, stp, fmt.Sprintf("node %d: status unavailable (%v)", inputs.NodeId, err))
				return automa.StepSuccessReport(stp.Id())
			}

			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion
			capsuleName := models.ConsensusCapsuleName(inputs.OrbitName, inputs.NodeId)

			phase, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule), inputs.Namespace, capsuleName, "status", "phase")
			if phase == "" {
				phase = string(operatorv1alpha1.PhasePending)
			}
			policy, _ := kc.GetResourceNestedString(ctx, apiVersion, string(kube.KindConsensusCapsule), inputs.Namespace, capsuleName, "spec", "startPolicy")
			if policy == "" {
				policy = string(operatorv1alpha1.StartPolicyAuto) // CRD default is Auto
			}
			genesisExists, _ := kc.ResourceExists(ctx, "v1", "ConfigMap", inputs.Namespace, NetworkGenesisConfigMapName)

			// Pick the most actionable next step for the operator.
			var hint string
			switch {
			case !genesisExists:
				hint = fmt.Sprintf("awaiting network genesis — run 'solo-provisioner consensus network genesis --namespace %s'", inputs.Namespace)
			case operatorv1alpha1.StartPolicy(policy) == operatorv1alpha1.StartPolicyManual:
				hint = "Manual start policy — run 'solo-provisioner consensus node start' to start it"
			case operatorv1alpha1.Phase(phase) == operatorv1alpha1.PhaseRunning || operatorv1alpha1.Phase(phase) == operatorv1alpha1.PhaseActive:
				hint = "running"
			case operatorv1alpha1.Phase(phase) == operatorv1alpha1.PhaseFailed:
				hint = fmt.Sprintf("Failed — inspect: kubectl -n %s describe consensuscapsule %s", inputs.Namespace, capsuleName)
			default:
				hint = "starting — track with 'solo-provisioner consensus node status'"
			}

			notify.As().StepDetail(ctx, stp, fmt.Sprintf("node %d: phase=%s (%s)", inputs.NodeId, phase, hint))
			return automa.StepSuccessReport(stp.Id(), automa.WithMetadata(map[string]string{
				"phase":       phase,
				"startPolicy": policy,
			}))
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, fmt.Sprintf("Recording consensus node %d status (not started yet — see next steps)", inputs.NodeId))
			return ctx, nil
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Consensus node status recorded")
		})
}

// splitConsensusImage splits a full image reference "registry/path/name" into
// the operator's separate repository (registry + path) and imageName (the last
// path segment). The solo-operator requires a non-empty imageName and assembles
// the container image as repository/imageName:tag (see solo-operator
// docs/example/node0/consensus-capsule.yaml). When the reference has no slash the
// whole value is treated as the image name with an empty repository.
func splitConsensusImage(full string) (repository, imageName string) {
	full = strings.TrimSpace(full)
	if i := strings.LastIndex(full, "/"); i != -1 {
		return full[:i], full[i+1:]
	}
	return "", full
}

// consensusImagePullSecrets maps an optional image-pull secret name to the
// operator's ImagePullSecrets slice. An empty name yields nil (no secret —
// public images), otherwise a single LocalObjectReference the operator threads
// onto the pod and the node's ServiceAccount.
func consensusImagePullSecrets(name string) []corev1.LocalObjectReference {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	return []corev1.LocalObjectReference{{Name: name}}
}

// valueOrDefault returns v when non-empty (after trimming), otherwise def.
func valueOrDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// buildConsensusResources parses the consensus-node container's CPU/memory limits
// and requests from inputs (falling back to the ConsensusDefault* values), returning
// an error on any invalid quantity so install fails with an actionable message
// rather than applying a malformed CR.
func buildConsensusResources(inputs models.ConsensusNodeInputs) (*corev1.ResourceRequirements, error) {
	parse := func(value, def, flag string) (resource.Quantity, error) {
		q, err := resource.ParseQuantity(valueOrDefault(value, def))
		if err != nil {
			return resource.Quantity{}, errorx.IllegalArgument.Wrap(err, "invalid quantity %q for %s", valueOrDefault(value, def), flag)
		}
		return q, nil
	}

	cpuLimit, err := parse(inputs.CPULimit, models.ConsensusDefaultCPULimit, "--cpu-limit")
	if err != nil {
		return nil, err
	}
	memLimit, err := parse(inputs.MemoryLimit, models.ConsensusDefaultMemoryLimit, "--memory-limit")
	if err != nil {
		return nil, err
	}
	cpuRequest, err := parse(inputs.CPURequest, models.ConsensusDefaultCPURequest, "--cpu-request")
	if err != nil {
		return nil, err
	}
	memRequest, err := parse(inputs.MemoryRequest, models.ConsensusDefaultMemoryRequest, "--memory-request")
	if err != nil {
		return nil, err
	}

	return &corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{corev1.ResourceCPU: cpuLimit, corev1.ResourceMemory: memLimit},
		Requests: corev1.ResourceList{corev1.ResourceCPU: cpuRequest, corev1.ResourceMemory: memRequest},
	}, nil
}
