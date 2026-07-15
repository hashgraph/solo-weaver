// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"testing"
	"time"

	shp "github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// resetFlags clears package-level flag vars and cobra's per-flag Changed state.
// Call via defer at the top of every test that mutates these vars so state
// does not leak into subsequent tests.
func resetFlags() {
	flagClass = ""
	flagDevice = ""
	flagRate = ""
	flagCeil = ""
	flagPrio = 0
	flagDefault = ""
	for _, cmd := range []*cobra.Command{createCmd, setCmd, showCmd, deleteCmd} {
		cmd.Flags().VisitAll(func(f *pflag.Flag) { f.Changed = false })
	}
}

// --- Structure tests ---

func TestShapeCmd_HasExpectedSubcommands(t *testing.T) {
	subs := map[string]bool{}
	for _, c := range shapeCmd.Commands() {
		subs[c.Name()] = true
	}
	for _, want := range []string{"create", "set", "show", "delete"} {
		require.True(t, subs[want], "missing subcommand %q", want)
	}
}

func TestGetCmd_ReturnsShapeCmd(t *testing.T) {
	require.Equal(t, "shape", GetCmd().Name())
}

func TestCreateCmd_FlagsRegistered(t *testing.T) {
	for _, flag := range []string{"class", "device", "rate", "ceil", "prio", "default"} {
		require.NotNil(t, createCmd.Flags().Lookup(flag), "create: missing --%s", flag)
	}
}

func TestSetCmd_FlagsRegistered(t *testing.T) {
	for _, flag := range []string{"class", "rate", "ceil", "prio"} {
		require.NotNil(t, setCmd.Flags().Lookup(flag), "set: missing --%s", flag)
	}
}

func TestShowCmd_FlagsRegistered(t *testing.T) {
	for _, flag := range []string{"class", "device"} {
		require.NotNil(t, showCmd.Flags().Lookup(flag), "show: missing --%s", flag)
	}
}

func TestDeleteCmd_FlagsRegistered(t *testing.T) {
	for _, flag := range []string{"class", "device"} {
		require.NotNil(t, deleteCmd.Flags().Lookup(flag), "delete: missing --%s", flag)
	}
}

// --- create: mutual exclusion (validation fires before newManager is called) ---

func TestCreateCmd_BothClassAndDevice_Error(t *testing.T) {
	defer resetFlags()
	flagClass = "partner"
	flagDevice = "egress"

	err := createCmd.RunE(createCmd, nil)
	require.ErrorContains(t, err, "mutually exclusive")
}

func TestCreateCmd_NeitherClassNorDevice_Error(t *testing.T) {
	defer resetFlags()

	err := createCmd.RunE(createCmd, nil)
	require.ErrorContains(t, err, "exactly one of")
}

// --- create --class: flag-required validation (fires before manager) ---

func TestRunCreateClass_MissingRate_Error(t *testing.T) {
	defer resetFlags()
	// Construct a minimal cmd with only the flags runCreateClass inspects.
	cmd := &cobra.Command{}
	cmd.Flags().StringVar(&flagRate, "rate", "", "")
	// --rate not Set → Changed returns false → runCreateClass errors.
	err := runCreateClass(cmd, nil, false)
	require.ErrorContains(t, err, "--rate is required")
}

// --- create --device: flag-required validation ---

func TestRunCreateDevice_MissingRate_Error(t *testing.T) {
	defer resetFlags()
	cmd := &cobra.Command{}
	cmd.Flags().StringVar(&flagRate, "rate", "", "")
	cmd.Flags().StringVar(&flagDefault, "default", "", "")
	// --rate not Changed
	err := runCreateDevice(cmd, nil, false)
	require.ErrorContains(t, err, "--rate is required")
}

func TestRunCreateDevice_MissingDefault_Error(t *testing.T) {
	defer resetFlags()
	cmd := &cobra.Command{}
	cmd.Flags().StringVar(&flagRate, "rate", "", "")
	cmd.Flags().StringVar(&flagDefault, "default", "", "")
	require.NoError(t, cmd.Flags().Set("rate", "1gbit"))
	// --default not Changed
	err := runCreateDevice(cmd, nil, false)
	require.ErrorContains(t, err, "--default is required")
}

// --- set: early validation (fires before newManager is called) ---

func TestSetCmd_NoClass_Error(t *testing.T) {
	defer resetFlags()
	// flagClass is "" — RunE should return before calling the manager.
	// We need a cmd with the same RunE but its own fresh FlagSet so Changed()
	// works correctly and we do not pollute the shared setCmd FlagSet.
	cmd := &cobra.Command{
		RunE: setCmd.RunE,
	}
	cmd.Flags().StringVar(&flagClass, "class", "", "")
	cmd.Flags().StringVar(&flagRate, "rate", "", "")
	cmd.Flags().StringVar(&flagCeil, "ceil", "", "")
	cmd.Flags().IntVar(&flagPrio, "prio", 0, "")
	require.NoError(t, cmd.Flags().Set("rate", "200mbit"))

	err := cmd.RunE(cmd, nil)
	require.ErrorContains(t, err, "--class is required")
}

func TestSetCmd_NoChangedBandwidthFlag_Error(t *testing.T) {
	defer resetFlags()
	cmd := &cobra.Command{
		RunE: setCmd.RunE,
	}
	cmd.Flags().StringVar(&flagClass, "class", "", "")
	cmd.Flags().StringVar(&flagRate, "rate", "", "")
	cmd.Flags().StringVar(&flagCeil, "ceil", "", "")
	cmd.Flags().IntVar(&flagPrio, "prio", 0, "")
	// Set --class but do not touch --rate/--ceil/--prio.
	require.NoError(t, cmd.Flags().Set("class", "partner"))

	err := cmd.RunE(cmd, nil)
	require.ErrorContains(t, err, "at least one of --rate, --ceil, or --prio")
}

// --- show: mutual exclusion (fires before newManager) ---

func TestShowCmd_BothClassAndDevice_Error(t *testing.T) {
	defer resetFlags()
	flagClass = "partner"
	flagDevice = "egress"

	err := showCmd.RunE(showCmd, nil)
	require.ErrorContains(t, err, "mutually exclusive")
}

// --- delete: mutual exclusion and required-one-of validation ---

func TestDeleteCmd_BothClassAndDevice_Error(t *testing.T) {
	defer resetFlags()
	flagClass = "partner"
	flagDevice = "egress"

	err := deleteCmd.RunE(deleteCmd, nil)
	require.ErrorContains(t, err, "mutually exclusive")
}

func TestDeleteCmd_NeitherClassNorDevice_Error(t *testing.T) {
	defer resetFlags()

	err := deleteCmd.RunE(deleteCmd, nil)
	require.ErrorContains(t, err, "exactly one of")
}

// --- model field tests ---

func TestClassConfig_CeilDefaultsToEmpty(t *testing.T) {
	cls := &shp.ClassConfig{Name: "partner", Rate: "400mbit"}
	require.Empty(t, cls.Ceil)
}

func TestDeviceConfig_DirEgressConstant(t *testing.T) {
	require.Equal(t, "egress", shp.DirEgress)
	require.Equal(t, "ingress", shp.DirIngress)
}

func TestDeviceConfig_FieldsRoundtrip(t *testing.T) {
	dev := &shp.DeviceConfig{
		Dir:          shp.DirEgress,
		Rate:         "1gbit",
		DefaultClass: "reserve-egress",
		CreatedAt:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	require.Equal(t, "egress", dev.Dir)
	require.Equal(t, "1gbit", dev.Rate)
	require.Equal(t, "reserve-egress", dev.DefaultClass)
}
