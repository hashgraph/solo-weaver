// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/workflows/notify"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/hashgraph/solo-weaver/pkg/semver"
	"github.com/hashgraph/solo-weaver/pkg/software"
)

// cgroupV2ControllersFile exists only when /sys/fs/cgroup is the cgroup v2
// unified hierarchy.
const cgroupV2ControllersFile = "/sys/fs/cgroup/cgroup.controllers"

// minKubeletWithoutCgroupV1 is the first kubelet release that refuses to start
// on cgroup v1 by default, and whose kubeadm preflight rejects it.
const minKubeletWithoutCgroupV1 = "1.35.0"

const apiServerProbeTimeout = 10 * time.Second

// controlPlaneFiles are what kubeadm init leaves under /etc/kubernetes.
var controlPlaneFiles = []string{
	"admin.conf",
	"manifests/kube-apiserver.yaml",
	"manifests/etcd.yaml",
	"manifests/kube-controller-manager.yaml",
	"manifests/kube-scheduler.yaml",
}

// kubernetesHostSource is a host component the Kubernetes setup installs at its
// catalog default when it is missing. key is its MachineRuntime name.
type kubernetesHostSource struct {
	key      string
	provider func(opts ...software.InstallerOption) (software.Software, error)
}

var kubernetesHostSources = []kubernetesHostSource{
	{software.KubeletBinaryName, software.NewKubeletInstaller},
	{software.KubeadmBinaryName, software.NewKubeadmInstaller},
	{software.KubectlBinaryName, software.NewKubectlInstaller},
	{software.CrioBinaryName, software.NewCrioInstaller},
}

// kubernetesHostComponent is what this run would do to one component.
type kubernetesHostComponent struct {
	name      string
	target    string // the catalog default this run installs
	installed bool
	version   string // installed version; empty when unknown or not installed
	// pending means this run writes the component from the catalog default:
	// the binaries are missing, or for kubeadm, its config is missing and
	// would be rendered with the default Kubernetes version.
	pending bool
}

// cgroupV2Enabled and identifyInstalledVersions are package vars so tests can
// stub the host.
var (
	cgroupV2Enabled = func() (bool, error) {
		_, err := os.Stat(cgroupV2ControllersFile)
		if err == nil {
			return true, nil
		}
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errorx.ExternalError.Wrap(err, "failed to check %s", cgroupV2ControllersFile)
	}
	identifyInstalledVersions = software.IdentifyInstalledVersions

	// /etc/kubernetes is a bind mount of the sandbox copy that this workflow
	// only sets up after the check, so an unmounted host keeps its files there.
	kubernetesConfigDirs = func() []string {
		return []string{"/etc/kubernetes", path.Join(models.Paths().SandboxDir, "etc", "kubernetes")}
	}
	probeAPIServer = func(kubeconfig string) error {
		return kube.ProbeAPIServer(kubeconfig, apiServerProbeTimeout)
	}
)

// existingControlPlane is what kubeadm left on this host from an earlier init.
type existingControlPlane struct {
	evidence   []string
	version    string // from the kube-apiserver static pod image; empty when unknown
	kubeconfig string // admin kubeconfig to probe its API with; empty when none survives
}

// CheckKubernetesHost stops the Kubernetes setup before it installs anything
// when the run could change a cluster it did not create or leave the node
// unable to work: an existing control plane whose API server is unreachable or
// whose missing components would come back at the catalog default, a missing
// component next to components installed at another version, or a kubelet that
// needs cgroup v2 on a cgroup v1 host. A fresh host, and a healthy one whose
// components are all installed, pass.
func CheckKubernetesHost(mr software.MachineRuntime) *automa.StepBuilder {
	return automa.NewStepBuilder().WithId("check-kubernetes-host").
		WithPrepare(func(ctx context.Context, stp automa.Step) (context.Context, error) {
			notify.As().StepStart(ctx, stp, "Checking installed Kubernetes components")
			return ctx, nil
		}).
		WithOnFailure(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepFailure(ctx, stp, rpt, "Kubernetes components cannot be installed on this host")
		}).
		WithOnCompletion(func(ctx context.Context, stp automa.Step, rpt *automa.Report) {
			notify.As().StepCompletion(ctx, stp, rpt, "Kubernetes components checked")
		}).
		WithExecute(func(ctx context.Context, stp automa.Step) *automa.Report {
			components, err := collectKubernetesHostComponents(mr, kubernetesHostSources)
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			cp, err := detectControlPlane(kubernetesConfigDirs())
			if err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			if err := checkKubernetesHostComponents(components, cp); err != nil {
				return automa.FailureReport(stp, automa.WithError(err))
			}
			return automa.SuccessReport(stp)
		})
}

