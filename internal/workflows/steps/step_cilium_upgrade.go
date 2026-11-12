// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/cli/values"

	"github.com/hashgraph/solo-weaver/internal/cilium"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/helm"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

const (
	ciliumPreflightRelease = "cilium-preflight"
	ciliumAgentDaemonSet   = "cilium"
	ciliumOperatorName     = "cilium-operator"
	hostLegacyRoutingKey   = "enable-host-legacy-routing"
	lbAccelerationKey      = "bpf-lb-acceleration"
)

// Seams so tests run without a cluster, a chart or the cilium CLI.
var (
	rerenderCiliumConfig = software.ReconfigureCiliumConfig
	newCiliumHopHelm     = newHelmManager
	readCiliumHealth     = defaultReadCiliumHealth
	ciliumHealthTimeout  = 5 * time.Minute
	ciliumPollInterval   = 5 * time.Second
)

// ciliumHop holds what the steps of one Cilium hop share.
type ciliumHop struct {
	from, to  string
	newClient kube.ClientProvider
	chartDir  string
	config    string
}

// CiliumHopSteps are the steps that move the running Cilium from one version
// to the next: a health check, the verified chart and values, Cilium's
// pre-flight image pull, `cilium upgrade`, and a health gate that also checks
// our settings survived. Every step skips once Cilium runs the target.
func CiliumHopSteps(from, to string, newClient kube.ClientProvider) []automa.Builder {
	h := &ciliumHop{from: from, to: to, newClient: newClient}
	return []automa.Builder{
		h.checkHealthyBefore(),
		h.prepare(),
		h.preflight(),
		h.upgrade(),
		h.waitHealthy(),
		guardBandwidthManagerDisabled(),
	}
}

// ciliumHealth is what the health gate reads from the cluster.
type ciliumHealth struct {
	AgentVersion      string
	AgentsDesired     int64
	AgentsUpdated     int64
	AgentsAvailable   int64
	AgentsRolledOut   bool // the DaemonSet controller has seen the latest spec
	OperatorDesired   int64
	OperatorAvailable int64
	HostLegacyRouting string
	LBAcceleration    string
}

// problems lists why h is not a healthy Cilium on version.
func (h ciliumHealth) problems(version string) []string {
	var out []string
	if h.AgentVersion != version {
		out = append(out, fmt.Sprintf("the agent runs %q, not %s", h.AgentVersion, version))
	}
	if !h.AgentsRolledOut || h.AgentsDesired == 0 || h.AgentsUpdated != h.AgentsDesired || h.AgentsAvailable != h.AgentsDesired {
		out = append(out, fmt.Sprintf("agents: %d desired, %d updated, %d available", h.AgentsDesired, h.AgentsUpdated, h.AgentsAvailable))
	}
	if h.OperatorDesired == 0 || h.OperatorAvailable < h.OperatorDesired {
		out = append(out, fmt.Sprintf("operator: %d of %d available", h.OperatorAvailable, h.OperatorDesired))
	}
	if h.HostLegacyRouting != "true" {
		out = append(out, fmt.Sprintf("%s is %q, not \"true\"", hostLegacyRoutingKey, h.HostLegacyRouting))
	}
	if h.LBAcceleration != "disabled" {
		out = append(out, fmt.Sprintf("%s is %q, not \"disabled\"", lbAccelerationKey, h.LBAcceleration))
	}
	return out
}

