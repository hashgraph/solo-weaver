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
// the ProvisionRecord from the state files, and the live cluster ID.
type Collector interface {
	Collect(ctx context.Context, need Need, kubeconfigGiven bool) (Input, error)
}

// Paths the local-evidence checks look at. kubeadm writes all of these on a
// control-plane node.
const (
	adminConfPath = "/etc/kubernetes/admin.conf"
	manifestsPath = "/etc/kubernetes/manifests"
	caCertPath    = "/etc/kubernetes/pki/ca.crt"
	machineIDPath = "/etc/machine-id"
)

// osCollector reads real evidence from this machine and the live cluster.
//
// It implements the parts that can be read today: the kubeadm control-plane
// files, the recorded kubelet/CRI-O install state, the live HostIdentity, and the
// live ClusterIdentity. Two pieces are not yet wired and are called out inline:
//
//   - TODO(#1235): the ProvisionRecord (cluster ID, server address, CA
//     fingerprint) and the recorded HostIdentity live in hosts/local/host.yaml,
//     which does not exist until the per-component state work lands. Until then
//     nothing is recorded, so a provisioned host is classified through branch B
//     (trust on first use), which is the intended path for older installs.
//   - TODO: binary provenance (the running executable is the sandbox binary and
//     matches the catalog checksum) and the server-address-resolves-to-this-host
//     check are Linux/root-only and not folded into LocalEvidencePasses yet.
type osCollector struct{}

// NewOSCollector returns a collector that probes this machine and cluster.
func NewOSCollector() Collector { return osCollector{} }

func (osCollector) Collect(ctx context.Context, _ Need, kubeconfigGiven bool) (Input, error) {
	in := Input{KubeconfigGiven: kubeconfigGiven}

	// Live machine ID. Absent on machines without /etc/machine-id (macOS, some
	// containers); the design skips the check there.
	_ = readMachineID() // live id; compared against the recorded one once #1235 lands.

	// TODO(#1235): the ProvisionRecord + recorded HostIdentity come from host.yaml.
	// Nothing is recorded yet, so these stay false/empty and a provisioned host
	// flows through branch B.
	in.RecordedMachineIDPresent = false
	in.HasProvisionRecord = false

	// Local evidence (the slice readable today: control-plane files + recorded
	// install state).
	passes, failed, unreadable, piece := collectLocalEvidence()
	in.LocalEvidencePasses = passes
	in.LocalEvidenceFailed = failed
	in.LocalEvidenceUnreadable = unreadable
	in.FailingPiece = piece

	// Live cluster: reachability and the kube-system UID.
	_, reachable := kube.ProbeCluster()
	in.APIReachable = reachable
	if reachable {
		if uid := liveClusterID(ctx); uid != "" {
			in.LiveClusterID = uid
		}
	}

	// TODO(#1235): ExternalStateExists is whether clusters/<uid>/ already holds
	// state. That directory does not exist on this branch, so it stays false.
	in.ExternalStateExists = false

	return in, nil
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
