// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/proxy"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	sysos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// UpgradeCordonAnnotation marks a node the Kubernetes hop cordoned, so only
// that cordon is lifted afterwards and an operator's own cordon stays.
const UpgradeCordonAnnotation = "solo-provisioner/cordoned-for-upgrade"

// controlPlaneComponents run at the Kubernetes version; etcd has its own.
var controlPlaneComponents = []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler"}

// Seams so tests run without a cluster, systemd or kubeadm.
var (
	readKubeHealth      = defaultReadKubeHealth
	setNodeCordon       = defaultSetNodeCordon
	stopService         = sysos.StopService
	startService        = sysos.StartService
	restartService      = sysos.RestartService
	daemonReload        = sysos.DaemonReload
	removeCrioRelease   = software.RemoveCrioReleaseFiles
	installRegistries   = proxy.InstallRegistriesConf
	kubeHealthTimeout   = 5 * time.Minute
	kubeStartTimeout    = 2 * time.Minute
	kubePollInterval    = 5 * time.Second
	kubeHopInstallers   = defaultKubeHopInstallers
	containerRegistryOf = func() string { return config.Get().Proxy.ContainerRegistryProxy }
)

// kubeHopArtifact is one host artifact the hop moves.
type kubeHopArtifact struct {
	name     string
	version  string
	archived bool
	provider func(opts ...software.InstallerOption) (software.Software, error)
}

func defaultKubeHopInstallers(kube, crio string) []kubeHopArtifact {
	return []kubeHopArtifact{
		{software.KubeadmBinaryName, kube, false, software.NewKubeadmInstaller},
		{software.CrioArtifactName, crio, true, software.NewCrioInstaller},
		{software.KubeletBinaryName, kube, false, software.NewKubeletInstaller},
		{software.KubectlBinaryName, kube, false, software.NewKubectlInstaller},
	}
}

// kubeHop holds what the steps of one Kubernetes hop share.
type kubeHop struct {
	from, to, crio string
	newClient      kube.ClientProvider
	artifacts      []kubeHopArtifact
}

func (h *kubeHop) artifact(name string) kubeHopArtifact {
	for _, a := range h.artifacts {
		if a.name == name {
			return a
		}
	}
	panic("unknown hop artifact " + name)
}

// KubernetesHopSteps move a single-node kubeadm cluster from one Kubernetes
// version to the next, with cri-o on the target minor: check health, download
// everything, upgrade kubeadm and run `kubeadm upgrade apply`, cordon, swap
// cri-o, the kubelet and kubectl, wait for health, uncordon. Each step checks
// the live state first, so a re-run finishes a hop that stopped part way and
// a done hop changes nothing. A resume hop is the same steps: the control plane
// is already at the target, so the apply skips.
func KubernetesHopSteps(from, to, crio string, newClient kube.ClientProvider) []automa.Builder {
	h := &kubeHop{from: from, to: to, crio: crio, newClient: newClient, artifacts: kubeHopInstallers(to, crio)}
	return []automa.Builder{
		h.checkHealthyBefore(),
		h.download(),
		h.upgradeArtifact(software.KubeadmBinaryName),
		h.apply(),
		h.cordon(),
		h.stopNodeServices(),
		h.upgradeArtifact(software.CrioArtifactName),
		h.upgradeArtifact(software.KubeletBinaryName),
		h.upgradeArtifact(software.KubectlBinaryName),
		h.startNodeServices(),
		h.waitHealthy(),
		h.uncordon(),
	}
}

// RecoverKubernetesHop is run when a hop fails: it starts cri-o and the kubelet
// and lifts the hop's cordon, so the node keeps running what it has. Each part
// is best effort and logged.
func RecoverKubernetesHop(ctx context.Context, newClient kube.ClientProvider) {
	for _, svc := range []string{software.CrioServiceName, software.KubeletServiceName} {
		if err := startService(ctx, svc); err != nil {
			logx.As().Warn().Err(err).Str("service", svc).Msg("Could not start the service after the failed hop")
		}
	}
	k, err := newClient()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Could not reach the cluster to lift the upgrade cordon")
		return
	}
	health, err := readKubeHealth(ctx, k)
	if err != nil || !health.OurCordon {
		return
	}
	if err := setNodeCordon(ctx, k, health.Node, false); err != nil {
		logx.As().Warn().Err(err).Str("node", health.Node).Msg("Could not lift the upgrade cordon")
	}
}

// staticPod is one control-plane pod.
type staticPod struct {
	Version string // from the image tag, without the leading v
	Ready   bool
}

