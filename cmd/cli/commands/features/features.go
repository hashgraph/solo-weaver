// SPDX-License-Identifier: Apache-2.0

// Package features implements the operator-facing `features` command: a
// read-only listing of solo-provisioner's gatekit feature catalog and each
// feature's maturity, activation point, and current state. It is a pure reader
// of internal/featuregate — it hides and gates nothing (that is #1392).
package features

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	gatekit "github.com/automa-saga/gatekit"
	"github.com/hashgraph/solo-weaver/internal/featuregate"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

// featureRow is one feature's operator-facing view. Maturity and ActivatesAt are
// the feature's *declared* target (its latest spec), so the roadmap is visible
// even before a feature's activation version is reached; Enabled is its resolved
// state right now at the in-force activation version with no opt-in.
type featureRow struct {
	Feature     string `json:"feature"`
	Maturity    string `json:"maturity"`
	ActivatesAt uint   `json:"activatesAt"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

type listing struct {
	ActivationVersion uint         `json:"activationVersion"`
	Features          []featureRow `json:"features"`
}

// GetCmd returns the `features` command. Running it (or `features list`) prints
// the catalog; a hidden/experimental feature is *not* excluded — operators see
// everything and its maturity.
func GetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "features",
		Short: "List available features and their maturity",
		Long: "List solo-provisioner's feature catalog: each feature's maturity " +
			"(alpha/beta/ga), the activation version it turns on at, whether it is " +
			"currently enabled, and a short description.",
		RunE: runList,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List available features and their maturity",
		RunE:  runList,
	})
	return cmd
}

func runList(cmd *cobra.Command, _ []string) error {
	format, _ := cmd.Flags().GetString("output")

	gate, err := featuregate.NewDefault()
	if err != nil {
		return err
	}

	data := build(gate)

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
			Description: def.Description,
		}
		if len(def.Specs) > 0 {
			// First spec = when the feature first activates; last spec = its
			// current declared maturity target.
			row.ActivatesAt = uint(def.Specs[0].ActiveFrom)
			row.Maturity = strings.ToLower(string(def.Specs[len(def.Specs)-1].Maturity))
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

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FEATURE\tMATURITY\tACTIVATES\tENABLED\tDESCRIPTION")
	for _, r := range data.Features {
		enabled := "no"
		if r.Enabled {
			enabled = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\tv%d\t%s\t%s\n",
			r.Feature, r.Maturity, r.ActivatesAt, enabled, r.Description)
	}
	_ = tw.Flush()

	b.WriteString("\nAlpha features are off by default; enable them with --experimental " +
		"(or SOLO_PROVISIONER_ENABLE_<FEATURE>=1) in non-production environments.\n")
	return b.String()
}
