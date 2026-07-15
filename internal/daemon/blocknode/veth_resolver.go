// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joomcode/errorx"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

// vethRESTTimeout caps initial TLS/connection setup for the k8s exec API.
// The streaming phase itself is bounded by the context passed to Resolve.
const vethRESTTimeout = 10 * time.Second

const defaultSysClassNet = "/sys/class/net"

var (
	// ErrVethNotFound is returned by Resolve when no host interface has an ifindex
	// matching the pod's eth0 iflink. The host-side veth may not yet be visible
	// (Cilium is still setting up the veth pair); callers should retry.
	ErrVethNotFound = errorx.ExternalError.New("host veth not yet visible for pod iflink")

	// ErrVethNotReady is returned (via errors.Join) when exec into the pod fails
	// because the container is not yet started or is not exec-capable. Callers
	// should retry after the pod reaches ContainersReady.
	ErrVethNotReady = errorx.ExternalError.New("pod container not ready for veth iflink exec")
)

// VethResolverConfig holds inputs for NewVethResolver.
type VethResolverConfig struct {
	// KubeconfigPath is the path to the BN-scoped kubeconfig used to exec
	// into the BN pod to read its eth0 iflink.
	KubeconfigPath string

	// SysClassNet is the host-side /sys/class/net path to scan for ifindex
	// files. Defaults to /sys/class/net when empty; override in tests.
	SysClassNet string
}

// VethResolver resolves the host-side veth peer of a BN pod by matching the
// pod's eth0 iflink against the host's /sys/class/net/*/ifindex entries.
//
// Algorithm (v4 design §8.1.1):
//  1. Exec "cat /sys/class/net/eth0/iflink" inside the pod → the host-side
//     veth's ifindex.
//  2. Scan /sys/class/net/*/ifindex on the host for the matching interface.
type VethResolver struct {
	// readPodIflink returns the iflink index of eth0 inside the given pod.
	// Injected at construction so tests can supply a fake without mocking the
	// SPDY transport.
	readPodIflink func(ctx context.Context, pod *corev1.Pod) (int, error)

	sysClassNet string
}

// NewVethResolver builds a VethResolver backed by the k8s exec API.
// The REST config and typed k8s client are built once from cfg.KubeconfigPath
// and reused across Resolve calls (matching the UpgradeMonitor pattern).
func NewVethResolver(cfg VethResolverConfig) (*VethResolver, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags("", cfg.KubeconfigPath)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "load kubeconfig %s", cfg.KubeconfigPath)
	}
	restCfg.Timeout = vethRESTTimeout

	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "build k8s client for veth resolver")
	}

	sysClassNet := cfg.SysClassNet
	if sysClassNet == "" {
		sysClassNet = defaultSysClassNet
	}

	r := &VethResolver{sysClassNet: sysClassNet}
	r.readPodIflink = func(ctx context.Context, pod *corev1.Pod) (int, error) {
		return execReadIflink(ctx, restCfg, client, pod)
	}
	return r, nil
}

// Resolve returns the host-side veth name (e.g. "lxc1a2b3c4d") for the given
// pod's eth0 interface.
//
// Callers should retry on both ErrVethNotFound (veth not yet visible) and any
// error wrapping ErrVethNotReady (container not yet exec-capable).
func (r *VethResolver) Resolve(ctx context.Context, pod *corev1.Pod) (string, error) {
	iflink, err := r.readPodIflink(ctx, pod)
	if err != nil {
		return "", err
	}
	return r.scanHostVeth(iflink)
}

// scanHostVeth scans r.sysClassNet/*/ifindex for an entry whose value equals
// iflink. Returns the matching interface name or ErrVethNotFound.
func (r *VethResolver) scanHostVeth(iflink int) (string, error) {
	entries, err := os.ReadDir(r.sysClassNet)
	if err != nil {
		return "", errorx.ExternalError.Wrap(err, "read %s", r.sysClassNet)
	}
	for _, entry := range entries {
		idx, err := readIfindex(filepath.Join(r.sysClassNet, entry.Name(), "ifindex"))
		if err != nil {
			continue // not a network-interface dir, or ifindex unreadable — skip
		}
		if idx == iflink {
			return entry.Name(), nil
		}
	}
	return "", ErrVethNotFound
}

// readIfindex reads a single sysfs ifindex file and returns its integer value.
func readIfindex(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, errorx.ExternalError.Wrap(err, "read %s", path)
	}
	trimmed := strings.TrimSpace(string(data))
	val, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, errorx.IllegalFormat.Wrap(err, "parse ifindex at %s: %q", path, trimmed)
	}
	return val, nil
}

// execReadIflink execs "cat /sys/class/net/eth0/iflink" inside pod's first
// container via SPDY and returns the parsed integer.
//
// All exec failures (RBAC, network, container-not-started) are wrapped with
// ErrVethNotReady so the caller's retry loop treats them uniformly. Stderr is
// captured and appended to the error message to surface hard failures (e.g.
// RBAC forbidden) that would otherwise be invisible.
func execReadIflink(ctx context.Context, restCfg *rest.Config, client kubernetes.Interface, pod *corev1.Pod) (int, error) {
	if pod == nil {
		return 0, errors.Join(ErrVethNotReady,
			errorx.ExternalError.New("execReadIflink called with nil pod"))
	}
	if len(pod.Spec.Containers) == 0 {
		return 0, errors.Join(ErrVethNotReady,
			errorx.ExternalError.New("pod %s/%s has no containers", pod.Namespace, pod.Name))
	}

	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: pod.Spec.Containers[0].Name,
			Command:   []string{"cat", "/sys/class/net/eth0/iflink"},
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		return 0, errors.Join(ErrVethNotReady,
			errorx.ExternalError.Wrap(err, "create SPDY executor for pod %s/%s", pod.Namespace, pod.Name))
	}

	var stdout, stderr bytes.Buffer
	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr}); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return 0, errors.Join(ErrVethNotReady,
				errorx.ExternalError.Wrap(err, "exec iflink in pod %s/%s: %s", pod.Namespace, pod.Name, msg))
		}
		return 0, errors.Join(ErrVethNotReady,
			errorx.ExternalError.Wrap(err, "exec iflink in pod %s/%s", pod.Namespace, pod.Name))
	}

	trimmed := strings.TrimSpace(stdout.String())
	val, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, errorx.IllegalFormat.Wrap(err, "parse iflink output from pod %s/%s: %q", pod.Namespace, pod.Name, trimmed)
	}
	return val, nil
}