// kubeHealth is what the hop reads from the cluster.
type kubeHealth struct {
	APIServer     string
	Nodes         int
	Node          string
	Ready         bool
	Unschedulable bool
	OurCordon     bool
	Kubelet       string
	Runtime       string // as the node reports it, e.g. cri-o://1.34.15
	ControlPlane  map[string]staticPod
	KubeProxy     bool
}

func (k kubeHealth) common() []string {
	var out []string
	if k.Nodes != 1 {
		out = append(out, fmt.Sprintf("the cluster has %d nodes; upgrades support one", k.Nodes))
	}
	if !k.Ready {
		out = append(out, "the node is not Ready")
	}
	if k.KubeProxy {
		out = append(out, "a kube-proxy DaemonSet exists, but the cluster runs Cilium without kube-proxy")
	}
	if _, ok := k.ControlPlane["etcd"]; !ok {
		out = append(out, "no etcd pod")
	} else if !k.ControlPlane["etcd"].Ready {
		out = append(out, "etcd is not Ready")
	}
	return out
}

// startProblems lists why the cluster cannot start the hop: it must be
// healthy, with the control plane and the kubelet each at the start or the
// target, so a hop that stopped part way can run again.
func (k kubeHealth) startProblems(from, to string) []string {
	out := k.common()
	at := func(v string) bool { return v == from || v == to }
	if !at(k.APIServer) {
		out = append(out, fmt.Sprintf("the API server runs %q, not %s or %s", k.APIServer, from, to))
	}
	if !at(k.Kubelet) {
		out = append(out, fmt.Sprintf("the kubelet runs %q, not %s or %s", k.Kubelet, from, to))
	}
	for _, c := range controlPlaneComponents {
		p, ok := k.ControlPlane[c]
		switch {
		case !ok:
			out = append(out, "no "+c+" pod")
		case !p.Ready:
			out = append(out, c+" is not Ready")
		case !at(p.Version):
			out = append(out, fmt.Sprintf("%s runs %q, not %s or %s", c, p.Version, from, to))
		}
	}
	return out
}

// problems lists why the cluster is not a healthy cluster on kube with cri-o
// at crio.
func (k kubeHealth) problems(kubeVersion, crio string) []string {
	out := k.common()
	if k.APIServer != kubeVersion {
		out = append(out, fmt.Sprintf("the API server runs %q, not %s", k.APIServer, kubeVersion))
	}
	if k.Kubelet != kubeVersion {
		out = append(out, fmt.Sprintf("the kubelet runs %q, not %s", k.Kubelet, kubeVersion))
	}
	if want := "cri-o://" + crio; k.Runtime != want {
		out = append(out, fmt.Sprintf("the runtime is %q, not %s", k.Runtime, want))
	}
	for _, c := range controlPlaneComponents {
		p, ok := k.ControlPlane[c]
		switch {
		case !ok:
			out = append(out, "no "+c+" pod")
		case p.Version != kubeVersion:
			out = append(out, fmt.Sprintf("%s runs %q, not %s", c, p.Version, kubeVersion))
		case !p.Ready:
			out = append(out, c+" is not Ready")
		}
	}
	return out
}

func defaultReadKubeHealth(ctx context.Context, k *kube.Client) (kubeHealth, error) {
	h := kubeHealth{ControlPlane: map[string]staticPod{}}
	v, err := kube.ServerVersion(10 * time.Second)
	if err != nil {
		return h, err
	}
	h.APIServer = strings.TrimPrefix(v, "v")

	nodes, err := k.List(ctx, kube.KindNode, "", kube.WaitOptions{})
	if err != nil {
		return h, errorx.ExternalError.Wrap(err, "failed to list nodes")
	}
	h.Nodes = len(nodes.Items)
	if h.Nodes > 0 {
		n := nodes.Items[0].Object
		h.Node = nodes.Items[0].GetName()
		h.Ready = conditionTrue(n, "Ready")
		h.Unschedulable, _, _ = unstructured.NestedBool(n, "spec", "unschedulable")
		h.OurCordon = nodes.Items[0].GetAnnotations()[UpgradeCordonAnnotation] == "true"
		kubelet, _, _ := unstructured.NestedString(n, "status", "nodeInfo", "kubeletVersion")
		h.Kubelet = strings.TrimPrefix(kubelet, "v")
		h.Runtime, _, _ = unstructured.NestedString(n, "status", "nodeInfo", "containerRuntimeVersion")
	}

	pods, err := k.List(ctx, kube.KindPod, "kube-system", kube.WaitOptions{LabelSelector: "tier=control-plane"})
	if err != nil {
		return h, errorx.ExternalError.Wrap(err, "failed to list the control-plane pods")
	}
	for _, p := range pods.Items {
		component := p.GetLabels()["component"]
		containers, _, _ := unstructured.NestedSlice(p.Object, "spec", "containers")
		var image string
		if len(containers) > 0 {
			if c, ok := containers[0].(map[string]any); ok {
				image, _ = c["image"].(string)
			}
		}
		version := ""
		if i := strings.LastIndex(image, ":"); i >= 0 {
			version = strings.TrimPrefix(image[i+1:], "v")
		}
		h.ControlPlane[component] = staticPod{Version: version, Ready: conditionTrue(p.Object, "Ready")}
	}

	h.KubeProxy, err = k.ResourceExists(ctx, "apps/v1", "DaemonSet", "kube-system", "kube-proxy")
	return h, err
}

