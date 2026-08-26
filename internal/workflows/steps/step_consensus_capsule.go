// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	EnsureOrbitStepId            = "ensure-orbit"
	EnsureConfigCRsStepId        = "ensure-config-crs"
	CreateConsensusCapsuleStepId = "create-consensus-capsule"
)

func readDefault(path string) (string, error) {
	b, err := templates.Files.ReadFile("files/" + path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EnsureOrbit creates or updates the Orbit CR (cluster-scoped).
func EnsureOrbit(inputs models.ConsensusNodeInputs) automa.Builder {
	return automa.NewStepBuilder().WithId(EnsureOrbitStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			l := logx.As()
			kc, err := kube.NewClient()
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

// resolveContent returns config file content using 2-tier precedence:
// deployment package file > embedded default.
func resolveContent(pkgPath, embeddedPath string) (string, error) {
	if pkgPath != "" {
		if b, err := os.ReadFile(pkgPath); err == nil {
			return string(b), nil
		}
	}
	return readDefault(embeddedPath)
}

// pkgFile returns a deployment-package-relative path if the dir is set, empty otherwise.
func pkgFile(pkgDir string, relPath string) string {
	if pkgDir == "" {
		return ""
	}
	return filepath.Join(pkgDir, relPath)
}

// EnsureConfigCRs creates all 11 config CRs required by a ConsensusCapsule.
// Resolution precedence: deployment package file > embedded default.
func EnsureConfigCRs(inputs models.ConsensusNodeInputs) automa.Builder {
	return automa.NewStepBuilder().WithId(EnsureConfigCRsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			scope := fmt.Sprintf("node%d", inputs.NodeId)
			pkg := inputs.DeploymentPackageDir

			type configEntry struct {
				pkgRelPath   string
				embeddedPath string
				crName       string
				kind         string
				builder      func(scope, content, orbit, ns, name string) runtime.Object
			}

			entries := []configEntry{
				{"log4j2.xml", "consensus/log4j2.xml",
					fmt.Sprintf("%s-log4j2", scope), "Log4j2Config",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.Log4j2Config{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "Log4j2Config"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.Log4j2ConfigSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"settings.txt", "consensus/settings.txt",
					fmt.Sprintf("%s-settings", scope), "NodeSettings",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.NodeSettings{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "NodeSettings"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.NodeSettingsSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"data/config/application.properties", "consensus/application.properties",
					fmt.Sprintf("%s-appprops", scope), "ApplicationProperties",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.ApplicationProperties{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "ApplicationProperties"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.ApplicationPropertiesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"data/config/application-override.properties", "consensus/application-override.properties",
					fmt.Sprintf("%s-appoverride", scope), "ApplicationOverrideProperties",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.ApplicationOverrideProperties{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "ApplicationOverrideProperties"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.ApplicationOverridePropertiesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"data/config/api-permission.properties", "consensus/api-permission.properties",
					fmt.Sprintf("%s-apiperm", scope), "ApiPermissionProperties",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.ApiPermissionProperties{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "ApiPermissionProperties"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.ApiPermissionPropertiesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"data/config/bootstrap.properties", "consensus/bootstrap.properties",
					fmt.Sprintf("%s-bootstrap", scope), "BootstrapProperties",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.BootstrapProperties{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "BootstrapProperties"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.BootstrapPropertiesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"data/config/node.properties", "consensus/node.properties",
					fmt.Sprintf("%s-nodeprops", scope), "NodePropertiesConfig",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.NodePropertiesConfig{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "NodePropertiesConfig"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.NodePropertiesConfigSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"data/config/feeSchedules.json", "consensus/feeSchedules.json",
					fmt.Sprintf("%s-feeschedules", scope), "FeeSchedules",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.FeeSchedules{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "FeeSchedules"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.FeeSchedulesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"data/config/simpleFeesSchedules.json", "consensus/simpleFeesSchedules.json",
					fmt.Sprintf("%s-simplefeesschedules", scope), "SimpleFeesSchedules",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.SimpleFeesSchedules{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "SimpleFeesSchedules"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.SimpleFeesSchedulesSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{"data/config/throttles.json", "consensus/throttles.json",
					fmt.Sprintf("%s-throttles", scope), "ThrottlesConfig",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.ThrottlesConfig{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "ThrottlesConfig"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.ThrottlesConfigSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
				{fmt.Sprintf("block-nodes/config/block-nodes-%d.json", inputs.NodeId),
					"consensus/block-nodes.json",
					fmt.Sprintf("%s-blocknodes", scope), "BlockNodesConfig",
					func(scope, content, orbit, ns, name string) runtime.Object {
						return &operatorv1alpha1.BlockNodesConfig{
							TypeMeta:   metav1.TypeMeta{APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion, Kind: "BlockNodesConfig"},
							ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
							Spec:       operatorv1alpha1.BlockNodesConfigSpec{Scope: scope, Content: content, Orbit: orbit},
						}
					}},
			}

			for _, e := range entries {
				content, err := resolveContent(pkgFile(pkg, e.pkgRelPath), e.embeddedPath)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(err))
				}

				obj := e.builder(scope, content, inputs.OrbitName, inputs.Namespace, e.crName)
				logx.As().Info().Str("name", e.crName).Str("kind", e.kind).Msg("Applying config CR")

				if err := kc.ApplyTyped(ctx, obj); err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(
						errorx.IllegalState.Wrap(err, "failed to apply %s %s", e.kind, e.crName)))
				}
			}

			logx.As().Info().Str("scope", scope).Int("count", len(entries)).Msg("All config CRs created")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating all 11 config CRs")
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
func CreateConsensusCapsule(inputs models.ConsensusNodeInputs) automa.Builder {
	return automa.NewStepBuilder().WithId(CreateConsensusCapsuleStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			scope := fmt.Sprintf("node%d", inputs.NodeId)
			capsuleName := fmt.Sprintf("%s-consensus-%d", inputs.OrbitName, inputs.NodeId)

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
