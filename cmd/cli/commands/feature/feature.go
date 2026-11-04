// SPDX-License-Identifier: Apache-2.0

// Package feature implements the operator-facing `feature` command: a read-only
// listing of solo-provisioner's gatekit feature catalog and each feature's
// maturity, activation point, and current state. It is a pure reader of
// internal/featuregate — it hides and gates nothing (that is #1392).
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
	Feature     string `json:"feature"`
	Maturity    string `json:"maturity"`
	ActiveFrom  uint   `json:"activeFrom"`
	Enabled     bool   `json:"enabled"`
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
	if cmd.Flags().Changed("activation-version") {
		f.activationVersion, _ = cmd.Flags().GetUint("activation-version")
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

// GetCmd returns the `feature` command. Running it (or `feature list`) prints the
// catalog; an experimental feature is not excluded — operators see everything
// and its maturity.
func GetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feature",
		Short: "List available features and their maturity",
		Long: "List solo-provisioner's feature catalog: each feature's maturity " +
			"(ALPHA/BETA/GA), the activation version it turns on at, whether it is " +
			"currently enabled, and a short description.",
		RunE: runList,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available features and their maturity",
		RunE:  runList,
	})

	// Filters, persistent so `feature list` inherits them.
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
		cmd.Println(string(out))
	case "", "text":
		cmd.Print(render(data))
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

	// List the exact opt-in env var for each ALPHA (off-by-default) feature so
	// operators never have to guess the UPPER_SNAKE_CASE form.
	var alpha []featureRow
	for _, r := range data.Features {
		if r.Maturity == string(gatekit.Alpha) {
			alpha = append(alpha, r)
		}
	}
	if len(alpha) > 0 {
		b.WriteString("\nALPHA features are off by default. Enable all of them with --experimental, " +
			"or one at a time by setting its opt-in env var to 1:\n")
		et := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for _, r := range alpha {
			fmt.Fprintf(et, "  %s=1\t# %s\n", r.EnvVar, r.Feature)
		}
		_ = et.Flush()
	}
	return b.String()
}
