// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func runStringFlagTest(t *testing.T, persistent bool) {
	fp := FlagDefinition[string]{Name: "name", ShortName: "n", Description: "a name", Default: "default"}
	var v string
	cmd := &cobra.Command{}
	var err error
	if persistent {
		err = fp.varP(cmd, &v, false)
	} else {
		err = fp.varNP(cmd, &v, false)
	}
	require.NoError(t, err)

	// default
	if persistent {
		got, err := fp.ValueP(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, fp.Default, got)
	} else {
		got, err := fp.Value(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, fp.Default, got)
	}

	// set and verify
	if persistent {
		require.NoError(t, cmd.PersistentFlags().Set(fp.Name, "alice"))
		got, err := fp.ValueP(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, "alice", got)
	} else {
		require.NoError(t, cmd.Flags().Set(fp.Name, "alice"))
		got, err := fp.Value(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, "alice", got)
	}
}

func runBoolFlagTest(t *testing.T, persistent bool) {
	fp := FlagDefinition[bool]{Name: "enabled", ShortName: "e", Description: "enabled", Default: false}
	var v bool
	cmd := &cobra.Command{}
	var err error
	if persistent {
		err = fp.varP(cmd, &v, false)
	} else {
		err = fp.varNP(cmd, &v, false)
	}
	require.NoError(t, err)

	if persistent {
		require.NoError(t, cmd.PersistentFlags().Set(fp.Name, "true"))
		got, err := fp.ValueP(cmd, nil)
		require.NoError(t, err)
		require.True(t, got)
	} else {
		require.NoError(t, cmd.Flags().Set(fp.Name, "true"))
		got, err := fp.Value(cmd, nil)
		require.NoError(t, err)
		require.True(t, got)
	}
}

func runIntFlagTest(t *testing.T, persistent bool) {
	fp := FlagDefinition[int]{Name: "count", ShortName: "c", Description: "count", Default: 0}
	var v int
	cmd := &cobra.Command{}
	var err error
	if persistent {
		err = fp.varP(cmd, &v, false)
	} else {
		err = fp.varNP(cmd, &v, false)
	}
	require.NoError(t, err)

	if persistent {
		require.NoError(t, cmd.PersistentFlags().Set(fp.Name, "42"))
		got, err := fp.ValueP(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, 42, got)
	} else {
		require.NoError(t, cmd.Flags().Set(fp.Name, "42"))
		got, err := fp.Value(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, 42, got)
	}
}

