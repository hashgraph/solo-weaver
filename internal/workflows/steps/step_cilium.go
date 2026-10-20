// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/internal/cilium"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
)

func SetupCilium(mr software.MachineRuntime) *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("setup-cilium-cli").Steps(
		installCilium(software.NewCiliumInstaller, mr),
		configureCilium(software.NewCiliumInstaller, mr),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Setting up Cilium")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to setup Cilium")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cilium setup successfully")
		})
}

func installCilium(provider func(opts ...software.InstallerOption) (software.Software, error), mr software.MachineRuntime) automa.Builder {
	return automa.NewStepBuilder().WithId("install-cilium-cli").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "installing Cilium CLI...")
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
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CLI is already installed"), automa.WithMetadata(meta))
			}

			err = installer.Download()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[DownloadedByThisStep] = "true"

			err = installer.Extract()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			meta[ExtractedByThisStep] = "true"

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
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CLI was not installed by this step, skipping rollback"))
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

func configureCilium(provider func(opts ...software.InstallerOption) (software.Software, error), mr software.MachineRuntime) automa.Builder {
	return automa.NewStepBuilder().WithId("configure-cilium-cli").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "configuring Cilium CLI...")
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
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CLI is already configured"), automa.WithMetadata(meta))
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
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CLI was not configured by this step, skipping rollback"))
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

// ciliumRelease is a Cilium release, the Helm repository serving its chart, and
// the sha256 of that chart archive.
type ciliumRelease struct {
	Version     string
	ChartRepo   string
	ChartSHA256 string
}

// newClusterCilium is the Cilium release a new cluster gets. Clusters that
// already run Cilium keep their release: installCiliumCNI leaves them alone.
var newClusterCilium = ciliumRelease{
	Version:     "1.20.2",
	ChartRepo:   "https://helm.cilium.io",
	ChartSHA256: "b2afd87b7f75f875f92a14559f14f59b7babbb479d968e3fd625a20bf30ec20e",
}

const (
	ciliumChartRepoAlias = "cilium"
	ciliumChartRef       = ciliumChartRepoAlias + "/cilium"
)

func StartCilium() *automa.WorkflowBuilder {
	return automa.NewWorkflowBuilder().WithId("start-cilium").Steps(
		installCiliumCNI(newClusterCilium, kube.NewClient),
		guardBandwidthManagerDisabled(),
	).
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Starting Cilium")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Failed to start Cilium")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Cilium started successfully")
		})
}

// installCiliumCNI installs Cilium only on a cluster with no trace of it. Any
// existing Cilium resource, healthy or not, means an install already happened,
// and installing again could overwrite or duplicate it. A failed lookup is
// never read as "absent".
//
// TODO to be replaced with helm invocation
func installCiliumCNI(release ciliumRelease, newClient kube.ClientProvider) *automa.StepBuilder {
	version := release.Version

	return automa.NewStepBuilder().WithId("install-cilium-cni").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "installing Cilium CNI...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}

			// Decide from the Kubernetes API before touching the cilium binary, so
			// an existing cluster is left alone even if the CLI is missing or bad.
			kc, err := newClient()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.Decorate(err, "cannot reach the Kubernetes API to check for an existing Cilium, so 'cilium install' was not run"),
					reasons.K8sAPIFailed,
					"Check that the admin kubeconfig exists and the API server is up: kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes")))
			}

			existing, err := cilium.DetectInstallation(ctx, kc.Dyn)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(
					errorx.Decorate(err, "cannot tell whether Cilium is already installed, so 'cilium install' was not run")))
			}
			if existing.Found() {
				meta[AlreadyInstalled] = "true"
				meta[CiliumResources] = strings.Join(existing.Resources, ",")
				logx.As().Info().Str("step_id", stp.Id()).Strs("resources", existing.Resources).
					Msg("Cilium is already installed; leaving it unchanged")
				return automa.SkippedReport(stp,
					automa.WithDetail(fmt.Sprintf("Cilium is already installed (found %s); leaving it unchanged", strings.Join(existing.Resources, ", "))),
					automa.WithMetadata(meta))
			}

			logx.As().Info().Str("step_id", stp.Id()).Str("software", "cilium-cni").Str("version", version).Msgf("cilium-cni version: %s", version)

			// Verify the cilium binary before executing it.
			if err := verifyExecutables("cilium"); err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}

			chartDir, removeChart, err := fetchCiliumChart(ctx, release)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}
			defer removeChart()

			// --chart-directory overrides --version, so the chart that is
			// installed is exactly the one verified above.
			installScript := []string{
				fmt.Sprintf("/usr/bin/sudo %s/cilium install --wait --version \"%s\" --chart-directory %s --values %s/etc/weaver/cilium-config.yaml",
					models.Paths().SandboxBinDir, version, chartDir, models.Paths().SandboxDir),
			}
			_, err = runCiliumScript(installScript, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err), automa.WithMetadata(meta))
			}

			meta[InstalledByThisStep] = "true"
			stp.State().Local().Set(InstalledByThisStep, true)

			return automa.SuccessReport(stp, automa.WithMetadata(meta))
		}).
		WithRollback(func(ctx context.Context, stp automa.Step) *automa.Report {
			var installedByThisStep bool
			if v, ok := stp.State().Local().Bool(InstalledByThisStep); ok {
				installedByThisStep = v
			}
			if !installedByThisStep {
				return automa.SkippedReport(stp, automa.WithDetail("Cilium CNI was not installed by this step, skipping rollback"))
			}

			// Verify the cilium binary before executing it on rollback.
			if err := verifyExecutables("cilium"); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			// Uninstall Cilium CNI
			scripts := []string{
				fmt.Sprintf("/usr/bin/sudo %s/cilium uninstall", models.Paths().SandboxBinDir),
			}
			_, err := runCiliumScript(scripts, "")
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}

			return automa.SuccessReport(stp)
		})
}