// collectKubernetesHostComponents asks each installer what the setup steps
// will ask it, so the check sees the same installed/configured answers.
func collectKubernetesHostComponents(mr software.MachineRuntime, sources []kubernetesHostSource) ([]kubernetesHostComponent, error) {
	components := make([]kubernetesHostComponent, 0, len(sources))
	for _, src := range sources {
		inst, err := src.provider(software.WithMachineRuntime(mr))
		if err != nil {
			return nil, err
		}

		c := kubernetesHostComponent{name: inst.GetSoftwareName(), target: inst.Version()}
		if c.installed, err = inst.IsInstalled(); err != nil {
			return nil, err
		}

		if !c.installed {
			c.pending = true
		} else {
			c.version = installedComponentVersion(mr, src.key, c.name)
			if src.key == software.KubeadmBinaryName {
				configured, err := inst.IsConfigured()
				if err != nil {
					return nil, err
				}
				c.pending = !configured
			}
		}

		components = append(components, c)
	}
	return components, nil
}

// installedComponentVersion prefers the runtime snapshot, whose version the
// machine checker already resolved from state or by checksum.
func installedComponentVersion(mr software.MachineRuntime, key, artifact string) string {
	if mr != nil {
		if sw, ok := mr.SoftwareState(key); ok && sw.Installed {
			return sw.Version
		}
	}

	matches, err := identifyInstalledVersions(artifact)
	if err != nil || len(matches) != 1 {
		return ""
	}
	return matches[0]
}

func checkKubernetesHostComponents(components []kubernetesHostComponent, cp existingControlPlane) error {
	var pending, foreign []string
	for _, c := range components {
		if c.pending {
			pending = append(pending, fmt.Sprintf("%s %s", c.name, c.target))
		}
		if c.installed && c.version != c.target {
			v := c.version
			if v == "" {
				v = "unknown version"
			}
			foreign = append(foreign, fmt.Sprintf("%s %s", c.name, v))
		}
	}

	if len(cp.evidence) > 0 {
		if err := checkExistingControlPlane(components, pending, cp); err != nil {
			return err
		}
	}

	if len(pending) == 0 {
		return nil
	}

	if len(foreign) > 0 {
		return errx.Decorate(
			errorx.IllegalState.New(
				"Kubernetes is partly installed on this host at another version (%s); this run would add %s next to it",
				strings.Join(foreign, ", "), strings.Join(pending, ", ")),
			reasons.PreconditionNotMet,
			"Restore the missing components with the provisioner release that installed the others, then re-run this command",
			"If the host has no cluster you need to keep, run 'solo-provisioner kube cluster uninstall' and then 'solo-provisioner kube cluster install'")
	}

	return checkCgroupV2(components)
}

// checkExistingControlPlane never treats an unreachable API server as proof
// that the cluster is gone, and only lets missing components back in at the
// catalog default when the cluster already runs that Kubernetes version.
func checkExistingControlPlane(components []kubernetesHostComponent, pending []string, cp existingControlPlane) error {
	found := strings.Join(cp.evidence, ", ")

	if len(pending) > 0 && cp.version != kubeletComponent(components).target {
		version := cp.version
		if version == "" {
			version = "unknown"
		}
		return errx.Decorate(
			errorx.IllegalState.New(
				"an existing Kubernetes control plane was found (%s; version %s); this run would install %s next to it",
				found, version, strings.Join(pending, ", ")),
			reasons.PreconditionNotMet,
			"Restore the missing components with the provisioner release that created the cluster, then re-run this command",
			"If the cluster is no longer needed, run 'solo-provisioner kube cluster uninstall' and remove what is left under /etc/kubernetes before installing")
	}

	if cp.kubeconfig == "" {
		return errx.Decorate(
			errorx.IllegalState.New("an existing Kubernetes control plane was found (%s), but no admin.conf is left to check its API server", found),
			reasons.PreconditionNotMet,
			"Restore /etc/kubernetes/admin.conf from the control plane, then re-run this command",
			"If the cluster is no longer needed, run 'solo-provisioner kube cluster uninstall' and remove what is left under /etc/kubernetes before installing")
	}

	if err := probeAPIServer(cp.kubeconfig); err != nil {
		return errx.Decorate(
			errorx.IllegalState.Wrap(err, "an existing Kubernetes control plane was found (%s), but its API server is not reachable", found),
			reasons.PreconditionNotMet,
			fmt.Sprintf("Check the control plane with 'sudo kubectl --kubeconfig %s get --raw /readyz' and 'sudo systemctl status kubelet crio'", cp.kubeconfig),
			"Re-run this command once the API server is ready")
	}
	return nil
}

