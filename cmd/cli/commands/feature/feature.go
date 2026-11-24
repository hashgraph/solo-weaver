// SPDX-License-Identifier: Apache-2.0

// Package feature implements the operator-facing `feature-gate` command: a read-only
// listing of solo-provisioner's gatekit feature-gate catalog and each gate's maturity,
// activation point, and current state. It is a pure reader of internal/featuregate — it
// hides and gates nothing (that is #1392).
package feature

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	gatekit "github.com/automa-saga/gatekit"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// featureRow is one feature's operator-facing view. ActiveFrom (the activation
// version the feature first turns on at — gatekit's FeatureSpec.ActiveFrom) and
// Maturity are the feature's *declared* target, so the roadmap is visible even
// before that version is reached; Enabled is its resolved state right now at the
// in-force activation version with no opt-in.
type featureRow struct {
	Feature    string `json:"feature"`
	Maturity   string `json:"maturity"`
	ActiveFrom uint   `json:"activeFrom"`
	Enabled    bool   `json:"enabled"`
	// Ready reports whether an operator opt-in (--experimental / env var) can enable
	// this feature: a ready ALPHA is Default:true; a dormant ALPHA
	// (Default:false) is unfinished and cannot be turned on by any opt-in.
	Ready       bool   `json:"ready"`
	EnvVar      string `json:"envVar"`
	Description string `json:"description"`
}

type listing struct {
	ActivationVersion uint         `json:"activationVersion"`
	Features          []featureRow `json:"features"`
}

// filters holds the optional list filters. A zero value means "no filter".
type filters struct {
	activationVersion uint
	byActivation      bool
	maturity          string // upper-case, "" = no filter
	enabled           *bool  // nil = no filter
}

var knownMaturities = map[string]bool{
	string(gatekit.Alpha): true, string(gatekit.Beta): true,
	string(gatekit.GA): true, string(gatekit.Deprecated): true,
}

func readFilters(cmd *cobra.Command) (filters, error) {
	var f filters
	// 0 means "all" (per the flag help), so only filter on a non-zero value.
	if v, _ := cmd.Flags().GetUint("activation-version"); v != 0 {
		f.activationVersion = v
		f.byActivation = true
	}
	if m, _ := cmd.Flags().GetString("maturity"); m != "" {
		up := strings.ToUpper(m)
		if !knownMaturities[up] {
			return f, errorx.IllegalArgument.New(
				"unknown maturity %q (want alpha, beta, ga, or deprecated)", m)
		}
		f.maturity = up
	}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		f.enabled = &v
	}
	return f, nil
}

func (f filters) apply(rows []featureRow) []featureRow {
	out := make([]featureRow, 0, len(rows))
	for _, r := range rows {
		if f.byActivation && r.ActiveFrom != f.activationVersion {
			continue
		}
		if f.maturity != "" && r.Maturity != f.maturity {
			continue
		}
		if f.enabled != nil && r.Enabled != *f.enabled {
			continue
		}
		out = append(out, r)
	}
	return out
}

// GetCmd returns the `feature-gate` command. Running it (or `feature-gate list`) prints
// the catalog of gated behaviours and their maturity. Stable (GA) commands are not
// gated and so are not listed — they are discoverable via `--help`.
func GetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feature-gate",
		Short: "List feature gates and their maturity",
		Long: "List solo-provisioner's feature gates: each gate's maturity " +
			"(ALPHA/BETA/GA), the activation version it turns on at, whether it is " +
			"currently enabled, and a short description. Stable commands are not gated " +
			"and are not listed here — see --help for the full command surface.",
		RunE: runList,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List feature gates and their maturity",
		RunE:  runList,
	})

	// Filters, persistent so `feature-gate list` inherits them.
	cmd.PersistentFlags().Uint("activation-version", 0,
		"Show only features that activate at this activation version (0 = all).")
	cmd.PersistentFlags().String("maturity", "",
		"Show only features with this maturity (alpha|beta|ga|deprecated).")
	cmd.PersistentFlags().Bool("enabled", false,
		"Show only enabled features; --enabled=false shows only disabled.")

	// This is a read-only listing that must run unprivileged: it needs neither
	// the weaver-installation check nor the startup migrations (which read the
	// root-only state file). Skip global checks on the group AND every
	// subcommand — the pre-run runs against the invoked leaf, so marking only
	// the parent would leave `feature list` still gated.
	common.SkipGlobalChecks(cmd)
	for _, sub := range cmd.Commands() {
		common.SkipGlobalChecks(sub)
	}
	return cmd
}