func conditionTrue(obj map[string]any, kind string) bool {
	conditions, _, _ := unstructured.NestedSlice(obj, "status", "conditions")
	for _, c := range conditions {
		m, ok := c.(map[string]any)
		if ok && m["type"] == kind {
			return m["status"] == "True"
		}
	}
	return false
}

func defaultSetNodeCordon(ctx context.Context, k *kube.Client, node string, cordon bool) error {
	var unschedulable, mark any
	if cordon {
		unschedulable, mark = true, "true"
	}
	patch, err := json.Marshal(map[string]any{
		"spec":     map[string]any{"unschedulable": unschedulable},
		"metadata": map[string]any{"annotations": map[string]any{UpgradeCordonAnnotation: mark}},
	})
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to build the cordon patch")
	}
	_, err = k.Dyn.Resource(schema.GroupVersionResource{Version: "v1", Resource: "nodes"}).
		Patch(ctx, node, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to patch node %s", node)
	}
	return nil
}

func (h *kubeHop) client(stp automa.Step) (*kube.Client, *automa.Report) {
	k, err := h.newClient()
	if err != nil {
		return nil, automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
			"Check that the admin kubeconfig works: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes")))
	}
	return k, nil
}

// done reports whether the cluster already runs the target, healthy, with
// every host binary at the target and no cordon of ours left.
func (h *kubeHop) done(ctx context.Context, k *kube.Client) bool {
	health, err := readKubeHealth(ctx, k)
	if err != nil || len(health.problems(h.to, h.crio)) > 0 || health.OurCordon {
		return false
	}
	for _, a := range h.artifacts {
		if at, err := HostBinaryAt(a.name, a.version); err != nil || !at {
			return false
		}
	}
	return true
}

func (h *kubeHop) skipDone(ctx context.Context, stp automa.Step) *automa.Report {
	k, rpt := h.client(stp)
	if rpt != nil {
		return rpt
	}
	if h.done(ctx, k) {
		return automa.SkippedReport(stp, automa.WithDetail("Kubernetes already runs "+h.to))
	}
	return nil
}

func detail(msg string) func(context.Context, automa.Step) (context.Context, error) {
	return func(ctx context.Context, stp automa.Step) (context.Context, error) {
		notify.As().StepDetail(ctx, stp, msg)
		return ctx, nil
	}
}

// checkHealthyBefore waits a little for the cluster to be ready to start,
// since a re-run can follow a control plane that has just restarted.
func (h *kubeHop) checkHealthyBefore() automa.Builder {
	return automa.NewStepBuilder().WithId("check-kubernetes-healthy").
		WithPrepare(detail("checking the cluster before the upgrade...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			if h.done(ctx, k) {
				return automa.SkippedReport(stp, automa.WithDetail("Kubernetes already runs "+h.to))
			}
			deadline := time.Now().Add(kubeStartTimeout)
			var err error
			for {
				var health kubeHealth
				health, err = readKubeHealth(ctx, k)
				if err == nil {
					p := health.startProblems(h.from, h.to)
					if len(p) == 0 {
						return automa.SuccessReport(stp)
					}
					err = errorx.IllegalState.New("the cluster cannot start the %s -> %s upgrade: %s", h.from, h.to, strings.Join(p, "; "))
				}
				if time.Now().After(deadline) {
					break
				}
				select {
				case <-ctx.Done():
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(ctx.Err(), reasons.Internal)))
				case <-time.After(kubePollInterval):
				}
			}
			return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.PreconditionNotMet,
				"Check the node and the control plane: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes,pods -n kube-system -o wide",
				"Fix the cluster before upgrading it")))
		})
}