func runStringSliceFlagTest(t *testing.T, persistent bool) {
	fp := FlagDefinition[[]string]{Name: "items", ShortName: "i", Description: "items", Default: []string{}}
	var v []string
	cmd := &cobra.Command{}
	var err error
	if persistent {
		err = fp.varP(cmd, &v, false)
	} else {
		err = fp.varNP(cmd, &v, false)
	}
	require.NoError(t, err)

	if persistent {
		require.NoError(t, cmd.PersistentFlags().Set(fp.Name, "a,b,c"))
		got, err := fp.ValueP(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b", "c"}, got)
	} else {
		require.NoError(t, cmd.Flags().Set(fp.Name, "a,b,c"))
		got, err := fp.Value(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, []string{"a", "b", "c"}, got)
	}
}

func runDurationFlagTest(t *testing.T, persistent bool) {
	fp := FlagDefinition[time.Duration]{Name: "timeout", ShortName: "t", Description: "timeout", Default: 0}
	var v time.Duration
	cmd := &cobra.Command{}
	var err error
	if persistent {
		err = fp.varP(cmd, &v, false)
	} else {
		err = fp.varNP(cmd, &v, false)
	}
	require.NoError(t, err)

	if persistent {
		require.NoError(t, cmd.PersistentFlags().Set(fp.Name, "5s"))
		got, err := fp.ValueP(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, 5*time.Second, got)
	} else {
		require.NoError(t, cmd.Flags().Set(fp.Name, "5s"))
		got, err := fp.Value(cmd, nil)
		require.NoError(t, err)
		require.Equal(t, 5*time.Second, got)
	}
}

func TestFlags_PersistentAndNonPersistent(t *testing.T) {
	for _, persistent := range []bool{true, false} {
		name := "non-persistent"
		if persistent {
			name = "persistent"
		}
		t.Run(name+"/string", func(t *testing.T) { runStringFlagTest(t, persistent) })
		t.Run(name+"/bool", func(t *testing.T) { runBoolFlagTest(t, persistent) })
		t.Run(name+"/int", func(t *testing.T) { runIntFlagTest(t, persistent) })
		t.Run(name+"/stringslice", func(t *testing.T) { runStringSliceFlagTest(t, persistent) })
		t.Run(name+"/duration", func(t *testing.T) { runDurationFlagTest(t, persistent) })
	}
}

func TestVarP_VarNP_NilPointer_ReturnsError(t *testing.T) {
	fp := FlagDefinition[string]{Name: "z", ShortName: "z", Description: "nil test", Default: ""}
	cmd := &cobra.Command{}

	err := fp.varP(cmd, nil, false)
	require.Error(t, err)

	err = fp.varNP(cmd, nil, false)
	require.Error(t, err)
}

func TestVarP_RequiredFlag_IsMarked(t *testing.T) {
	fp := FlagDefinition[string]{Name: "req", ShortName: "r", Description: "required flag", Default: ""}
	var v string
	cmd := &cobra.Command{}

	// use varP to avoid doctor.CheckErr exiting on error
	err := fp.varP(cmd, &v, true)
	require.NoError(t, err)

	f := cmd.PersistentFlags().Lookup(fp.Name)
	require.NotNil(t, f)
	ann, ok := f.Annotations[cobra.BashCompOneRequiredFlag]
	require.Equal(t, true, ok, "persistent flag should have required annotation")
	require.Contains(t, ann, "true", "persistent flag required annotation should be 'true'")
}

func TestValue_CheckPersistentFlagInParentCommand(t *testing.T) {
	fp := FlagDefinition[string]{Name: "name", ShortName: "n", Description: "a name", Default: "default"}
	var v string
	rc := &cobra.Command{Use: "root"}
	err := fp.varP(rc, &v, false)
	require.NoError(t, err)

	cmd := &cobra.Command{}
	rc.AddCommand(cmd)

	// child won't see default from persistent flag if using ValueP()
	got, err := fp.ValueP(cmd, nil)
	require.Error(t, err) // because ValueP doesn't look into parent commands
	require.Equal(t, "", got)

	// but Value() will
	got, err = fp.Value(cmd, nil)
	require.NoError(t, err)
	require.Equal(t, fp.Default, got)

	// set persistent flag value on root command
	require.NoError(t, rc.PersistentFlags().Set(fp.Name, "alice")) // set on root command

	// get from root command
	got, err = fp.ValueP(rc, nil)
	require.NoError(t, err)
	require.Equal(t, "alice", got)

	// get from child command
	got, err = fp.ValueP(cmd, nil)
	require.Error(t, err) // because ValueP doesn't look into parent commands
	require.Equal(t, "", got)

	got, err = fp.Value(cmd, nil)
	require.NoError(t, err)
	require.Equal(t, "alice", got)
}

func TestGetExecutionMode_ValidCases(t *testing.T) {
	cases := []struct {
		name          string
		continueOnErr bool
		stopOnErr     bool
		rollbackOnErr bool
		expect        automa.TypeMode
	}{
		{"continue only", true, false, false, automa.ContinueOnError},
		{"rollback only", false, false, true, automa.RollbackOnError},
		{"stop only", false, true, false, automa.StopOnError},
		{"none set (default)", false, false, false, automa.StopOnError},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GetExecutionMode(tc.continueOnErr, tc.stopOnErr, tc.rollbackOnErr)
			require.NoError(t, err)
			require.Equal(t, tc.expect, got)
		})
	}
}

func TestGetExecutionMode_MutuallyExclusiveFlags_ReturnsError(t *testing.T) {
	// More than one flag set should produce an error.
	_, err := GetExecutionMode(true, true, false)
	require.Error(t, err)

	_, err = GetExecutionMode(true, false, true)
	require.Error(t, err)

	_, err = GetExecutionMode(false, true, true)
	require.Error(t, err)

	_, err = GetExecutionMode(true, true, true)
	require.Error(t, err)
}
