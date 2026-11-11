// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"

	"github.com/hashgraph/solo-weaver/pkg/software"
)

const testDefaults = `host:
  tool: 1.0.0
  other: 2.0.0
cluster: {}
`

// testHost has an empty block (tool 1.0.0) and a complete one (other 2.0.0).
const testHost = `# SPDX-License-Identifier: Apache-2.0

host:

- name: tool
  versions:
    1.0.0:
      archives:
        - name: 'tool-{{.ARCH}}.tar.gz'
          url: 'https://example.com/tool-{{.ARCH}}.tar.gz'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: '' # from https://example.com/tool-amd64.tar.gz.sha256
            arm64:
              algorithm: 'sha256'
              checksum: ''
      binaries:
        - name: 'tool/bin/tool'
          archive: 'tool-{{.ARCH}}.tar.gz'
          linux:
            amd64:
              algorithm: 'sha256'
              checksum: ''
            arm64:
              algorithm: 'sha256'
              checksum: ''
      configs:
        - name: 'tool/etc/tool.service'
          archive: 'tool-{{.ARCH}}.tar.gz'
          algorithm: 'sha256'
          checksum: ''

- name: other
  versions:
    2.0.0:
      binaries:
        - name: 'other'
          url: 'https://example.com/other/{{.ARCH}}'
          linux:
            amd64:
              algorithm: 'sha512'
              checksum: 'o-amd64'
`

// fakeCompute returns per-arch digests and one config value for every platform
// (unless perArchConfig), and records its calls.
type fakeCompute struct {
	calls         []string
	dirs          []string
	perArchConfig bool
}

func (f *fakeCompute) compute(name, version, goos, goarch, workDir string) ([]software.ComputedChecksum, error) {
	f.calls = append(f.calls, name+" "+version+" "+goos+"/"+goarch)
	f.dirs = append(f.dirs, workDir)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, err
	}
	switch name {
	case "tool":
		cfg := "svc"
		if f.perArchConfig {
			cfg += "-" + goarch
		}
		return []software.ComputedChecksum{
			{Section: software.SectionArchives, Name: "tool-{{.ARCH}}.tar.gz", Algorithm: "sha256", Value: "a-" + goarch},
			{Section: software.SectionBinaries, Name: "tool/bin/tool", Algorithm: "sha256", Value: "b-" + goarch},
			{Section: software.SectionConfigs, Name: "tool/etc/tool.service", Algorithm: "sha256", Value: cfg},
		}, nil
	case "other":
		return []software.ComputedChecksum{
			{Section: software.SectionBinaries, Name: "other", Algorithm: "sha512", Value: "o-" + goarch},
		}, nil
	}
	return nil, errorx.IllegalArgument.New("unknown %s", name)
}

func writeCatalog(t *testing.T, host string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "defaults.yaml"), []byte(testDefaults), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "host-test.yaml"), []byte(host), 0o644))
	return dir
}

func testOptions(t *testing.T, dir string, f *fakeCompute) (options, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return options{
		dir:     dir,
		workDir: t.TempDir(),
		out:     out,
		compute: f.compute,
		fetch: func(string, string) (string, error) {
			return "a-amd64  tool-amd64.tar.gz\n", nil
		},
	}, out
}

func readHost(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "host-test.yaml"))
	require.NoError(t, err)
	return string(data)
}

func TestRun_WriteFillsEmptyChecksumsAndKeepsTheRest(t *testing.T) {
	dir := writeCatalog(t, testHost)
	f := &fakeCompute{}
	opts, _ := testOptions(t, dir, f)
	opts.write = true

	require.NoError(t, run(opts))

	want := testHost
	for _, r := range []struct{ old, new string }{
		{"checksum: '' # from", "checksum: 'a-amd64' # from"},
		{"checksum: ''", "checksum: 'a-arm64'"},
		{"checksum: ''", "checksum: 'b-amd64'"},
		{"checksum: ''", "checksum: 'b-arm64'"},
		{"checksum: ''", "checksum: 'svc'"},
	} {
		want = strings.Replace(want, r.old, r.new, 1)
	}
	require.Equal(t, want, readHost(t, dir))
	require.Equal(t, []string{"tool 1.0.0 linux/amd64", "tool 1.0.0 linux/arm64"}, f.calls, "only the block with empty checksums")

	// A second run finds nothing to do and changes nothing.
	f.calls = nil
	require.NoError(t, run(opts))
	require.Empty(t, f.calls)
	require.Equal(t, want, readHost(t, dir))
}

