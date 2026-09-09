// SPDX-License-Identifier: Apache-2.0

// Package privexec is the daemon's privileged-exec delegation seam.
//
// The solo-provisioner-daemon runs unprivileged (systemd `User=weaver`) and
// performs every privileged operation by exec'ing the root solo-provisioner CLI
// under sudo — the privilege-by-exec-delegation model documented in
// docs/dev/daemon/daemon-architecture.md and the BN traffic-shaper design's
// daemon-vs-CLI separation. This package is the ONLY place in the daemon's
// import closure that reaches for os/exec, and it execs nothing but sudo +
// solo-provisioner: never kubectl/helm/systemctl/nft/tc directly. Almost every
// delegation runs solo-provisioner under sudo; the sole exception is the
// traffic-shaper poll loop's unprivileged `reconcile-shaper --check` digest
// probe, which execs solo-provisioner directly (no sudo) because the check path
// touches no privileged state. The sudoers grant
// (internal/templates/files/weaver/sudoers) remains the single escalation path
// and is restricted to the solo-provisioner binary.
//
// The delegation is synchronous (run, wait, capture output) — distinct from the
// self-upgrade protocol's detached child spawn, which is a separate mechanism.
package privexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/automa-saga/daemonkit"
)

// cliBinName is the solo-provisioner CLI binary's file name. The daemon execs
// the sibling CLI installed alongside its own binary.
const cliBinName = "solo-provisioner"

// maxInlineStderr caps the child stderr execMessage inlines into a single
// journal field (#1035).
const maxInlineStderr = 512

// sudoBinCandidates are the absolute locations we look for the system sudo
// binary, in order. We never exec a bare "sudo" off PATH so a caller cannot
// hijack the escalation via an attacker-controlled directory (the same
// hardening the network exec runners apply to nft/tc — see
// docs/dev/security-model.md).
var sudoBinCandidates = []string{"/usr/bin/sudo", "/bin/sudo", "/usr/sbin/sudo"}

// cliBinCandidates are the solo-provisioner install paths the weaver sudoers
// grant permits, in resolution order. The daemon's own sibling path (resolved
// via os.Executable) is tried before these so a non-standard install stays
// self-consistent.
var cliBinCandidates = []string{
	"/opt/solo/weaver/bin/" + cliBinName,
	"/usr/local/bin/" + cliBinName,
}

// Delegator runs privileged solo-provisioner subcommands under sudo on behalf
// of the unprivileged daemon. Failures carry an operator-facing Reason and
// Resolution (via *daemonkit.ProbeError) so the caller can surface them through
// the component's /status endpoint.
// NamedIssue mirrors shaper.NamedIssue's JSON shape: a statusz FQDN needing an
// operator's attention, together with the policy set(s) its endpoints would
// have fed. Declared locally rather than importing internal/blocknode/shaper:
// the contract between the daemon and the CLI worker is JSON, not a shared Go
// type, the same reasoning as the digest-only decode this package already did
// before this field existed.
type NamedIssue struct {
	Name     string   `json:"name"`
	Policies []string `json:"policies"`
}

// ReconcileShaperCheckResult is what the daemon needs from one --check probe:
// the desired-membership digest (the poll loop's change-detection gate) and
// the statusz names that need an operator's attention. Unresolved/Stale/
// AAAAOnly never reach a human otherwise: the shaper package cannot log (see
// shaper.TestPackageEmitsNoLogs), and the CLI only prints them on the
// human-readable path, which nobody reads for a daemon-scheduled tick.
type ReconcileShaperCheckResult struct {
	Digest     string
	Unresolved []NamedIssue
	Stale      []NamedIssue
	AAAAOnly   []NamedIssue
}

// ReconcileShaperResult is what the daemon needs from one privileged apply:
// the same per-category attention lists as ReconcileShaperCheckResult, read
// back from the worker's `--output json` summary.
type ReconcileShaperResult struct {
	Unresolved []NamedIssue
	Stale      []NamedIssue
	AAAAOnly   []NamedIssue
}

