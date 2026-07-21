// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"strings"
	"testing"
)

// directiveLines returns the non-comment, non-blank lines of an embedded config
// file. Comment prose is stripped because both the sudoers file and the daemon
// service unit deliberately *name* things they do NOT do (raw tools that are
// not granted; NoNewPrivileges that is intentionally omitted), so assertions
// must run against the effective directives, not the explanatory text.
func directiveLines(t *testing.T, name string) string {
	t.Helper()
	data, err := Read(name)
	if err != nil {
		t.Fatalf("read embedded %s: %v", name, err)
	}
	var b strings.Builder
	for _, ln := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		b.WriteString(ln)
		b.WriteString("\n")
	}
	return b.String()
}

// TestSudoersGrantsOnlySoloProvisioner locks the privilege model: the daemon's
// only escalation path is exec'ing the solo-provisioner CLI under sudo, so the
// grant must cover that binary at both install paths and must NOT hand out any
// raw tool (kubectl/helm/kubeadm/systemctl/nft/tc/mkdir), which would grant
// root-equivalent access far beyond the audited CLI surface.
func TestSudoersGrantsOnlySoloProvisioner(t *testing.T) {
	grant := directiveLines(t, "files/weaver/sudoers")

	for _, want := range []string{
		"/opt/solo/weaver/bin/solo-provisioner",
		"/usr/local/bin/solo-provisioner",
	} {
		if !strings.Contains(grant, want) {
			t.Errorf("sudoers grant missing expected path %q\n--- grant ---\n%s", want, grant)
		}
	}

	// No raw-tool grants may leak into the escalation surface.
	for _, forbidden := range []string{"kubectl", "helm", "kubeadm", "systemctl", "mkdir", "/nft", "/tc"} {
		if strings.Contains(grant, forbidden) {
			t.Errorf("sudoers grant unexpectedly references raw tool %q — the daemon must escalate only via solo-provisioner\n--- grant ---\n%s", forbidden, grant)
		}
	}
}

// TestDaemonServiceRunsUnprivileged locks the two systemd invariants the
// privilege model depends on: the daemon runs as the unprivileged weaver user,
// and NoNewPrivileges is not enabled (which would block the sudo escalation the
// delegation model relies on).
func TestDaemonServiceRunsUnprivileged(t *testing.T) {
	unit := directiveLines(t, "files/weaver/solo-provisioner-daemon.service")

	for _, want := range []string{"User=weaver", "Group=weaver"} {
		if !strings.Contains(unit, want) {
			t.Errorf("daemon service unit missing %q\n--- unit ---\n%s", want, unit)
		}
	}

	// NoNewPrivileges must be omitted entirely, not merely set to a false-y
	// value: any true-ish form (true/yes/1/on) would block the sudo setuid
	// escalation the delegation model relies on. Assert the directive is absent
	// altogether rather than matching one spelling of "on".
	if strings.Contains(unit, "NoNewPrivileges") {
		t.Errorf("daemon service unit sets a NoNewPrivileges directive; it must be omitted so sudo delegation works\n--- unit ---\n%s", unit)
	}
}

// TestDaemonServiceGrantsRuntimeDir locks the invariant that the daemon's
// privileged-exec children can write their tc/nft flock under
// /run/solo-provisioner. ProtectSystem=strict makes /run read-only, so without a
// RuntimeDirectory the exec'd `block node tc-attach` / `network policy set` fail
// with "read-only file system" when opening /run/solo-provisioner/network/.*
// lock files.
func TestDaemonServiceGrantsRuntimeDir(t *testing.T) {
	unit := directiveLines(t, "files/weaver/solo-provisioner-daemon.service")

	if !strings.Contains(unit, "ProtectSystem=strict") {
		t.Fatalf("expected ProtectSystem=strict (the reason the runtime dir grant is required)\n--- unit ---\n%s", unit)
	}
	// RuntimeDirectory=solo-provisioner makes /run/solo-provisioner writable inside
	// the sandbox (and recreates it after reboot, since /run is tmpfs).
	if !strings.Contains(unit, "RuntimeDirectory=solo-provisioner") {
		t.Errorf("daemon service unit must set RuntimeDirectory=solo-provisioner so exec'd tc/nft workers can write their flock under /run/solo-provisioner\n--- unit ---\n%s", unit)
	}
}