// download fetches and verifies every target artifact before anything
// changes, so a network or checksum failure leaves the cluster as it was.
func (h *kubeHop) download() automa.Builder {
	return automa.NewStepBuilder().WithId("download-kubernetes-binaries").
		WithPrepare(detail("downloading Kubernetes " + h.to + " and cri-o " + h.crio + "...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if rpt := h.skipDone(ctx, stp); rpt != nil {
				return rpt
			}
			for _, a := range h.artifacts {
				at, err := HostBinaryAt(a.name, a.version)
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.IntegrityCheckFailed,
						fmt.Sprintf("Check the %s binaries in the sandbox bin directory", a.name))))
				}
				if at {
					continue
				}
				installer, err := a.provider(software.WithVersion(a.version))
				if err == nil {
					err = installer.Download()
				}
				if err != nil {
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.DownloadFailed,
						fmt.Sprintf("Check that the host can download %s %s, then re-run the upgrade; nothing was changed", a.name, a.version))))
				}
			}
			return automa.SuccessReport(stp)
		})
}

func (h *kubeHop) upgradeArtifact(name string) automa.Builder {
	a := h.artifact(name)
	var before func() error
	if name == software.CrioArtifactName {
		before = removeCrioRelease
	}
	return upgradeHostBinary(a.name, a.version, a.archived, a.provider, before)
}

// apply runs `kubeadm upgrade apply`. kubeadm backs up the static pod
// manifests and etcd and rolls its own change back when the apply fails.
func (h *kubeHop) apply() automa.Builder {
	return automa.NewStepBuilder().WithId("kubeadm-upgrade-apply").
		WithPrepare(detail("upgrading the control plane to " + h.to + "...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			health, err := readKubeHealth(ctx, k)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
					"Check the API server: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get --raw /readyz")))
			}
			if health.APIServer == h.to {
				return automa.SkippedReport(stp, automa.WithDetail("the control plane already runs "+h.to))
			}
			// The recorded kubeadm version is the old one until the hop is
			// recorded, so the binary is checked against the target instead.
			at, err := HostBinaryAt(software.KubeadmBinaryName, h.to)
			if err == nil && !at {
				err = errorx.IllegalState.New("the installed kubeadm is not the catalog's %s", h.to)
			}
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.IntegrityCheckFailed,
					fmt.Sprintf("Check %s/kubeadm; re-run the upgrade to install kubeadm %s again", models.Paths().SandboxBinDir, h.to))))
			}
			cmd := kubeadmUpgradeApplyCommand(h.to)
			if _, err := runBashScript([]string{cmd}, ""); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(
					errorx.ExternalError.Wrap(err, "kubeadm upgrade apply %s failed; kubeadm rolls back its own failed apply", h.to),
					ReasonHostCommandFailed,
					"Check the control plane: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf -n kube-system get pods -l tier=control-plane",
					"Check kubeadm's output above, fix the cause, then re-run the upgrade")))
			}
			return automa.SuccessReport(stp, automa.WithDetail("the control plane runs "+h.to))
		})
}

func kubeadmUpgradeApplyCommand(version string) string {
	return fmt.Sprintf("sudo %s/kubeadm upgrade apply v%s --yes --v=3", models.Paths().SandboxBinDir, version)
}

// cordon stops new pods landing on the node while the kubelet and cri-o
// restart. The node is not drained: on one node evicted pods have nowhere to
// go, and the restarts keep running containers.
func (h *kubeHop) cordon() automa.Builder {
	return automa.NewStepBuilder().WithId("cordon-node").
		WithPrepare(detail("cordoning the node...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			health, err := readKubeHealth(ctx, k)
			if err == nil && len(health.problems(h.to, h.crio)) == 0 {
				return automa.SkippedReport(stp, automa.WithDetail("the node already runs "+h.to))
			}
			if err == nil && health.Unschedulable {
				return automa.SkippedReport(stp, automa.WithDetail("the node is already cordoned"))
			}
			if err == nil {
				err = setNodeCordon(ctx, k, health.Node, true)
			}
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
					"Check the node: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes")))
			}
			return automa.SuccessReport(stp)
		})
}

// nodeAtTarget reports whether cri-o and the kubelet need no change: their
// binaries are the target and the node reports running them.
func (h *kubeHop) nodeAtTarget(ctx context.Context, k *kube.Client) bool {
	health, err := readKubeHealth(ctx, k)
	if err != nil || health.Kubelet != h.to || health.Runtime != "cri-o://"+h.crio {
		return false
	}
	for _, name := range []string{software.CrioArtifactName, software.KubeletBinaryName} {
		a := h.artifact(name)
		if at, err := HostBinaryAt(a.name, a.version); err != nil || !at {
			return false
		}
	}
	return true
}

