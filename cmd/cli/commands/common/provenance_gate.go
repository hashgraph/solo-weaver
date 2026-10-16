// SPDX-License-Identifier: Apache-2.0

package common

import (
	"os"

	"github.com/hashgraph/solo-weaver/internal/provenance"
	"github.com/spf13/cobra"
)

// allowedOriginsEnv is an operator allowlist of the cluster origins weaver may
// run in on this host: a comma-separated subset of "provisioned" and "external".
// Unset or empty means no restriction (the gate's own verdict stands). It can
// only NARROW what is permitted — it is applied after the gate has independently
// classified the target, so it can turn an allowed origin into a refusal but can
// never grant an origin the gate did not verify, and never turns a stop into a
// pass. This is the safe inverse of a bypass: setting it tightens the tool, never
// loosens it.
const allowedOriginsEnv = "SOLO_PROVISIONER_ALLOWED_ORIGINS"

// keyProvenanceNeed is the command annotation that records a command's declared
// provenance Need (design section 5.3). Absent means NeedNone — the command is
// not gated. The annotation is inherited: declaring it on a parent gates the
// subtree.
const keyProvenanceNeed = "provenanceNeed"

// provenanceCollector is the collector the gate uses. It is a package var so that
// in-process tests can swap in a fake; it is deliberately NOT settable from the
// environment. The gate is a safety check, so its evidence must always come from
// real collection in a shipped binary — there is no runtime override that could
// force a verdict and skip the checks.
var provenanceCollector = provenance.NewOSCollector()

// SetProvenanceNeed declares what a command needs from the provenance gate. The
// shared pre-run (RunPersistentPreRun) reads it and runs the gate accordingly,
// after startup migrations. Commands that leave it unset default to NeedNone and
// are not gated. A gated command must not also SkipGlobalChecks, because the
// shared pre-run returns early for those before the gate runs.
func SetProvenanceNeed(cmd *cobra.Command, need provenance.Need) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[keyProvenanceNeed] = need.String()
}

// ProvenanceNeed returns the Need declared for cmd, walking up to a parent so a
// declaration on a command group covers its subcommands. Defaults to NeedNone.
func ProvenanceNeed(cmd *cobra.Command) provenance.Need {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations != nil {
			if v, ok := c.Annotations[keyProvenanceNeed]; ok {
				return provenance.ParseNeed(v)
			}
		}
	}
	return provenance.NeedNone
}

// RunProvenanceGate runs the cluster provenance gate for the given need. On
// success it returns the resolved Origin (provisioned or external) and a nil
// error; the caller uses the Origin to decide which post-gate startup migrations
// to run. A non-nil error means the command must be stopped: the target cluster is
// not safely this host's own, or the host's SOLO_PROVISIONER_ALLOWED_ORIGINS
// allowlist forbids the resolved origin. It is called from the shared pre-run,
// after the filesystem migrations, for any command that declared a need (see
// SetProvenanceNeed). Design section 5.
func RunProvenanceGate(cmd *cobra.Command, need provenance.Need) (provenance.Origin, error) {
	in, err := collectProvenanceInput(cmd, need)
	if err != nil {
		return 0, err
	}

	out := provenance.Gate(need, in)
	if out.Stopped() {
		return out.Origin, out.Err
	}

	// Apply the operator allowlist last. It can only narrow: an origin the gate
	// passed may still be refused here, but nothing the gate stopped is reopened.
	allowed, err := provenance.ParseAllowedOrigins(os.Getenv(allowedOriginsEnv))
	if err != nil {
		return out.Origin, err
	}
	if !provenance.OriginAllowed(allowed, out.Origin) {
		return out.Origin, provenance.RestrictedError(allowed, out.Origin)
	}

	return out.Origin, nil
}

// collectProvenanceInput gathers the gate Input from real collection.
func collectProvenanceInput(cmd *cobra.Command, need provenance.Need) (provenance.Input, error) {
	return provenanceCollector.Collect(cmd.Context(), need, kubeconfigGiven(cmd))
}

// kubeconfigGiven reports whether an explicit --kubeconfig was passed. The root
// flag is introduced by the resolver work (design step 1); until then this is
// false, which the gate reads as "the target is this host's own cluster".
func kubeconfigGiven(cmd *cobra.Command) bool {
	f := cmd.Flags().Lookup("kubeconfig")
	return f != nil && f.Changed
}