func runList(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("output")

	flt, err := readFilters(cmd)
	if err != nil {
		return err
	}

	gate, err := featuregate.NewDefault()
	if err != nil {
		return err
	}

	data := build(gate)
	data.Features = flt.apply(data.Features)

	switch strings.ToLower(format) {
	case "json":
		out, mErr := json.MarshalIndent(data, "", "  ")
		if mErr != nil {
			return mErr
		}
		// Write to stdout, not stderr: cobra's cmd.Print* use OutOrStderr, which
		// would break `feature-gate list -o json | jq`.
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
	case "", "text":
		fmt.Fprint(cmd.OutOrStdout(), render(data))
	default:
		return errorx.IllegalFormat.New("unsupported format %q (want text or json)", format)
	}
	return nil
}

// build assembles the listing from the catalog (for the declared maturity /
// activation point) and the gate (for the current resolved state).
func build(gate *gatekit.FeatureGate) listing {
	catalog := featuregate.Catalog()

	names := make([]string, 0, len(catalog))
	for f := range catalog {
		names = append(names, string(f))
	}
	sort.Strings(names)

	rows := make([]featureRow, 0, len(names))
	for _, n := range names {
		f := gatekit.Feature(n)
		def := catalog[f]
		row := featureRow{
			Feature:     n,
			Maturity:    "-",
			Enabled:     gate.Enabled(f),
			EnvVar:      featuregate.OverrideEnvVar(f),
			Description: def.Description,
		}
		if len(def.Specs) > 0 {
			// First spec = when the feature first activates (ActiveFrom); last spec
			// = its current declared maturity target. Maturity is reported in
			// gatekit's canonical upper-case form (ALPHA/BETA/GA/DEPRECATED).
			row.ActiveFrom = uint(def.Specs[0].ActiveFrom)
			row.Maturity = string(def.Specs[len(def.Specs)-1].Maturity)
			// Readiness: the effective spec at the in-force version is default-on.
			// Specs are ascending by ActiveFrom, so the last one reached wins — the
			// same rule gatekit's WithExperimental (ExperimentalReady) applies.
			for _, s := range def.Specs {
				if s.ActiveFrom <= featuregate.CurrentActivationVersion {
					row.Ready = s.Default
				}
			}
		}
		rows = append(rows, row)
	}

	return listing{
		ActivationVersion: uint(featuregate.CurrentActivationVersion),
		Features:          rows,
	}
}

func render(data listing) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Activation version: %d\n\n", data.ActivationVersion)

	if len(data.Features) == 0 {
		b.WriteString("No features match the given filters.\n")
		return b.String()
	}

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FEATURE\tMATURITY\tACTIVE FROM\tENABLED\tDESCRIPTION")
	for _, r := range data.Features {
		enabled := "no"
		if r.Enabled {
			enabled = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n",
			r.Feature, r.Maturity, r.ActiveFrom, enabled, r.Description)
	}
	_ = tw.Flush()

	// Split ALPHA features by readiness. A ready ALPHA (Default:true) can be
	// opted into; a dormant ALPHA (Default:false) is unfinished and cannot be turned
	// on by any opt-in, so we must not advertise an opt-in for it.
	var readyAlpha, dormantAlpha []featureRow
	for _, r := range data.Features {
		if r.Maturity != string(gatekit.Alpha) {
			continue
		}
		if r.Ready {
			readyAlpha = append(readyAlpha, r)
		} else {
			dormantAlpha = append(dormantAlpha, r)
		}
	}

	if len(readyAlpha) > 0 {
		b.WriteString("\nThese ALPHA features are off by default. Enable one for non-production testing by:\n")
		b.WriteString("  - passing --experimental to its command (scoped to that command only), or\n")
		b.WriteString("  - setting its opt-in env var to 1 (the exact UPPER_SNAKE_CASE name):\n")
		et := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, r := range readyAlpha {
			fmt.Fprintf(et, "      %s=1\t# %s\n", r.EnvVar, r.Feature)
		}
		_ = et.Flush()
	}

	if len(dormantAlpha) > 0 {
		b.WriteString("\nThese ALPHA features are under active development and cannot be enabled yet\n")
		b.WriteString("(no opt-in will turn them on until they are marked ready):\n")
		dt := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, r := range dormantAlpha {
			fmt.Fprintf(dt, "      %s\t# %s\n", r.Feature, r.Description)
		}
		_ = dt.Flush()
	}
	return b.String()
}
