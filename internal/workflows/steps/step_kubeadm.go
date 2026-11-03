// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// runBashScript, configureKubeconfig and kubeconfigCopiesMissing are package
// vars so tests can stub kubeadm and the admin kubeconfig copies.
var (
	runBashScript       = automa_steps.RunBashScript
	configureKubeconfig = func() error {
		kubeConfigManager, err := kube.NewKubeConfigManager()
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to create kubeconfig manager")
		}
		return kubeConfigManager.Configure()
	}
	kubeconfigCopiesMissing = func() bool {
		for _, p := range []string{"/root/.kube/config", path.Join(config.WeaverHomeDir(), ".kube", "config")} {
			if _, err := os.Stat(p); err != nil {
				return true
			}
		}
		return false
	}
)

func SetupKubeadm(mr software.MachineRuntime) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("setup-kubeadm").
		Steps(
			installKubeadm(software.NewKubeadmInstaller, mr),
			configureKubeadm(software.NewKubeadmInstaller, mr),
		).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up kubeadm")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup kubeadm")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "kubeadm setup successfully")
		})
}

func installKubeadm(provider func(opts ...software.InstallerOption) (software.Software, error), mr software.MachineRuntime) automa.Builder {
	return automa.NewStepBuilder().WithId("install-kubeadm").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "installing kubeadm...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider(software.WithMachineRuntime(mr))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			logx.As().Info().Str("step_id", stp.Id()).Str("software", installer.GetSoftwareName()).Str("version", installer.Version()).Msgf("%s version: %s", installer.GetSoftwareName(), installer.Version())

			// Prepare metadata for reporting
			meta := map[string]string{}

			installed, err := installer.IsInstalled()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if installed {
				meta[AlreadyInstalled] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("kubeadm is already installed"), automa.WithMetadata(meta))
			}

			err = installer.Download()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[DownloadedByThisStep] = "true"

			err = installer.Install()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[InstalledByThisStep] = "true"
			stp.State().Local().Set(InstalledByThisStep, true)

			err = installer.Cleanup()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[CleanedUpByThisStep] = "true"

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var installedByThisStep bool
			if v, ok := stp.State().Local().Bool(InstalledByThisStep); ok {
				installedByThisStep = v
			}

			if !installedByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("kubeadm was not installed by this step, skipping rollback"))
			}

			installer, err := provider(software.WithMachineRuntime(mr))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = installer.Uninstall()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

