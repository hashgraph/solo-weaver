// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	EnsureOrbitStepId            = "ensure-orbit"
	EnsureConfigCRsStepId        = "ensure-config-crs"
	CreateConsensusCapsuleStepId = "create-consensus-capsule"
)

// CapsuleKubeClient is the subset of *kube.Client used by the consensus capsule
// steps. Depending on this narrow interface (rather than *kube.Client) lets the
// steps be unit-tested with a fake, mirroring reality.ConsensusKubeClient.
type CapsuleKubeClient interface {
	ResourceExists(ctx context.Context, apiVersion, kind, namespace, name string) (bool, error)
	GetResourceNestedString(ctx context.Context, apiVersion, kind, namespace, name string, fields ...string) (string, error)
	ApplyTyped(ctx context.Context, obj runtime.Object) error
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

			l := logx.As()
			apiVersion := kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion

			apply := func(e configEntry) error {
				obj := e.builder(scope, e.content, inputs.OrbitName, inputs.Namespace, e.crName)
				if err := kc.ApplyTyped(ctx, obj); err != nil {
					return errx.Decorate(
						errorx.IllegalState.Wrap(err, "failed to apply %s %s", e.kind, e.crName),
						reasons.PreconditionNotMet,
						"Verify cluster connectivity and that your kubeconfig has RBAC to create/update solo-operator config CRs",
						"Check the solo-operator is running: solo-provisioner consensus node install --upgrade-operator")
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
					HapiAppSecretsName:               inputs.HapiAppSecret,
					Log4j2ConfigRef:                  fmt.Sprintf("%s-log4j2", scope),
					SettingsConfigRef:                fmt.Sprintf("%s-settings", scope),
					ApplicationPropertiesRef:         fmt.Sprintf("%s-appprops", scope),
					ApplicationOverridePropertiesRef: fmt.Sprintf("%s-appoverride", scope),
					ApiPermissionPropertiesRef:       fmt.Sprintf("%s-apiperm", scope),
					BootstrapPropertiesRef:           fmt.Sprintf("%s-bootstrap", scope),
					NodePropertiesRef:                fmt.Sprintf("%s-nodeprops", scope),
					FeeSchedulesRef:                  fmt.Sprintf("%s-feeschedules", scope),
					SimpleFeesSchedulesRef:           fmt.Sprintf("%s-simplefeesschedules", scope),
					ThrottlesConfigRef:               fmt.Sprintf("%s-throttles", scope),
					BlockNodesConfigRef:              fmt.Sprintf("%s-blocknodes", scope),
					PodProperties: operatorv1alpha1.ConsensusPodProperties{
						Containers: operatorv1alpha1.ConsensusContainers{
							ConsensusNode: operatorv1alpha1.ConsensusNodeContainer{
								SoftwareVersion: &operatorv1alpha1.SoftwareVersion{
									Repository: inputs.ConsensusImageRepo,
									ImageTag:   inputs.ConsensusImageTag,
								},
							},
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