type Delegator interface {
	// Run execs `sudo <solo-provisioner> <args...>`, waits for completion, and
	// returns the command's stdout. A missing sudo/CLI binary, a denied sudo
	// grant, or a non-zero exit returns a *daemonkit.ProbeError describing the
	// failure and how to resolve it; on success err is nil and the returned
	// bytes are stdout only (stderr is folded into the error, never stdout, so
	// callers can parse `--output json`).
	Run(ctx context.Context, args ...string) ([]byte, error)

	// NetworkPolicySet delegates `network policy set --name <name>
	// [--cidrs <csv>]` — the statusz poll loop's membership write path. It
	// atomically replaces the named policy's live nft set with cidrs; an empty
	// (or nil) cidrs slice clears the set (the --cidrs flag is omitted).
	NetworkPolicySet(ctx context.Context, name string, cidrs []string) error

	// TCAttach delegates `block node tc-attach --veth <veth>` — the pod-lifecycle
	// watcher's ingress-HTB install path. It installs the $VETH HTB hierarchy on
	// the given host-side veth from the recorded ingress shape config.
	TCAttach(ctx context.Context, veth string) error

	// TCDetach delegates `block node tc-attach --veth <veth> --detach` — the
	// best-effort teardown of the $VETH HTB hierarchy on pod delete or before a
	// clean re-attach.
	TCDetach(ctx context.Context, veth string) error

	// ReconcileShaper delegates `block node reconcile-shaper --statusz-url <url>
	// --output json` under sudo — the traffic-shaper poll loop's privileged
	// apply path. The worker fetches statusz, diffs the live nft policy sets,
	// and rewrites only the policies whose membership changed. --output json is
	// requested so the daemon can read back the names statusz reported that
	// need an operator's attention (see ReconcileShaperResult); the log line
	// this normally prints on the human-readable path never reaches a human on
	// the daemon-scheduled path, so this is the only way it surfaces at all.
	ReconcileShaper(ctx context.Context, statuszURL string) (ReconcileShaperResult, error)

	// ReconcileShaperCheck delegates the unprivileged
	// `block node reconcile-shaper --statusz-url <url> --check --output json`
	// probe (no sudo, no nft) and returns the desired-membership digest plus
	// any names needing attention. The poll loop calls it every tick and only
	// invokes the privileged ReconcileShaper when the digest changed, so a
	// steady-state roster costs no root escalation.
	ReconcileShaperCheck(ctx context.Context, statuszURL string) (ReconcileShaperCheckResult, error)
}

// execDelegator is the production Delegator. Its resolution and exec seams are
// injectable so unit tests can assert the constructed argv and the failure
// mapping without a real sudo or CLI binary on the host.
type execDelegator struct {
	// executable resolves the running daemon binary's path so the sibling CLI
	// can be preferred. Defaults to os.Executable.
	executable func() (string, error)
	// stat reports whether a candidate binary path exists. Defaults to os.Stat.
	stat func(string) (os.FileInfo, error)
	// output runs name+args and returns stdout; on a non-zero exit the returned
	// error is an *exec.ExitError whose Stderr carries the CLI's stderr.
	// Defaults to exec.CommandContext(...).Output().
	output func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// New returns a Delegator wired to the host's sudo and solo-provisioner
// binaries.
func New() Delegator {
	return &execDelegator{
		executable: os.Executable,
		stat:       os.Stat,
		output: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		},
	}
}

func (d *execDelegator) Run(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, &daemonkit.ProbeError{
			Reason:     "PrivilegedExecNoArgs",
			Message:    "privileged delegation called with no solo-provisioner arguments",
			Resolution: "this is a daemon bug; report it with the daemon logs",
		}
	}

	sudoBin, err := d.resolve(sudoBinCandidates, "SudoBinaryNotFound",
		"install sudo, or verify it exists at one of: "+strings.Join(sudoBinCandidates, ", "))
	if err != nil {
		return nil, err
	}

	cliBin, err := d.resolveCLI()
	if err != nil {
		return nil, err
	}

	// argv is `sudo -n <cli> <args...>`. -n makes sudo non-interactive: a missing
	// or expired grant exits immediately (a daemon has no tty to prompt on)
	// rather than blocking, and surfaces as the ProbeError below. The CLI path is
	// passed as an explicit argument (not via PATH) so it matches the absolute
	// paths the sudoers grant authorizes.
	argv := append([]string{"-n", cliBin}, args...)
	out, err := d.output(ctx, sudoBin, argv...)
	if err != nil {
		return out, &daemonkit.ProbeError{
			Reason:     "PrivilegedExecFailed",
			Message:    privilegedExecMessage(cliBin, args, err),
			Resolution: "verify the weaver sudoers grant (/etc/sudoers.d/solo-provisioner) permits `sudo " + cliBinName + "`, then reproduce manually: sudo " + cliBin + " " + strings.Join(args, " "),
			Err:        err,
		}
	}
	return out, nil
}

