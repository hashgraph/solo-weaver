// SPDX-License-Identifier: Apache-2.0

package sudoers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/stretchr/testify/require"
)

const embeddedSudoersPath = "files/weaver/sudoers"

// ---- ValidateContent ----

func TestValidateContent_EmbeddedTemplate(t *testing.T) {
	t.Parallel()
	content, err := templates.Files.ReadFile(embeddedSudoersPath)
	require.NoError(t, err)
	require.NoError(t, ValidateContent(content))
}

func TestValidateContent_Valid(t *testing.T) {
	t.Parallel()
	content := []byte("# comment\nweaver ALL=(root) NOPASSWD: /usr/bin/helm, /usr/bin/kubectl\n")
	require.NoError(t, ValidateContent(content))
}

func TestValidateContent_ValidContinuation(t *testing.T) {
	t.Parallel()
	content := []byte("weaver ALL=(root) NOPASSWD: /usr/bin/helm, \\\n  /usr/bin/kubectl\n")
	require.NoError(t, ValidateContent(content))
}

func TestValidateContent_EmptyLinesAndComments(t *testing.T) {
	t.Parallel()
	content := []byte("# comment\n\n# another comment\n")
	require.NoError(t, ValidateContent(content))
}

func TestValidateContent_MissingEquals(t *testing.T) {
	t.Parallel()
	content := []byte("weaver ALL NOPASSWD: /usr/bin/helm\n")
	require.Error(t, ValidateContent(content))
}

func TestValidateContent_MissingColon(t *testing.T) {
	t.Parallel()
	content := []byte("weaver ALL=(root) NOPASSWD /usr/bin/helm\n")
	require.Error(t, ValidateContent(content))
}

func TestValidateContent_RelativePath(t *testing.T) {
	t.Parallel()
	content := []byte("weaver ALL=(root) NOPASSWD: helm\n")
	require.Error(t, ValidateContent(content))
}

// ---- WriteEntry ----

func TestWriteEntry_ValidContent(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "test-sudoers")
	content := []byte("# test\nweaver ALL=(root) NOPASSWD: /usr/bin/helm\n")

	require.NoError(t, WriteEntry(dst, content))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, content, got)

	info, err := os.Stat(dst)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o440), info.Mode().Perm())
}

func TestWriteEntry_InvalidContent_NothingWritten(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "test-sudoers")
	content := []byte("weaver ALL NOPASSWD: helm\n") // missing '=', relative path

	err := WriteEntry(dst, content)
	require.Error(t, err)

	_, statErr := os.Stat(dst)
	require.True(t, os.IsNotExist(statErr), "no file should be written when validation fails")
}

func TestWriteEntry_NoTempFileLeft_OnSuccess(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "test-sudoers")
	content := []byte("weaver ALL=(root) NOPASSWD: /usr/bin/helm\n")

	require.NoError(t, WriteEntry(dst, content))

	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the final file should remain; temp file must be gone")
}

// ---- Cleanup ----

func TestCleanup_RemovesFinalFile(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "test-sudoers")
	require.NoError(t, os.WriteFile(dst, []byte("x"), 0o440))

	Cleanup(dst)

	_, err := os.Stat(dst)
	require.True(t, os.IsNotExist(err))
}
