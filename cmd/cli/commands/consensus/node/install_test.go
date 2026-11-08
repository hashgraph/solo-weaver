// SPDX-License-Identifier: Apache-2.0

package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	dir := pkgWithAdminKeys(t, "x")
	require.NoError(t, os.WriteFile(filepath.Join(dir, nodeAdminKeysPath), nil, 0o644))
	confirm, _ := confirmSpy(true, nil)
	require.Error(t, checkNodeAdminKeys(dir, false, false, confirm))
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
