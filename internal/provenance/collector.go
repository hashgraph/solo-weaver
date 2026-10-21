// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/internal/state"
)

// Collector gathers the Input the gate evaluates: local evidence (section 5.1),
// the provision record from the state files, and the live cluster ID.
type Collector interface {
	Collect(ctx context.Context, need Need, kubeconfigGiven bool) (Input, error)
}

// probe fills its own part of the Input from one evidence source. The default
// collector runs a fixed set of probes and fuses their output into one Input.
// Probes here are scoped to provenance (is this our cluster, and the host it?);
// broad reality probes (helm releases, secrets, disk) belong to the drift and
// diagnostics layers, not the gate.
type probe interface {
	collect(ctx context.Context, need Need, in *Input) error
}

// defaultCollector is the real collector: it runs the provenance probes against
// this machine and the live cluster. Tests swap in a fake Collector instead.
type defaultCollector struct {
	probes []probe
}

// NewCollector returns the default collector, composing the provenance probes.
func NewCollector() Collector {
	return defaultCollector{
		probes: []probe{
			recordedIdentityProbe{},
			hostProbe{},
			clusterProbe{},
		},
	}
}

func (c defaultCollector) Collect(ctx context.Context, need Need, kubeconfigGiven bool) (Input, error) {
	in := Input{KubeconfigGiven: kubeconfigGiven}
	for _, p := range c.probes {
		if err := p.collect(ctx, need, &in); err != nil {
			return in, err
		}
	}
	return in, nil
}

// Paths the host probe looks at. kubeadm writes all of these on a control-plane node.
const (
	adminConfPath = "/etc/kubernetes/admin.conf"
	manifestsPath = "/etc/kubernetes/manifests"
	caCertPath    = "/etc/kubernetes/pki/ca.crt"
	machineIDPath = "/etc/machine-id"
)

// recordedIdentityProbe reads what this host recorded about the cluster it
// provisioned (the provision record) and the recorded HostIdentity.
//
// TODO(#1235): the provision record and recorded machine ID live in
// hosts/local/host.yaml, and ExternalStateExists is whether clusters/<uid>/ holds
// state — none of which exists until the per-component state work lands. Until
// then nothing is recorded, so these stay false and a provisioned host flows
// through branch B (trust on first use), the intended path for older installs.
type recordedIdentityProbe struct{}

func (recordedIdentityProbe) collect(_ context.Context, _ Need, in *Input) error {
	in.RecordedMachineIDPresent = false
	in.HasProvisionRecord = false
	in.ExternalStateExists = false
	return nil
}

// hostProbe reads local host evidence: the kubeadm control-plane files and the
// recorded kubelet/CRI-O install state. It needs no cluster API.
//
// TODO: binary provenance (the running executable is the sandbox binary and
// matches the catalog checksum) and the server-address-resolves-to-this-host
// check are Linux/root-only and not folded into LocalEvidencePasses yet.
type hostProbe struct{}

func (hostProbe) collect(_ context.Context, _ Need, in *Input) error {
	// Live machine ID, for comparison against the recorded one once #1235 lands.
	// Absent on machines without /etc/machine-id (macOS, some containers).
	_ = readMachineID()

	passes, failed, unreadable, piece := collectLocalEvidence()
	in.LocalEvidencePasses = passes
	in.LocalEvidenceFailed = failed
	in.LocalEvidenceUnreadable = unreadable
	in.FailingPiece = piece
	return nil
}

// clusterProbe reads the live cluster: reachability and the kube-system UID
// (the ClusterIdentity). It is skipped for a host-only need, which never reads
// the API, so an offline host command does not probe the cluster at all.
type clusterProbe struct{}

func (clusterProbe) collect(ctx context.Context, need Need, in *Input) error {
	if need == NeedHost {
		return nil
	}
	_, reachable := kube.ProbeCluster()
	in.APIReachable = reachable
	if reachable {
		if uid := liveClusterID(ctx); uid != "" {
			in.LiveClusterID = uid
		}
	}
	return nil
}

// collectLocalEvidence stats the kubeadm control-plane files and reads the
// recorded kubelet/CRI-O install state. It returns whether the readable evidence
// passes, whether a required piece is genuinely absent (failed) or could not be
// read without more privilege (unreadable), and the first offending piece.
func collectLocalEvidence() (passes, failed, unreadable bool, piece string) {
	for _, f := range []struct {
		path string
		dir  bool
	}{
		{adminConfPath, false},
		{manifestsPath, true},
		{caCertPath, false},
	} {
		ok, readErr := pathExists(f.path, f.dir)
		if readErr {
			return false, false, true, f.path
		}
		if !ok {
			return false, true, false, f.path
		}
	}

	for _, sw := range []string{"kubelet", "crio"} {
		v, err := state.ReadSoftwareVersionFromDisk(sw)
		if err != nil || v == "" {
			return false, true, false, sw + " not recorded as installed"
		}
	}

	return true, false, false, ""
}

// pathExists reports whether path exists (as a directory when dir is true). The
// second return is true when the path could not be read for a reason other than
// absence (typically a permission error), which the gate treats as "unreadable",
// not "missing".
func pathExists(path string, dir bool) (ok bool, unreadable bool) {
	info, err := os.Stat(path)
	if err == nil {
		return info.IsDir() == dir, false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, false
	}
	// Permission or other I/O error: cannot tell, so not proof of absence.
	return false, true
}

// liveClusterID reads the kube-system namespace UID from the live cluster. It
// prefers a client already in the context and falls back to building one.
// Returns "" when no client or UID can be obtained.
func liveClusterID(ctx context.Context) string {
	kc, err := kube.ClientFromContext(ctx)
	if err != nil || kc == nil {
		kc, err = kube.NewClient()
		if err != nil || kc == nil {
			return ""
		}
	}
	uid, err := kc.GetResourceNestedString(ctx, "v1", "Namespace", "", "kube-system", "metadata", "uid")
	if err != nil {
		return ""
	}
	return uid
}

func readMachineID() string {
	b, err := os.ReadFile(machineIDPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