func defaultReadCiliumHealth(ctx context.Context, k *kube.Client) (ciliumHealth, error) {
	var h ciliumHealth
	var err error
	if h.AgentVersion, _, err = cilium.AgentVersion(ctx, k.Dyn); err != nil {
		return h, err
	}
	ds := func(field ...string) (int64, error) {
		v, _, err := k.GetResourceNestedInt64(ctx, "apps/v1", "DaemonSet", cilium.Namespace, ciliumAgentDaemonSet, field...)
		return v, err
	}
	gen, err := ds("metadata", "generation")
	if err != nil {
		return h, err
	}
	observed, err := ds("status", "observedGeneration")
	if err != nil {
		return h, err
	}
	h.AgentsRolledOut = observed >= gen
	if h.AgentsDesired, err = ds("status", "desiredNumberScheduled"); err != nil {
		return h, err
	}
	if h.AgentsUpdated, err = ds("status", "updatedNumberScheduled"); err != nil {
		return h, err
	}
	if h.AgentsAvailable, err = ds("status", "numberAvailable"); err != nil {
		return h, err
	}
	if h.OperatorDesired, _, err = k.GetResourceNestedInt64(ctx, "apps/v1", "Deployment", cilium.Namespace, ciliumOperatorName, "spec", "replicas"); err != nil {
		return h, err
	}
	if h.OperatorAvailable, _, err = k.GetResourceNestedInt64(ctx, "apps/v1", "Deployment", cilium.Namespace, ciliumOperatorName, "status", "availableReplicas"); err != nil {
		return h, err
	}
	cm := func(key string) (string, error) {
		return k.GetResourceNestedString(ctx, "v1", "ConfigMap", ciliumConfigMapNamespace, ciliumConfigMapName, "data", key)
	}
	if h.HostLegacyRouting, err = cm(hostLegacyRoutingKey); err != nil {
		return h, err
	}
	h.LBAcceleration, err = cm(lbAccelerationKey)
	return h, err
}

// done reports whether Cilium already runs the target and is healthy, so a
// re-run of the hop has nothing to do.
func (h *ciliumHop) done(ctx context.Context, k *kube.Client) bool {
	health, err := readCiliumHealth(ctx, k)
	return err == nil && len(health.problems(h.to)) == 0
}

func (h *ciliumHop) client(stp automa.Step) (*kube.Client, *automa.Report) {
	k, err := h.newClient()
	if err != nil {
		return nil, automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
			"Check that the admin kubeconfig works: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes")))
	}
	return k, nil
}

func (h *ciliumHop) checkHealthyBefore() automa.Builder {
	return automa.NewStepBuilder().WithId("check-cilium-healthy").
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			if h.done(ctx, k) {
				return automa.SkippedReport(stp, automa.WithDetail("Cilium already runs "+h.to))
			}
			health, err := readCiliumHealth(ctx, k)
			if err == nil {
				if p := health.problems(h.from); len(p) > 0 {
					err = errorx.IllegalState.New("Cilium %s is not healthy before the upgrade: %s", h.from, strings.Join(p, "; "))
				}
			}
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.PreconditionNotMet,
					"Check Cilium with: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system get ds cilium deploy cilium-operator",
					"Fix Cilium before upgrading it")))
			}
			return automa.SuccessReport(stp)
		})
}

func (h *ciliumHop) prepare() automa.Builder {
	return automa.NewStepBuilder().WithId("prepare-cilium-upgrade").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "fetching the Cilium "+h.to+" chart...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			if h.done(ctx, k) {
				return automa.SkippedReport(stp, automa.WithDetail("Cilium already runs "+h.to))
			}
			release, err := ciliumReleaseFor(h.to)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			if h.chartDir, err = fetchCiliumChart(ctx, release); err != nil {
				return automa.FailureReport(stp, automa.WithError(err)) // already decorated
			}
			if h.config, err = rerenderCiliumConfig(); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			return automa.SuccessReport(stp)
		})
}

// preflight runs Cilium's pre-flight DaemonSet, which pulls the new agent
// image on every node before the agent restarts, and removes it again.
func (h *ciliumHop) preflight() automa.Builder {
	return automa.NewStepBuilder().WithId("cilium-preflight").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "pre-pulling the Cilium "+h.to+" images...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if h.chartDir == "" {
				return automa.SkippedReport(stp, automa.WithDetail("Cilium already runs "+h.to))
			}
			vals, err := ciliumPreflightValues(h.config)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			hm, err := newCiliumHopHelm()
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
			}
			// A failed earlier run can leave the release behind; Helm will not reuse its name.
			if err := removePreflight(hm); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			_, err = hm.InstallChart(ctx, ciliumPreflightRelease, h.chartDir, h.to, cilium.Namespace, helm.InstallChartOptions{
				ValueOpts: &values.Options{Values: vals},
				Wait:      true,
				Timeout:   ciliumHealthTimeout,
			})
			removeErr := removePreflight(hm)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "Cilium's pre-flight check for %s did not become ready, so Cilium was not upgraded", h.to),
					ReasonHostCommandFailed,
					"Check that the host can pull the Cilium "+h.to+" images: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system get events")))
			}
			if removeErr != nil {
				return automa.FailureReport(stp, automa.WithError(removeErr))
			}
			return automa.SuccessReport(stp)
		})
}

