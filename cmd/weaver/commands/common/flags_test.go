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
		got, err := fp.Value(cmd, nil)
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
		got, err := fp.Value(cmd, nil)
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
		got, err := fp.Value(cmd, nil)
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
		got, err := fp.Value(cmd, nil)
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
		got, err := fp.Value(cmd, nil)
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
		got, err := fp.Value(cmd, nil)
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

	got, err := fp.Value(cmd, nil)
	require.NoError(t, err)
	require.Equal(t, fp.Default, got)

	// set persistent flag value on root command
	require.NoError(t, rc.PersistentFlags().Set(fp.Name, "alice")) // set on root command

	// get from root command
	got, err = fp.Value(rc, nil)
	require.NoError(t, err)
	require.Equal(t, "alice", got)

	// can also get from subcommand
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

// TestFlagStopOnError_DefaultIsFalse guards against the default being flipped back to
// true. FlagStopOnError must default to false so that GetExecutionMode does not see
// multiple flags as "set" when the user only passes --continue-on-error or
// --rollback-on-error. The stop-on-error behaviour when no flag is explicitly provided
// is handled by GetExecutionMode's else-branch, not by this flag's default value.
func TestFlagStopOnError_DefaultIsFalse(t *testing.T) {
	require.False(t, FlagStopOnError().Default)
}

func TestFlagCloneIndependent(t *testing.T) {
	orig := FlagConfig()
	clone := orig.Clone()
	clone.ShortName = "x"
	clone.Default = "abc"

	// original should be unchanged — each FlagConfig() call returns a fresh value
	require.Equal(t, "c", orig.ShortName)
	require.Equal(t, "", orig.Default)
}

// TestFlagDescriptorsAreImmutable verifies that mutating a descriptor returned by a
// factory function does not affect subsequent calls to that factory.
// This is the key contract provided by Option 2 (factory functions).
func TestFlagDescriptorsAreImmutable(t *testing.T) {
	first := FlagConfig()
	first.ShortName = "z"
	first.Default = "/bad/path"
	first.Name = "corrupted"

	second := FlagConfig()

	// second must be pristine regardless of what we did to first
	require.Equal(t, "config", second.Name)
	require.Equal(t, "c", second.ShortName)
	require.Equal(t, "", second.Default)

	// same for a bool flag
	bf := FlagForce()
	bf.ShortName = "x"
	bf.Default = true
	require.Equal(t, "y", FlagForce().ShortName)
	require.Equal(t, false, FlagForce().Default)
}

func TestValueFallbackPersistent(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)

	var cfgPath string
	FlagConfig().SetVarP(root, &cfgPath, false)

	// simulate executing child with --config=/tmp/foo
	args := []string{"--config", "/tmp/foo"}
	// Use child so parsing will use root persistent flags as fallback
	got, err := FlagConfig().Value(child, args)
	require.NoError(t, err)
	require.Equal(t, "/tmp/foo", got)
}

// TestValueOwnPersistent_DoesNotFindInheritedFlag asserts that ValueOwnPersistent
// only reads the command's own persistent FlagSet and does NOT find a flag
// registered persistently on a parent/root command.
func TestValueOwnPersistent_DoesNotFindInheritedFlag(t *testing.T) {
	fp := FlagDefinition[string]{Name: "cfg", ShortName: "", Description: "config", Default: ""}
	var v string

	root := &cobra.Command{Use: "root"}
	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)

	// register flag persistently on ROOT only
	err := fp.varP(root, &v, false)
	require.NoError(t, err)
	require.NoError(t, root.PersistentFlags().Set(fp.Name, "/etc/foo.yaml"))

	// ValueOwnPersistent on child must NOT find root's persistent flag
	_, err = fp.ValueOwnPersistent(child, nil)
	require.Error(t, err, "ValueOwnPersistent on child must not find a flag registered only on root")
}

// TestValueOwnPersistent_FindsOwnPersistentFlag asserts that ValueOwnPersistent
// correctly reads a flag registered persistently on the same command.
func TestValueOwnPersistent_FindsOwnPersistentFlag(t *testing.T) {
	fp := FlagDefinition[string]{Name: "cfg", ShortName: "", Description: "config", Default: "default"}
	var v string

	cmd := &cobra.Command{Use: "cmd"}
	err := fp.varP(cmd, &v, false)
	require.NoError(t, err)
	require.NoError(t, cmd.PersistentFlags().Set(fp.Name, "/etc/bar.yaml"))

	got, err := fp.ValueOwnPersistent(cmd, nil)
	require.NoError(t, err)
	require.Equal(t, "/etc/bar.yaml", got)
}

func TestDetectShortNameCollisions(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)

	var a string
	FlagA := FlagDefinition[string]{Name: "a", ShortName: "x", Default: ""}
	FlagB := FlagDefinition[string]{Name: "b", ShortName: "x", Default: ""}
	FlagA.SetVarP(root, &a, false)
	FlagB.SetVar(child, &a, false)
	found := DetectShortNameCollisions(root)
	require.True(t, found)
}

func TestDetectShortNameCollisions_PersistentOnNonRoot(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)

	var a, b string
	// persistent flag on child (not root) with shorthand -x
	FlagA := FlagDefinition[string]{Name: "alpha", ShortName: "x", Default: ""}
	// another persistent flag on child with the same shorthand -x
	FlagB := FlagDefinition[string]{Name: "beta", ShortName: "x", Default: ""}

	FlagA.SetVarP(child, &a, false) // persistent on child
	FlagB.SetVar(child, &b, false)  // local on child

	found := DetectShortNameCollisions(root)
	require.True(t, found, "should detect collision between persistent and local flags on non-root command")
}

// TestDetectShortNameCollisions_NoPersistentDoubleCount verifies that a persistent
// flag with a shortname is NOT reported as a collision with itself after
// MarkFlagsMutuallyExclusive is called. Cobra's MarkFlagsMutuallyExclusive calls
// mergePersistentFlags() internally, which merges own persistent flags into
// cmd.Flags(). Without the guard in DetectShortNameCollisions the same flag would
// be visited twice (once via cmd.Flags(), once via cmd.PersistentFlags()) producing
// a false shortname collision.
func TestDetectShortNameCollisions_NoPersistentDoubleCount(t *testing.T) {
	root := &cobra.Command{Use: "root"}
	child := &cobra.Command{Use: "child"}
	root.AddCommand(child)

	var v bool
	fp := FlagDefinition[bool]{Name: "myflag", ShortName: "m", Description: "a flag", Default: false}
	fp.SetVarP(child, &v, false)

	// MarkFlagsMutuallyExclusive internally calls mergePersistentFlags(), causing
	// the persistent flag to appear in both child.Flags() and child.PersistentFlags().
	child.MarkFlagsMutuallyExclusive("myflag")

	found := DetectShortNameCollisions(root)
	require.False(t, found, "persistent flag merged into cmd.Flags() must not be counted as a shortname collision with itself")
}