func (d *execDelegator) NetworkPolicySet(ctx context.Context, name string, cidrs []string) error {
	if strings.TrimSpace(name) == "" {
		return &daemonkit.ProbeError{
			Reason:     "PolicyNameEmpty",
			Message:    "network policy set requires a non-empty policy name",
			Resolution: "this is a daemon bug; report it with the daemon logs",
		}
	}
	args := []string{"network", "policy", "set", "--name", name}
	if len(cidrs) > 0 {
		args = append(args, "--cidrs", strings.Join(cidrs, ","))
	}
	_, err := d.Run(ctx, args...)
	return err
}

func (d *execDelegator) TCAttach(ctx context.Context, veth string) error {
	return d.tcAttach(ctx, veth, false)
}

func (d *execDelegator) TCDetach(ctx context.Context, veth string) error {
	return d.tcAttach(ctx, veth, true)
}

func (d *execDelegator) ReconcileShaper(ctx context.Context, statuszURL string) (ReconcileShaperResult, error) {
	if strings.TrimSpace(statuszURL) == "" {
		return ReconcileShaperResult{}, &daemonkit.ProbeError{
			Reason:     "StatuszURLEmpty",
			Message:    "block node reconcile-shaper requires a non-empty statusz URL",
			Resolution: "this is a daemon bug; report it with the daemon logs",
		}
	}
	out, err := d.Run(ctx, "block", "node", "reconcile-shaper", "--statusz-url", statuszURL, "--output", "json")
	if err != nil {
		return ReconcileShaperResult{}, err
	}
	var res struct {
		Unresolved []NamedIssue `json:"unresolved"`
		Stale      []NamedIssue `json:"stale"`
		AAAAOnly   []NamedIssue `json:"aaaa-only"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return ReconcileShaperResult{}, &daemonkit.ProbeError{
			Reason:     "ReconcileShaperParseFailed",
			Message:    "could not parse reconcile-shaper --output json result",
			Resolution: "this is a daemon/CLI contract bug; report it with the daemon logs",
			Err:        err,
		}
	}
	return ReconcileShaperResult{Unresolved: res.Unresolved, Stale: res.Stale, AAAAOnly: res.AAAAOnly}, nil
}

// ReconcileShaperCheck execs the reconcile-shaper worker's unprivileged --check
// path directly (not under sudo): it fetches statusz and prints the
// desired-membership digest as JSON without reading or writing any nft state, so
// no escalation is needed. It parses `desired-digest` from that JSON and returns
// it. The daemon-facing name of the CLI is resolved the same way Run resolves
// it, so the sibling-preference and granted-path fallbacks apply identically.
func (d *execDelegator) ReconcileShaperCheck(ctx context.Context, statuszURL string) (ReconcileShaperCheckResult, error) {
	if strings.TrimSpace(statuszURL) == "" {
		return ReconcileShaperCheckResult{}, &daemonkit.ProbeError{
			Reason:     "StatuszURLEmpty",
			Message:    "block node reconcile-shaper --check requires a non-empty statusz URL",
			Resolution: "this is a daemon bug; report it with the daemon logs",
		}
	}

	cliBin, err := d.resolveCLI()
	if err != nil {
		return ReconcileShaperCheckResult{}, err
	}

	// No sudo: --check reads only statusz over HTTP and touches no nft state, so
	// it runs as the unprivileged daemon user. --output json makes the digest
	// machine-parseable.
	args := []string{"block", "node", "reconcile-shaper", "--statusz-url", statuszURL, "--check", "--output", "json"}
	out, err := d.output(ctx, cliBin, args...)
	if err != nil {
		return ReconcileShaperCheckResult{}, &daemonkit.ProbeError{
			Reason:     "ReconcileShaperCheckFailed",
			Message:    unprivilegedExecMessage(cliBin, args, err),
			Resolution: "reproduce manually as the daemon user: " + cliBin + " " + strings.Join(args, " "),
			Err:        err,
		}
	}

	var res struct {
		Digest     string       `json:"desired-digest"`
		Unresolved []NamedIssue `json:"unresolved"`
		Stale      []NamedIssue `json:"stale"`
		AAAAOnly   []NamedIssue `json:"aaaa-only"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return ReconcileShaperCheckResult{}, &daemonkit.ProbeError{
			Reason:     "ReconcileShaperCheckParseFailed",
			Message:    "could not parse reconcile-shaper --check --output json digest",
			Resolution: "this is a daemon/CLI contract bug; report it with the daemon logs",
			Err:        err,
		}
	}
	// A missing or empty desired-digest means the CLI's --check contract drifted
	// (the field vanished or renamed). Fail fast: an empty digest would otherwise
	// be treated as a real membership value by the poll loop's digest gate and
	// silently suppress the privileged apply until the next forced resync.
	if res.Digest == "" {
		return ReconcileShaperCheckResult{}, &daemonkit.ProbeError{
			Reason:     "ReconcileShaperCheckEmptyDigest",
			Message:    "reconcile-shaper --check returned an empty desired-digest",
			Resolution: "this is a daemon/CLI contract bug; report it with the daemon logs",
		}
	}
	return ReconcileShaperCheckResult{Digest: res.Digest, Unresolved: res.Unresolved, Stale: res.Stale, AAAAOnly: res.AAAAOnly}, nil
}