func kubeletComponent(components []kubernetesHostComponent) kubernetesHostComponent {
	for _, c := range components {
		if c.name == software.KubeletBinaryName {
			return c
		}
	}
	return kubernetesHostComponent{}
}

// detectControlPlane looks only at local files: a missing default kubeconfig
// or an unreachable API server says nothing about whether a cluster exists.
func detectControlPlane(dirs []string) (existingControlPlane, error) {
	var cp existingControlPlane
	var seen []os.FileInfo
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err == nil {
			if sameAsAny(info, seen) {
				continue // the bind mount and its source are one directory
			}
			seen = append(seen, info)
		}
		for _, rel := range controlPlaneFiles {
			p := path.Join(dir, rel)
			_, err := os.Stat(p)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return cp, errorx.ExternalError.Wrap(err, "failed to check %s", p)
			}
			cp.evidence = append(cp.evidence, p)
			switch {
			case rel == "admin.conf" && cp.kubeconfig == "":
				cp.kubeconfig = p
			case rel == "manifests/kube-apiserver.yaml" && cp.version == "":
				cp.version = apiServerManifestVersion(p)
			}
		}
	}
	return cp, nil
}

func sameAsAny(info os.FileInfo, seen []os.FileInfo) bool {
	for _, s := range seen {
		if os.SameFile(info, s) {
			return true
		}
	}
	return false
}

// apiServerManifestVersion reads the Kubernetes version from the image tag of
// the kube-apiserver static pod, e.g. registry.k8s.io/kube-apiserver:v1.33.4.
func apiServerManifestVersion(manifest string) string {
	data, err := os.ReadFile(manifest)
	if err != nil {
		return ""
	}
	var pod struct {
		Spec struct {
			Containers []struct {
				Name  string `yaml:"name"`
				Image string `yaml:"image"`
			} `yaml:"containers"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &pod); err != nil {
		return ""
	}
	for _, c := range pod.Spec.Containers {
		if c.Name == "kube-apiserver" {
			return imageTagVersion(c.Image)
		}
	}
	return ""
}

func imageTagVersion(image string) string {
	image, _, _ = strings.Cut(image, "@")
	i := strings.LastIndex(image, ":")
	if i < 0 || strings.Contains(image[i:], "/") {
		return ""
	}
	return strings.TrimPrefix(image[i+1:], "v")
}

// checkCgroupV2 runs only for an install that changes the host, so a host that
// already runs its kubelet is never blocked by it.
func checkCgroupV2(components []kubernetesHostComponent) error {
	k := kubeletComponent(components)
	kubelet := k.target
	if k.installed {
		kubelet = k.version
	}

	needsV2, err := kubeletNeedsCgroupV2(kubelet)
	if err != nil || !needsV2 {
		return err
	}

	enabled, err := cgroupV2Enabled()
	if err != nil {
		return errx.Decorate(err, reasons.PreconditionNotMet,
			"Check that /sys/fs/cgroup is mounted: 'stat -fc %T /sys/fs/cgroup/'")
	}
	if enabled {
		return nil
	}

	return errx.Decorate(
		errorx.IllegalState.New("Kubernetes %s needs cgroup v2, but this host uses cgroup v1 (%s not found)", kubelet, cgroupV2ControllersFile),
		reasons.PreconditionNotMet,
		"Check the cgroup version with 'stat -fc %T /sys/fs/cgroup/': cgroup v2 prints 'cgroup2fs'",
		"Boot the host with cgroup v2 (kernel argument 'systemd.unified_cgroup_hierarchy=1'), or use an OS release that defaults to it such as Ubuntu 22.04 or Debian 11 and later")
}

func kubeletNeedsCgroupV2(version string) (bool, error) {
	if version == "" {
		return false, nil
	}
	v, err := semver.NewSemver(version)
	if err != nil {
		return false, errorx.IllegalFormat.Wrap(err, "invalid kubelet version %q", version)
	}
	floor, err := semver.NewSemver(minKubeletWithoutCgroupV1)
	if err != nil {
		return false, errorx.AssertionFailed.Wrap(err, "invalid cgroup v2 kubelet floor")
	}
	return v.GreaterOrEqual(floor), nil
}
