// SPDX-License-Identifier: Apache-2.0

package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pkgWithAdminKeys returns a package dir; with content it also writes node-admin-keys.json.
func pkgWithAdminKeys(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if content != "" {
		path := filepath.Join(dir, nodeAdminKeysPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

// confirmSpy records whether the prompt ran and returns the given answer.
func confirmSpy(answer bool, err error) (func() (bool, error), *bool) {
	called := false
	return func() (bool, error) {
		called = true
		return answer, err
	}, &called
}

func TestCheckNodeAdminKeys_Present(t *testing.T) {
	confirm, called := confirmSpy(false, nil)
	require.NoError(t, checkNodeAdminKeys(pkgWithAdminKeys(t, `{"0":"ab"}`), false, false, confirm))
	assert.False(t, *called)
}

func TestCheckNodeAdminKeys_NoPackage(t *testing.T) {
	confirm, called := confirmSpy(false, nil)
	require.NoError(t, checkNodeAdminKeys(" ", false, false, confirm))
	assert.False(t, *called)
}

func TestCheckNodeAdminKeys_MissingNonInteractiveFails(t *testing.T) {
	confirm, called := confirmSpy(true, nil)
	err := checkNodeAdminKeys(pkgWithAdminKeys(t, ""), false, false, confirm)
	require.ErrorContains(t, err, "node-admin-keys.json")
	assert.False(t, *called)
}

func TestCheckNodeAdminKeys_EmptyFileCountsAsMissing(t *testing.T) {
	for _, content := range []string{"", " \n\t"} {
		dir := pkgWithAdminKeys(t, "x")
		require.NoError(t, os.WriteFile(filepath.Join(dir, nodeAdminKeysPath), []byte(content), 0o644))
		confirm, _ := confirmSpy(true, nil)
		require.Error(t, checkNodeAdminKeys(dir, false, false, confirm), "content %q", content)
	}
}

func TestCheckNodeAdminKeys_MissingAllowed(t *testing.T) {
	confirm, called := confirmSpy(false, nil)
	require.NoError(t, checkNodeAdminKeys(pkgWithAdminKeys(t, ""), true, true, confirm))
	assert.False(t, *called)
}

func TestCheckNodeAdminKeys_MissingInteractive(t *testing.T) {
	dir := pkgWithAdminKeys(t, "")

	yes, called := confirmSpy(true, nil)
	require.NoError(t, checkNodeAdminKeys(dir, false, true, yes))
	assert.True(t, *called)

	no, _ := confirmSpy(false, nil)
	require.ErrorContains(t, checkNodeAdminKeys(dir, false, true, no), "aborted")

	boom := errors.New("prompt failed")
	failing, _ := confirmSpy(false, boom)
	require.ErrorIs(t, checkNodeAdminKeys(dir, false, true, failing), boom)
}

// recreateConfirm records whether the --recreate-pvcs prompt ran and returns the given answer.
func recreateConfirm(answer bool) (func(string, string, bool) (bool, error), *bool) {
	called := false
	return func(string, string, bool) (bool, error) {
		called = true
		return answer, nil
	}, &called
}

// recreateFinder returns the given PVC lines and records whether the lookup ran.
func recreateFinder(pvcs []string, err error) (func() ([]string, error), *bool) {
	called := false
	return func() ([]string, error) {
		called = true
		return pvcs, err
	}, &called
}

var recreateLine = []string{"orbit-consensus-0-saved (500Gi)"}

func TestCheckRecreatePVCs_FlagNotSetDoesNotPrompt(t *testing.T) {
	find, looked := recreateFinder(recreateLine, nil)
	confirm, called := recreateConfirm(false)
	require.NoError(t, checkRecreatePVCs(false, true, 0, "ns", find, confirm))
	assert.False(t, *looked)
	assert.False(t, *called)
}

func TestCheckRecreatePVCs_NoPromptModeSkipsPrompt(t *testing.T) {
	find, looked := recreateFinder(recreateLine, nil)
	confirm, called := recreateConfirm(false)
	require.NoError(t, checkRecreatePVCs(true, false, 0, "ns", find, confirm), "--force / --non-interactive is the confirmation")
	assert.False(t, *looked)
	assert.False(t, *called)
}

func TestCheckRecreatePVCs_NothingToDeleteDoesNotPrompt(t *testing.T) {
	find, _ := recreateFinder(nil, nil)
	confirm, called := recreateConfirm(false)
	require.NoError(t, checkRecreatePVCs(true, true, 0, "ns", find, confirm))
	assert.False(t, *called)
}

func TestCheckRecreatePVCs_ConfirmedNamesPVCs(t *testing.T) {
	find, _ := recreateFinder(recreateLine, nil)
	var desc string
	var def bool
	confirm := func(_ string, description string, defaultValue bool) (bool, error) {
		desc, def = description, defaultValue
		return true, nil
	}
	require.NoError(t, checkRecreatePVCs(true, true, 0, "ns", find, confirm))
	assert.Contains(t, desc, "orbit-consensus-0-saved (500Gi)", "the prompt names each PVC it deletes")
	assert.False(t, def, "the safe answer is the default")
}

func TestCheckRecreatePVCs_LookupErrorStillPrompts(t *testing.T) {
	find, _ := recreateFinder(nil, errors.New("no cluster"))
	confirm, called := recreateConfirm(false)
	err := checkRecreatePVCs(true, true, 0, "ns", find, confirm)
	require.Error(t, err, "a failed lookup does not skip the confirmation")
	assert.True(t, *called)
}

func TestCheckRecreatePVCs_DeclinedAborts(t *testing.T) {
	find, _ := recreateFinder(recreateLine, nil)
	confirm, called := recreateConfirm(false)
	err := checkRecreatePVCs(true, true, 0, "ns", find, confirm)
	require.Error(t, err)
	assert.True(t, *called)
	assert.Contains(t, err.Error(), "aborted")
}

// unmountedFinder returns the given lookup result and records whether the lookup ran.
func unmountedFinder(lines []string, err error) (func() ([]string, string, error), *bool) {
	called := false
	return func() ([]string, string, error) {
		called = true
		return lines, "--volume name=saved,type=pvc,size=1Gi", err
	}, &called
}

var unmountedLine = []string{"saved is emptydir, but PVC orbit-consensus-0-saved exists"}

func TestCheckUnmountedPVCs_NoPromptModeSkipsLookup(t *testing.T) {
	find, looked := unmountedFinder(unmountedLine, nil)
	confirm, asked := recreateConfirm(true)
	ok, err := checkUnmountedPVCs(false, 0, find, confirm)
	require.NoError(t, err)
	assert.False(t, ok, "--force / --non-interactive / no TTY: the precheck step decides")
	assert.False(t, *looked)
	assert.False(t, *asked)
}

func TestCheckUnmountedPVCs_NoUnmountedPVCsDoesNotPrompt(t *testing.T) {
	find, _ := unmountedFinder(nil, nil)
	confirm, asked := recreateConfirm(true)
	ok, err := checkUnmountedPVCs(true, 0, find, confirm)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.False(t, *asked)
}

func TestCheckUnmountedPVCs_LookupErrorSkipsPrompt(t *testing.T) {
	find, _ := unmountedFinder(nil, errors.New("no cluster"))
	confirm, asked := recreateConfirm(true)
	ok, err := checkUnmountedPVCs(true, 0, find, confirm)
	require.NoError(t, err, "the precheck step reports lookup errors")
	assert.False(t, ok)
	assert.False(t, *asked)
}

func TestCheckUnmountedPVCs_ConfirmedAccepts(t *testing.T) {
	find, _ := unmountedFinder(unmountedLine, nil)
	var desc string
	var def bool
	confirm := func(_ string, description string, defaultValue bool) (bool, error) {
		desc, def = description, defaultValue
		return true, nil
	}
	ok, err := checkUnmountedPVCs(true, 0, find, confirm)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Contains(t, desc, "PVC orbit-consensus-0-saved", "the prompt names the PVC")
	assert.False(t, def, "the safe answer is the default")
}

func TestCheckUnmountedPVCs_DeclinedStopsWithKeepHint(t *testing.T) {
	find, _ := unmountedFinder(unmountedLine, nil)
	confirm, _ := recreateConfirm(false)
	ok, err := checkUnmountedPVCs(true, 0, find, confirm)
	require.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "aborted")
	hints, _ := errx.Hints(err)
	assert.Contains(t, hints, "To keep the data, back the volumes with their PVCs again: --volume name=saved,type=pvc,size=1Gi")
}

func TestCheckUnmountedPVCs_PromptErrorReturns(t *testing.T) {
	find, _ := unmountedFinder(unmountedLine, nil)
	boom := errors.New("prompt failed")
	_, err := checkUnmountedPVCs(true, 0, find, func(string, string, bool) (bool, error) { return false, boom })
	require.ErrorIs(t, err, boom)
}