// runCiliumScript runs cilium CLI commands. A var so tests can record them.
var runCiliumScript = automa_steps.RunBashScript

// fetchCiliumChart returns the unpacked, verified chart directory and a func
// that removes it. A var so tests can avoid the network.
var fetchCiliumChart = func(ctx context.Context, release ciliumRelease) (string, func(), error) {
	hm, err := newHelmManager()
	if err != nil {
		return "", nil, err
	}
	return pullVerifiedCiliumChart(ctx, hm, release, os.TempDir())
}

// pullVerifiedCiliumChart downloads the release's chart into a fresh private
// directory under base, checks it against the pinned checksum and unpacks it
// for `cilium install --chart-directory`. The CLI bundles charts only up to its
// own default release and would otherwise download a newer one itself, without
// any checksum check.
//
// Only root can change the work directory, so the chart `cilium install` reads
// is the one that was verified.
func pullVerifiedCiliumChart(ctx context.Context, hm helm.Manager, release ciliumRelease, base string) (string, func(), error) {
	workDir, err := privateWorkDir(base, "solo-provisioner-cilium-chart-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(workDir) }

	chartDir, err := pullAndUnpackCiliumChart(ctx, hm, release, workDir)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return chartDir, cleanup, nil
}

func pullAndUnpackCiliumChart(ctx context.Context, hm helm.Manager, release ciliumRelease, workDir string) (string, error) {
	if _, err := hm.AddRepo(ciliumChartRepoAlias, release.ChartRepo, helm.RepoAddOptions{}); err != nil {
		return "", chartDownloadError(err, release)
	}

	archive, err := hm.PullAndVerify(ctx, workDir, ciliumChartRef, release.Version, "sha256", release.ChartSHA256)
	if err != nil {
		if errorx.IsOfType(err, helm.ErrChecksumMismatch) {
			return "", chartIntegrityError(err, release)
		}
		return "", chartDownloadError(err, release)
	}

	return unpackVerifiedChart(archive, release, workDir)
}

// unpackVerifiedChart unpacks exactly the bytes it has checked: the archive is
// read once, hashed against the pinned digest, and its Chart.yaml must name the
// release, because --chart-directory makes the CLI ignore --version.
func unpackVerifiedChart(archive string, release ciliumRelease, dest string) (string, error) {
	data, err := os.ReadFile(archive)
	if err != nil {
		return "", unpackError(errorx.ExternalError.Wrap(err, "failed to read the Cilium chart %s", archive), dest)
	}

	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, release.ChartSHA256) {
		return "", chartIntegrityError(errorx.IllegalState.New(
			"Cilium chart %s has sha256 %s, expected %s", archive, got, release.ChartSHA256), release)
	}

	ch, err := loader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return "", chartIntegrityError(errorx.IllegalFormat.Wrap(err, "cannot load the Cilium chart %s", archive), release)
	}
	if ch.Metadata.Name != "cilium" || ch.Metadata.Version != release.Version {
		return "", chartIntegrityError(errorx.IllegalState.New(
			"Cilium chart %s is %s %s, expected cilium %s", archive, ch.Metadata.Name, ch.Metadata.Version, release.Version), release)
	}

	if err := chartutil.Expand(dest, bytes.NewReader(data)); err != nil {
		return "", unpackError(errorx.ExternalError.Wrap(err, "failed to unpack the verified Cilium chart %s", archive), dest)
	}
	return filepath.Join(dest, ch.Metadata.Name), nil
}