func (h *kubeHop) stopNodeServices() automa.Builder {
	return automa.NewStepBuilder().WithId("stop-node-services").
		WithPrepare(detail("stopping the kubelet and cri-o...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			if h.nodeAtTarget(ctx, k) {
				return automa.SkippedReport(stp, automa.WithDetail("cri-o and the kubelet already run the target"))
			}
			// The kubelet goes first: it must not find cri-o gone. Running
			// containers keep running through both stops.
			for _, svc := range []string{software.KubeletServiceName, software.CrioServiceName} {
				if err := stopService(ctx, svc); err != nil {
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, ReasonHostCommandFailed,
						"Check the service: sudo systemctl status "+svc)))
				}
			}
			return automa.SuccessReport(stp)
		})
}

func (h *kubeHop) startNodeServices() automa.Builder {
	return automa.NewStepBuilder().WithId("start-node-services").
		WithPrepare(detail("starting cri-o and the kubelet...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			if rpt := h.skipDone(ctx, stp); rpt != nil {
				return rpt
			}
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			// The cri-o install puts back the release's registries.conf; a
			// configured pull-through cache needs ours again.
			if reg := containerRegistryOf(); reg != "" {
				if err := installRegistries(reg); err != nil {
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.Internal)))
				}
			}
			if err := daemonReload(ctx); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, ReasonHostCommandFailed,
					"Run: sudo systemctl daemon-reload")))
			}
			if err := startService(ctx, software.CrioServiceName); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, ReasonHostCommandFailed,
					"Check cri-o: sudo systemctl status crio; sudo journalctl -u crio")))
			}
			// A kubelet already on the target is only started, so a re-run does
			// not restart it.
			kubelet := restartService
			if h.nodeAtTarget(ctx, k) {
				kubelet = startService
			}
			if err := kubelet(ctx, software.KubeletServiceName); err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, ReasonHostCommandFailed,
					"Check the kubelet: sudo systemctl status kubelet; sudo journalctl -u kubelet")))
			}
			return automa.SuccessReport(stp)
		})
}

func (h *kubeHop) waitHealthy() automa.Builder {
	return automa.NewStepBuilder().WithId("wait-kubernetes-healthy").
		WithPrepare(detail("waiting for Kubernetes " + h.to + " to be healthy...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			deadline := time.Now().Add(kubeHealthTimeout)
			var last []string
			for {
				health, err := readKubeHealth(ctx, k)
				if err != nil {
					last = []string{err.Error()}
				} else if last = health.problems(h.to, h.crio); len(last) == 0 {
					return automa.SuccessReport(stp, automa.WithDetail("Kubernetes "+h.to+" is healthy"))
				}
				if time.Now().After(deadline) {
					break
				}
				select {
				case <-ctx.Done():
					return automa.FailureReport(stp, automa.WithError(errx.Decorate(ctx.Err(), reasons.Internal)))
				case <-time.After(kubePollInterval):
				}
			}
			slices.Sort(last)
			return automa.FailureReport(stp, automa.WithError(errx.Decorate(
				errorx.IllegalState.New("Kubernetes %s is not healthy after %s: %s", h.to, kubeHealthTimeout, strings.Join(last, "; ")),
				reasons.PreconditionNotMet,
				"Check the node and the control plane: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf get nodes,pods -n kube-system -o wide",
				"Check the services: sudo journalctl -u kubelet -u crio --since -15m")))
		})
}

func (h *kubeHop) uncordon() automa.Builder {
	return automa.NewStepBuilder().WithId("uncordon-node").
		WithPrepare(detail("uncordoning the node...")).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			k, rpt := h.client(stp)
			if rpt != nil {
				return rpt
			}
			health, err := readKubeHealth(ctx, k)
			if err == nil && !health.OurCordon {
				return automa.SkippedReport(stp, automa.WithDetail("the upgrade did not cordon the node"))
			}
			if err == nil {
				err = setNodeCordon(ctx, k, health.Node, false)
			}
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(errx.Decorate(err, reasons.K8sAPIFailed,
					"Uncordon the node by hand: sudo kubectl --kubeconfig /etc/kubernetes/admin.conf uncordon "+health.Node)))
			}
			return automa.SuccessReport(stp)
		})
}