// tcAttach delegates the `block node tc-attach --veth <veth> [--detach]` exec.
// The veth-name format is validated by the CLI/shape layer the exec reaches;
// here we only guard against an empty name so a daemon bug surfaces as a clear
// ProbeError rather than a malformed argv.
func (d *execDelegator) tcAttach(ctx context.Context, veth string, detach bool) error {
	if strings.TrimSpace(veth) == "" {
		return &daemonkit.ProbeError{
			Reason:     "VethNameEmpty",
			Message:    "block node tc-attach requires a non-empty veth name",
			Resolution: "this is a daemon bug; report it with the daemon logs",
		}
	}
	args := []string{"block", "node", "tc-attach", "--veth", veth}
	if detach {
		args = append(args, "--detach")
	}
	_, err := d.Run(ctx, args...)
	return err
}

// resolve returns the first candidate that exists on disk, or a ProbeError with
// the given reason/resolution when none do.
func (d *execDelegator) resolve(candidates []string, reason, resolution string) (string, error) {
	for _, c := range candidates {
		if _, err := d.stat(c); err == nil {
			return c, nil
		}
	}
	return "", &daemonkit.ProbeError{
		Reason:     reason,
		Message:    "no usable binary found in any known path: " + strings.Join(candidates, ", "),
		Resolution: resolution,
	}
}

// resolveCLI resolves the solo-provisioner CLI binary, preferring the sibling of
// the running daemon binary and falling back to the sudoers-granted install
// paths.
func (d *execDelegator) resolveCLI() (string, error) {
	candidates := cliBinCandidates
	if exe, err := d.executable(); err == nil {
		sibling := filepath.Join(filepath.Dir(exe), cliBinName)
		// Prefer the sibling; keep the granted paths as fallbacks.
		candidates = append([]string{sibling}, cliBinCandidates...)
	}
	return d.resolve(candidates, "CLIBinaryNotFound",
		"reinstall the solo-provisioner CLI, or verify it exists at one of: "+strings.Join(cliBinCandidates, ", "))
}

// privilegedExecMessage builds the human-readable failure message, preferring
// the CLI's stderr (available on a non-zero exit) over the raw exec error.
func privilegedExecMessage(cliBin string, args []string, err error) string {
	return execMessage("sudo "+cliBin, args, err)
}

// unprivilegedExecMessage is privilegedExecMessage's no-sudo counterpart for the
// reconcile-shaper --check probe.
func unprivilegedExecMessage(cliBin string, args []string, err error) string {
	return execMessage(cliBin, args, err)
}

// execMessage builds the human-readable failure message for a prefix (the sudo+
// CLI or the bare CLI) and argv, preferring the CLI's stderr (available on a
// non-zero exit) over the raw exec error. The inlined stderr is bounded by
// maxInlineStderr.
func execMessage(prefix string, args []string, err error) string {
	base := prefix + " " + strings.Join(args, " ") + " failed"
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			return base + ": " + truncateStderr(stderr, maxInlineStderr)
		}
	}
	return base + ": " + err.Error()
}

// truncateStderr returns s unchanged when it fits within limit bytes; otherwise
// it cuts at the last rune boundary within limit and appends a marker with the
// kept/total byte counts.
func truncateStderr(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	// Binary (non-UTF-8) stderr has no rune boundary to back up to; cut at the limit.
	if limit-cut >= utf8.UTFMax {
		cut = limit
	}
	return s[:cut] + " ... [stderr truncated at " + strconv.Itoa(cut) + "/" + strconv.Itoa(len(s)) + " bytes]"
}