// privateWorkDir creates a 0700 directory under base and checks that no other
// user can replace it or any directory above it: each must be owned by root or
// by this process, and writable by group or others only with the sticky bit.
func privateWorkDir(base, pattern string) (string, error) {
	dir, err := os.MkdirTemp(base, pattern)
	if err != nil {
		return "", unpackError(errorx.ExternalError.Wrap(err, "failed to create a work directory under %s", base), base)
	}

	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil {
		err = checkNotReplaceable(resolved)
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", errx.Decorate(err, reasons.PreconditionNotMet,
			"Use a temporary directory only root can modify, for example unset TMPDIR so /tmp is used",
			fmt.Sprintf("Check the owners and modes of %s and its parents: namei -l %s", base, base))
	}
	return dir, nil
}

func checkNotReplaceable(dir string) error {
	for p := dir; ; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err != nil {
			return errorx.ExternalError.Wrap(err, "failed to inspect %s", p)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return errorx.IllegalState.New("cannot read the owner of %s", p)
		}
		if st.Uid != 0 && int(st.Uid) != os.Geteuid() {
			return errorx.IllegalState.New("%s is owned by uid %d, so another user could replace the Cilium chart", p, st.Uid)
		}
		mode := fi.Mode()
		writableByOthers := mode.Perm()&0o022 != 0
		if p == dir && mode.Perm()&0o077 != 0 {
			return errorx.IllegalState.New("%s has mode %s, expected no group or other access", p, mode.Perm())
		}
		if writableByOthers && mode&os.ModeSticky == 0 {
			return errorx.IllegalState.New("%s has mode %s: writable by group or others without the sticky bit, so the Cilium chart could be replaced", p, mode.Perm())
		}
		if parent := filepath.Dir(p); parent == p {
			return nil
		}
	}
}

func chartIntegrityError(err error, release ciliumRelease) error {
	return errx.Decorate(
		errorx.Decorate(err, "the downloaded Cilium chart failed verification, so 'cilium install' was not run"),
		reasons.IntegrityCheckFailed,
		fmt.Sprintf("The pinned sha256 for cilium %s is %s", release.Version, release.ChartSHA256),
		fmt.Sprintf("Compare with the digest %s publishes for cilium %s: curl -s %s/index.yaml", release.ChartRepo, release.Version, release.ChartRepo))
}

func unpackError(err error, dir string) error {
	return errx.Decorate(err, reasons.PreconditionNotMet,
		fmt.Sprintf("Check that %s is writable and the disk has free space: df -h %s", dir, dir))
}

func chartDownloadError(err error, release ciliumRelease) error {
	return errx.Decorate(
		errorx.Decorate(err, "failed to download the Cilium %s chart, so 'cilium install' was not run", release.Version),
		reasons.DownloadFailed,
		fmt.Sprintf("Check that %s is reachable from this host, including any proxy: curl -sSI %s/index.yaml", release.ChartRepo, release.ChartRepo),
		"Re-run 'solo-provisioner kube cluster install' once the repository is reachable")
}

// Cilium agent config surfaces the Bandwidth Manager guard inspects. The
// cilium-config ConfigMap is created by `cilium install` and read by the agent.
const (
	ciliumConfigMapNamespace = "kube-system"
	ciliumConfigMapName      = "cilium-config"
	// bandwidthManagerConfigKey is the cilium-config data key set by the Cilium
	// agent flag --enable-bandwidth-manager. "true" when enabled; "false" or
	// absent when disabled.
	bandwidthManagerConfigKey = "enable-bandwidth-manager"
)

