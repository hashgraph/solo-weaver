// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	operatorv1alpha1 "github.com/hashgraph/solo-operator/api/v1alpha1"
	"github.com/joomcode/errorx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// EnsureConfigCRs creates the Log4j2Config, NodeSettings, and ApplicationProperties
// CRs required by a ConsensusCapsule.
func EnsureConfigCRs(inputs models.ConsensusNodeInputs) automa.Builder {
	return automa.NewStepBuilder().WithId(EnsureConfigCRsStepId).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			kc, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			scope := fmt.Sprintf("node%d", inputs.NodeId)

			log4j2Content, _ := readDefault("consensus/log4j2.xml")
			if inputs.Log4j2ConfigFile != "" {
				b, err := os.ReadFile(inputs.Log4j2ConfigFile)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(
						errorx.IllegalArgument.Wrap(err, "failed to read log4j2 config file")))
				}
				log4j2Content = string(b)
			}

			settingsContent, _ := readDefault("consensus/settings.txt")
			if inputs.SettingsFile != "" {
				b, err := os.ReadFile(inputs.SettingsFile)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(
						errorx.IllegalArgument.Wrap(err, "failed to read settings file")))
				}
				settingsContent = string(b)
			}

			appPropsContent, _ := readDefault("consensus/application.properties")
			if inputs.ApplicationPropertiesFile != "" {
				b, err := os.ReadFile(inputs.ApplicationPropertiesFile)
				if err != nil {
					return automa.StepFailureReport(stp.Id(), automa.WithError(
						errorx.IllegalArgument.Wrap(err, "failed to read application properties file")))
				}
				appPropsContent = string(b)
			}

			log4j2 := &operatorv1alpha1.Log4j2Config{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion,
					Kind:       "Log4j2Config",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-log4j2", scope),
					Namespace: inputs.Namespace,
				},
				Spec: operatorv1alpha1.Log4j2ConfigSpec{
					Scope:   scope,
					Content: log4j2Content,
					Orbit:   inputs.OrbitName,
				},
			}

			nodeSettings := &operatorv1alpha1.NodeSettings{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion,
					Kind:       "NodeSettings",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-settings", scope),
					Namespace: inputs.Namespace,
				},
				Spec: operatorv1alpha1.NodeSettingsSpec{
					Scope:   scope,
					Content: settingsContent,
					Orbit:   inputs.OrbitName,
				},
			}

			appProps := &operatorv1alpha1.ApplicationProperties{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kube.SoloOperatorGroup + "/" + kube.SoloOperatorVersion,
					Kind:       "ApplicationProperties",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-appprops", scope),
					Namespace: inputs.Namespace,
				},
				Spec: operatorv1alpha1.ApplicationPropertiesSpec{
					Scope:   scope,
					Content: appPropsContent,
					Orbit:   inputs.OrbitName,
				},
			}

			for _, obj := range []interface{ GetName() string }{log4j2, nodeSettings, appProps} {
				logx.As().Info().Str("name", obj.GetName()).Msg("Applying config CR")
			}

			if err := kc.ApplyTyped(ctx, log4j2); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := kc.ApplyTyped(ctx, nodeSettings); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}
			if err := kc.ApplyTyped(ctx, appProps); err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			logx.As().Info().Str("scope", scope).Msg("Config CRs created successfully")
			return automa.StepSuccessReport(stp.Id())
		}).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Creating config CRs (Log4j2, NodeSettings, ApplicationProperties)")
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
					Orbit:                   inputs.OrbitName,
					NodeId:                  inputs.NodeId,
					AccountId:               inputs.AccountId,
					Weight:                  inputs.Weight,
					HapiAppSecretsName:      inputs.HapiAppSecret,
					Log4j2ConfigRef:         fmt.Sprintf("%s-log4j2", scope),
					SettingsConfigRef:       fmt.Sprintf("%s-settings", scope),
					ApplicationPropertiesRef: fmt.Sprintf("%s-appprops", scope),
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