func configureKubeadm(provider func(opts ...software.InstallerOption) (software.Software, error), mr software.MachineRuntime) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-kubeadm").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "configuring kubeadm...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			installer, err := provider(software.WithMachineRuntime(mr))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			// Prepare metadata for reporting
			meta := map[string]string{}

			configured, err := installer.IsConfigured()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			if configured {
				meta[AlreadyConfigured] = "true"
				return automa.SkippedReport(stp, automa.WithDetail("kubeadm is already configured"), automa.WithMetadata(meta))
			}

			err = installer.Configure()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}

			meta[ConfiguredByThisStep] = "true"
			stp.State().Local().Set(ConfiguredByThisStep, true)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var configuredByThisStep bool
			if v, ok := stp.State().Local().Bool(ConfiguredByThisStep); ok {
				configuredByThisStep = v
			}

			if !configuredByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("kubeadm was not configured by this step, skipping rollback"))
			}

			installer, err := provider(software.WithMachineRuntime(mr))
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			err = installer.RemoveConfiguration()
			if err != nil {
				return automa.FailureReport(stp,
					automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

// InitializeCluster checks cluster status and performs initialization only if needed
func InitializeCluster() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("init-cluster").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Initializing Kubernetes cluster")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to initialize Kubernetes cluster")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Kubernetes cluster initialized successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// A cluster whose API server answers through the admin kubeconfig is
			// already initialized; it is outside this step's rollback scope.
			if err := probeAPIServer(adminKubeconfigPath); err == nil {
				// Later steps read the per-user copies of admin.conf. Only missing ones are
				// restored, so an operator's edited copy is not overwritten on every run.
				if kubeconfigCopiesMissing() {
					if err := configureKubeconfig(); err != nil {
						return automa.FailureReport(stp, automa.WithError(kubeconfigCopyError(err)))
					}
				}
				return automa.SkippedReport(stp,
					automa.WithDetail("Kubernetes cluster is already initialized"))
			}

			// A control plane that is on disk but not answering is not this run's
			// either: never initialize over it, so there is nothing to reset later.
			cp, err := detectControlPlane(kubernetesConfigDirs())
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err,
					reasons.PreconditionNotMet,
					"Check that /etc/kubernetes is readable, then re-run this command")))
			}
			if len(cp.evidence) > 0 {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.IllegalState.New("an existing Kubernetes control plane was found (%s), but its API server is not reachable; not initializing over it", strings.Join(cp.evidence, ", ")),
					reasons.PreconditionNotMet,
					"Check the control plane with 'sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get --raw /readyz' and 'sudo systemctl status kubelet crio'",
					"If the cluster is no longer needed, run 'solo-provisioner kube cluster uninstall' and remove what is left under /etc/kubernetes before installing")))
			}

			// Verify the kubeadm binary before executing it.
			if err := verifyExecutables("kubeadm"); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			// Step 1: Pull kubeadm images
			logx.As().Info().Msg("Pulling kubeadm images, this may take a while...")
			pullImageCmd := []string{
				fmt.Sprintf("sudo %s/kubeadm config images pull --config %s/etc/weaver/kubeadm-init.yaml", models.Paths().SandboxBinDir, models.Paths().SandboxDir),
			}

			_, err = runBashScript(pullImageCmd, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to pull kubeadm images"),
					ReasonHostCommandFailed,
					fmt.Sprintf("Re-run '%s' by hand to see which image failed", pullImageCmd[0]),
					"Check that the host can reach the image registry")))
			}
			logx.As().Info().Msg("Kubeadm images pulled successfully.")

			// Step 2: Initialize cluster with kubeadm
			initCmd := []string{
				fmt.Sprintf("sudo %s/kubeadm init --upload-certs --config %s/etc/weaver/kubeadm-init.yaml --v=3", models.Paths().SandboxBinDir, models.Paths().SandboxDir),
			}

			// kubeadm init fails preflight without writing anything, so a preflight
			// failure must not put the host in the rollback scope: kubeadm reset would
			// then wipe leftovers (/var/lib/etcd, a running kubelet) this run did not make.
			preflightCmd := fmt.Sprintf("sudo %s/kubeadm init phase preflight --config %s/etc/weaver/kubeadm-init.yaml", models.Paths().SandboxBinDir, models.Paths().SandboxDir)
			if _, err := runBashScript([]string{preflightCmd}, ""); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "kubeadm preflight checks failed, so the cluster was not initialized"),
					reasons.PreconditionNotMet,
					fmt.Sprintf("Re-run '%s' by hand: its [ERROR ...] lines name the port, directory or service in the way", preflightCmd),
					"If they are leftovers of an earlier cluster, run 'solo-provisioner kube cluster uninstall' before installing")))
			}

			// Whatever kubeadm init writes from here is this run's to undo, since an
			// init that fails after preflight does not clean up after itself.
			stp.State().Local().Set(InitializedByThisStep, true)
			_, err = runBashScript(initCmd, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "failed to initialize cluster with kubeadm"),
					ReasonHostCommandFailed,
					"Check 'sudo journalctl -u kubelet' and 'sudo crictl ps -a' for why the control plane did not start")))
			}

			// Step 3: Configure kubeconfig
			if err := configureKubeconfig(); err != nil {
				return automa.FailureReport(stp, automa.WithError(kubeconfigCopyError(err)))
			}

			return automa.SuccessReport(stp, automa.WithDetail("Cluster initialized successfully"))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			if initialized, _ := stp.State().Local().Bool(InitializedByThisStep); !initialized {
				return automa.SkippedReport(stp,
					automa.WithDetail("Kubernetes cluster was not initialized by this step, skipping rollback"))
			}

			// Verify the kubeadm binary before executing it on rollback.
			if err := verifyExecutables("kubeadm"); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			resetCmd := fmt.Sprintf("sudo %s/kubeadm reset --force --cri-socket unix:///opt/solo/weaver/sandbox/var/run/crio/crio.sock", models.Paths().SandboxBinDir)

			_, err := runBashScript([]string{resetCmd}, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "kubeadm reset failed, so the cluster this run initialized is still on the host"),
					ReasonHostCommandFailed,
					fmt.Sprintf("Run '%s' by hand and check its output", resetCmd))))
			}

			return automa.SuccessReport(stp)
		})
}

func kubeconfigCopyError(err error) error {
	return errx.Decorate(
		errorx.ExternalError.Wrap(err, "failed to copy /etc/kubernetes/admin.conf to the user kubeconfigs"),
		reasons.PermissionDenied,
		"Check that /etc/kubernetes/admin.conf exists and that /root/.kube and the weaver user's ~/.kube are writable")
}

// ResetCluster runs kubeadm reset to tear down the Kubernetes cluster
func ResetCluster() *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("reset-cluster").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Resetting Kubernetes cluster")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to reset Kubernetes cluster")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Kubernetes cluster reset successfully")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			// An absent kubeadm means there is nothing to reset with, so teardown
			// continues. Any other verification failure must stop the uninstall:
			// reporting success would claim a reset that never ran.
			if err := verifyExecutables("kubeadm"); err != nil {
				kubeadmPath := path.Join(models.Paths().SandboxBinDir, "kubeadm")
				if errorx.IsOfType(err, software.FileNotFoundError) {
					logx.As().Warn().Err(err).Msg("kubeadm binary not found, skipping kubeadm reset and continuing with teardown")
					return automa.SuccessReport(stp)
				}
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.Decorate(err, "kubeadm failed verification, so the Kubernetes reset was not run"),
					reasons.PreconditionNotMet,
					fmt.Sprintf("Check '%s': it does not match a known kubeadm release, so it was not run", kubeadmPath),
					"Replace it with the official kubeadm binary for the installed Kubernetes version, then re-run 'solo-provisioner kube cluster uninstall'")))
			}

			scripts := []string{
				fmt.Sprintf("sudo %s/kubeadm reset --force --cri-socket unix:///opt/solo/weaver/sandbox/var/run/crio/crio.sock", models.Paths().SandboxBinDir),
			}

			_, err := automa_steps.RunBashScript(scripts, "")
			if err != nil {
				logx.As().Warn().Err(err).Msg("kubeadm reset failed, continuing with teardown")
				// Don't fail if kubeadm reset fails - cluster might not be initialized
			}

			return automa.SuccessReport(stp)
		})
}