// ciliumPreflightValues are the values of Cilium's documented pre-flight
// install, plus the API server address our config gives Cilium, since the
// cluster runs without kube-proxy. Our full values must not be used: they turn
// on Hubble relay and its certificates, which the cilium release owns.
func ciliumPreflightValues(configPath string) ([]string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, errorx.IllegalState.Wrap(err, "failed to read the Cilium config %s", configPath)
	}
	var cfg struct {
		Host string `yaml:"k8sServiceHost"`
		Port int    `yaml:"k8sServicePort"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "failed to parse the Cilium config %s", configPath)
	}
	if strings.TrimSpace(cfg.Host) == "" || cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, errorx.IllegalFormat.New("the Cilium config %s has no valid k8sServiceHost and k8sServicePort", configPath)
	}
	return []string{
		"preflight.enabled=true", "agent=false", "operator.enabled=false",
		"k8sServiceHost=" + cfg.Host, fmt.Sprintf("k8sServicePort=%d", cfg.Port),
	}, nil
}

func removePreflight(hm helm.Manager) error {
	installed, err := hm.IsInstalled(ciliumPreflightRelease, cilium.Namespace)
	if err == nil && installed {
		err = hm.UninstallChart(ciliumPreflightRelease, cilium.Namespace)
	}
	if err != nil {
		return errx.Decorate(errorx.ExternalError.Wrap(err, "failed to remove the Cilium pre-flight release"),
			reasons.K8sAPIFailed,
			"Remove it by hand: sudo helm --kubeconfig /etc/kubernetes/admin.conf -n kube-system uninstall "+ciliumPreflightRelease)
	}
	return nil
}

// ciliumHopUpgradeCommand pins the version and the verified chart, so the CLI
// neither picks its own default release nor downloads a chart unchecked.
func ciliumHopUpgradeCommand(version, chartDir, config string) string {
	return fmt.Sprintf("/usr/bin/sudo %s/cilium upgrade --version %q --chart-directory %s --values %s --wait",
		models.Paths().SandboxBinDir, version, shellQuote(chartDir), shellQuote(config))
}

func (h *ciliumHop) upgrade() automa.Builder {
	return automa.NewStepBuilder().WithId("upgrade-cilium-cni").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "upgrading Cilium from "+h.from+" to "+h.to+"...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if h.chartDir == "" {
				return automa.SkippedReport(stp, automa.WithDetail("Cilium already runs "+h.to))
			}
			if err := verifyExecutables("cilium"); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			cmd := ciliumHopUpgradeCommand(h.to, h.chartDir, h.config)
			logx.As().Info().Str("from", h.from).Str("to", h.to).Msg("Upgrading Cilium")
			if _, err := runCiliumScript([]string{cmd}, ""); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "'cilium upgrade' to %s failed", h.to),
					ReasonHostCommandFailed,
					"Check the agents: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system get pods -l k8s-app=cilium",
					fmt.Sprintf("To go back to %s, run: sudo %s/cilium upgrade --version %q --values %s --wait", h.from, models.Paths().SandboxBinDir, h.from, h.config))))
			}
			return automa.SuccessReport(stp)
		})
}

func (h *ciliumHop) waitHealthy() automa.Builder {
	return automa.NewStepBuilder().WithId("wait-cilium-healthy").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepDetail(ctx, stp, "waiting for Cilium "+h.to+" to be healthy...")
			return ctx, nil
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			deadline := time.Now().Add(ciliumHealthTimeout)
			var last []string
			for {
				health, err := readCiliumHealth(ctx, k)
				if err != nil {
					last = []string{err.Error()}
				} else if last = health.problems(h.to); len(last) == 0 {
					return automa.SuccessReport(stp, automa.WithDetail("Cilium "+h.to+" is healthy"))
				}
				if time.Now().After(deadline) {
					break
				}
				select {
				case <-ctx.Done():
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(ctx.Err(), reasons.Internal)))
				case <-time.After(ciliumPollInterval):
				}
			}
			return automa.FailureReport(stp, automa.WithError(errx.Decorate(
				errorx.IllegalState.New("Cilium %s is not healthy after %s: %s", h.to, ciliumHealthTimeout, strings.Join(last, "; ")),
				reasons.PreconditionNotMet,
				"Check the agents: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system get pods -l k8s-app=cilium",
				"Check the settings: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system get configmap cilium-config -o yaml")))
		})
}