// bandwidthManagerEnabled reports whether a cilium-config enable-bandwidth-manager
// value means the Bandwidth Manager is on. An empty value (key absent) is disabled.
func bandwidthManagerEnabled(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// ciliumBandwidthManagerState reads the cilium-config ConfigMap via the Kubernetes
// API (pure Go, no shell) and reports whether the Bandwidth Manager is enabled
// along with the raw enable-bandwidth-manager value. Mirrors the cilium-config
// read in migration_cilium_acceleration.go. It errors if cilium-config is absent,
// since the guard cannot otherwise verify the precondition.
func ciliumBandwidthManagerState(ctx context.Context, k *kube.Client) (enabled bool, value string, err error) {
	exists, err := k.ResourceExists(ctx, "v1", "ConfigMap", ciliumConfigMapNamespace, ciliumConfigMapName)
	if err != nil {
		return false, "", errorx.ExternalError.Wrap(err, "failed to check for the %q ConfigMap in namespace %q", ciliumConfigMapName, ciliumConfigMapNamespace).
			WithProperty(models.ErrPropertyResolution, []string{
				"Ensure the cluster API is reachable (kubeconfig valid, API server up):",
				"  kubectl -n kube-system get configmap cilium-config",
			})
	}
	if !exists {
		return false, "", errorx.IllegalState.New("Cilium ConfigMap %q not found in namespace %q", ciliumConfigMapName, ciliumConfigMapNamespace).
			WithProperty(models.ErrPropertyResolution, []string{
				"Confirm Cilium is installed and its ConfigMap exists:",
				"  kubectl -n kube-system get configmap cilium-config -o yaml",
			})
	}

	value, err = k.GetResourceNestedString(ctx, "v1", "ConfigMap", ciliumConfigMapNamespace, ciliumConfigMapName, "data", bandwidthManagerConfigKey)
	if err != nil {
		return false, "", errorx.ExternalError.Wrap(err, "failed to read %q from the %q ConfigMap", bandwidthManagerConfigKey, ciliumConfigMapName).
			WithProperty(models.ErrPropertyResolution, []string{
				"Ensure the cluster API is reachable (kubeconfig valid, API server up):",
				"  kubectl -n kube-system get configmap cilium-config -o yaml",
			})
	}
	return bandwidthManagerEnabled(value), value, nil
}

// guardBandwidthManagerDisabled fails fast if Cilium's Bandwidth Manager is
// enabled. Bandwidth Manager is the only Cilium BPF writer of skb->priority, so
// the BN traffic shaper's egress-priority survival guarantee is void whenever it
// is on (its BPF writes to skb->priority void the traffic shaper's egress guarantee). It reads cilium-config through the
// Kubernetes API (pure Go, no shell) and is read-only, so there is no rollback.
func guardBandwidthManagerDisabled() automa.Builder {
	return automa.NewStepBuilder().WithId("guard-bandwidth-manager-disabled").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "verifying Cilium Bandwidth Manager is disabled...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			meta := map[string]string{}
			k, err := kube.NewClient()
			if err != nil {
				return automa.StepFailureReport(stp.Id(), automa.WithError(err))
			}

			enabled, value, err := ciliumBandwidthManagerState(ctx, k)
			if err != nil {
				// ciliumBandwidthManagerState types its errors: ExternalError for
				// kube-API failures, IllegalState for a missing ConfigMap. Forward
				// as-is so the classification (and resolution hints) are preserved.
				return automa.FailureReport(stp,
					automa.WithError(err),
					automa.WithMetadata(meta))
			}

			meta[BandwidthManagerStatus] = value

			if enabled {
				return automa.FailureReport(stp,
					automa.WithError(errorx.IllegalState.New(
						"Cilium Bandwidth Manager is enabled (%s=%q) — it is the only Cilium BPF writer of skb->priority and would void the BN egress-priority guarantee; it must be Disabled", bandwidthManagerConfigKey, value).
						WithProperty(models.ErrPropertyResolution, []string{
							"Disable the Cilium Bandwidth Manager, then re-run the install:",
							"  set 'bandwidthManager.enabled: false' in the Cilium Helm values and run 'cilium upgrade'",
							"Verify with: kubectl -n kube-system get configmap cilium-config -o jsonpath='{.data.enable-bandwidth-manager}'",
						})),
					automa.WithMetadata(meta))
			}

			return automa.SuccessReport(stp,
				automa.WithDetail("Cilium Bandwidth Manager is disabled"),
				automa.WithMetadata(meta))
		})
}