func TestRun_FreesEachPlatformWorkDir(t *testing.T) {
	dir := writeCatalog(t, testHost)
	f := &fakeCompute{}
	opts, _ := testOptions(t, dir, f)

	require.NoError(t, run(opts))
	require.Len(t, f.dirs, 2)
	for _, d := range f.dirs {
		require.NoDirExists(t, d)
	}
}

func TestRun_CheckOnlyWritesNothing(t *testing.T) {
	dir := writeCatalog(t, testHost)
	opts, out := testOptions(t, dir, &fakeCompute{})

	require.NoError(t, run(opts))
	require.Equal(t, testHost, readHost(t, dir))
	require.Contains(t, out.String(), "empty    tool 1.0.0 binaries tool/bin/tool linux/arm64 b-arm64")
}

func TestRun_AllVerifiesCompleteBlocks(t *testing.T) {
	dir := writeCatalog(t, testHost)
	f := &fakeCompute{}
	opts, out := testOptions(t, dir, f)
	opts.all = true
	opts.name = "other"

	require.NoError(t, run(opts))
	require.Equal(t, []string{"other 2.0.0 linux/amd64"}, f.calls)
	require.Contains(t, out.String(), "ok       other 2.0.0 binaries other linux/amd64 o-amd64")
}

func TestRun_MismatchIsNeverOverwritten(t *testing.T) {
	host := strings.Replace(testHost, "checksum: 'o-amd64'", "checksum: 'stale'", 1)
	dir := writeCatalog(t, host)
	opts, _ := testOptions(t, dir, &fakeCompute{})
	opts.all = true
	opts.write = true

	err := run(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "catalog has stale, computed o-amd64")
	require.Equal(t, host, readHost(t, dir), "nothing is written when any value mismatches")
}

func TestRun_PublishedChecksumMustContainTheComputedValue(t *testing.T) {
	dir := writeCatalog(t, testHost)
	opts, _ := testOptions(t, dir, &fakeCompute{})
	opts.write = true
	opts.fetch = func(string, string) (string, error) { return "deadbeef  tool-amd64.tar.gz\n", nil }

	err := run(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "is not in https://example.com/tool-amd64.tar.gz.sha256")
	require.Equal(t, testHost, readHost(t, dir))
}

func TestRun_ConfigMustMatchAcrossPlatforms(t *testing.T) {
	dir := writeCatalog(t, testHost)
	opts, _ := testOptions(t, dir, &fakeCompute{perArchConfig: true})
	opts.write = true

	err := run(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "differs between platforms")
}

func TestRun_EntryTheToolDidNotComputeIsAnError(t *testing.T) {
	host := strings.Replace(testHost, "        - name: 'tool/bin/tool'", "        - name: 'tool/bin/extra'\n          archive: 'tool-{{.ARCH}}.tar.gz'\n          linux:\n            amd64:\n              algorithm: 'sha256'\n              checksum: ''\n        - name: 'tool/bin/tool'", 1)
	dir := writeCatalog(t, host)
	opts, _ := testOptions(t, dir, &fakeCompute{})

	err := run(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool/bin/extra linux/amd64 was not computed")
}

func TestReadEntries_MissingChecksumKey(t *testing.T) {
	host := strings.Replace(testHost, "              algorithm: 'sha512'\n              checksum: 'o-amd64'\n", "              algorithm: 'sha512'\n", 1)
	dir := writeCatalog(t, host)

	_, err := readEntries(filepath.Join(dir, "host-test.yaml"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "no checksum key")
}

func TestFillChecksums(t *testing.T) {
	data := []byte("a:\n  checksum: '' # from x\n  checksum:\n  checksum: \"old\"\n")

	got, err := fillChecksums(data, map[int]string{2: "v2", 3: "v3", 4: "v4"})
	require.NoError(t, err)
	require.Equal(t, "a:\n  checksum: 'v2' # from x\n  checksum: 'v3'\n  checksum: 'v4'\n", string(got))

	_, err = fillChecksums(data, map[int]string{1: "v"})
	require.Error(t, err)
}
