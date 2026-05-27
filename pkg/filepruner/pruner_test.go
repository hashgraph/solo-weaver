// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package filepruner_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hashgraph/solo-weaver/pkg/filepruner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upgradeLayout is the timestamp format embedded in consensus upgrade filenames:
// consensus-upgrade-20260415T143000Z-v0.75.0.jsonl
const upgradeLayout = "20060102T150405Z"

const year = 365 * 24 * time.Hour

func writeFiles(t *testing.T, dir string, names []string) {
	t.Helper()
	for _, n := range names {
		require.NoError(t, os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o640))
	}
}

// tsName builds a consensus-upgrade filename with a timestamp offset from now.
// offset < 0 means in the past (e.g. -2*year = 2 years ago).
func tsName(offset time.Duration, ver string) string {
	ts := time.Now().UTC().Add(offset).Format(upgradeLayout)
	return "consensus-upgrade-" + ts + "-" + ver + ".jsonl"
}

func Test_FilenameTimestampStrategy_RemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	old1 := tsName(-2*year, "v0.70.0")
	old2 := tsName(-18*30*24*time.Hour, "v0.71.0") // ~18 months ago
	recent := tsName(-30*24*time.Hour, "v0.75.0")  // 30 days ago
	writeFiles(t, dir, []string{old1, old2, recent})

	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year})
	require.NoError(t, p.Prune(dir, "consensus-upgrade-*.jsonl", 50))

	assert.NoFileExists(t, filepath.Join(dir, old1))
	assert.NoFileExists(t, filepath.Join(dir, old2))
	assert.FileExists(t, filepath.Join(dir, recent))
}

func Test_FilenameTimestampStrategy_EnforcesHardCap(t *testing.T) {
	dir := t.TempDir()
	// 5 recent files (all within MaxAge); cap=3 → oldest 2 removed
	names := []string{
		tsName(-150*24*time.Hour, "v0.71.0"),
		tsName(-120*24*time.Hour, "v0.72.0"),
		tsName(-90*24*time.Hour, "v0.73.0"),
		tsName(-60*24*time.Hour, "v0.74.0"),
		tsName(-30*24*time.Hour, "v0.75.0"),
	}
	writeFiles(t, dir, names)

	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year})
	require.NoError(t, p.Prune(dir, "consensus-upgrade-*.jsonl", 3))

	assert.NoFileExists(t, filepath.Join(dir, names[0]))
	assert.NoFileExists(t, filepath.Join(dir, names[1]))
	assert.FileExists(t, filepath.Join(dir, names[2]))
	assert.FileExists(t, filepath.Join(dir, names[3]))
	assert.FileExists(t, filepath.Join(dir, names[4]))
}

func Test_FilenameTimestampStrategy_BothConditionsApplied(t *testing.T) {
	dir := t.TempDir()
	// 2 old + 4 recent; cap=3 → 2 removed by age, 1 more by cap.
	old1 := tsName(-2*year, "v0.70.0")
	old2 := tsName(-18*30*24*time.Hour, "v0.71.0")
	writeFiles(t, dir, []string{old1, old2})
	recent := []string{
		tsName(-150*24*time.Hour, "v0.72.0"),
		tsName(-120*24*time.Hour, "v0.73.0"),
		tsName(-90*24*time.Hour, "v0.74.0"),
		tsName(-30*24*time.Hour, "v0.75.0"),
	}
	writeFiles(t, dir, recent)

	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year})
	require.NoError(t, p.Prune(dir, "consensus-upgrade-*.jsonl", 3))

	assert.NoFileExists(t, filepath.Join(dir, old1))
	assert.NoFileExists(t, filepath.Join(dir, old2))
	assert.NoFileExists(t, filepath.Join(dir, recent[0]), "oldest recent should be pruned to satisfy cap")
	assert.FileExists(t, filepath.Join(dir, recent[1]))
	assert.FileExists(t, filepath.Join(dir, recent[2]))
	assert.FileExists(t, filepath.Join(dir, recent[3]))
}

func Test_FilenameTimestampStrategy_KeepsFileWithNoTimestamp(t *testing.T) {
	dir := t.TempDir()
	old := tsName(-2*year, "v0.70.0")
	writeFiles(t, dir, []string{
		old,
		"consensus-migrate-events.jsonl", // no timestamp — must be kept
	})

	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year})
	require.NoError(t, p.Prune(dir, "*.jsonl", 50))

	assert.NoFileExists(t, filepath.Join(dir, old))
	assert.FileExists(t, filepath.Join(dir, "consensus-migrate-events.jsonl"), "file with no timestamp must not be deleted")
}

func Test_FileSizeStrategy_PrunesOversizedFiles(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.jsonl")
	large := filepath.Join(dir, "large.jsonl")
	require.NoError(t, os.WriteFile(small, []byte("{}"), 0o640))
	require.NoError(t, os.WriteFile(large, make([]byte, 200), 0o640)) // 200 bytes

	p := filepruner.New(filepruner.FileSizeStrategy{MaxBytes: 100})
	require.NoError(t, p.Prune(dir, "*.jsonl", 50))

	assert.FileExists(t, small)
	assert.NoFileExists(t, large)
}

func Test_All_PrunesOnlyWhenBothConditionsMet(t *testing.T) {
	dir := t.TempDir()
	// old + large → pruned; old + small → kept; recent + large → kept
	oldLarge := tsName(-2*year, "v0.70.0")
	oldSmall := tsName(-18*30*24*time.Hour, "v0.71.0")
	recentLarge := tsName(-30*24*time.Hour, "v0.75.0")

	require.NoError(t, os.WriteFile(filepath.Join(dir, oldLarge), make([]byte, 200), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(dir, oldSmall), []byte("{}"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(dir, recentLarge), make([]byte, 200), 0o640))

	p := filepruner.New(filepruner.All(
		filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year},
		filepruner.FileSizeStrategy{MaxBytes: 100},
	))
	require.NoError(t, p.Prune(dir, "*.jsonl", 50))

	assert.NoFileExists(t, filepath.Join(dir, oldLarge), "old AND large — must be pruned")
	assert.FileExists(t, filepath.Join(dir, oldSmall), "old but small — must be kept")
	assert.FileExists(t, filepath.Join(dir, recentLarge), "large but recent — must be kept")
}

func Test_Any_PrunesWhenEitherConditionMet(t *testing.T) {
	dir := t.TempDir()
	// old + small → pruned (old); recent + large → pruned (large); recent + small → kept
	oldSmall := tsName(-2*year, "v0.70.0")
	recentLarge := tsName(-30*24*time.Hour, "v0.75.0")
	recentSmall := tsName(-60*24*time.Hour, "v0.74.0")

	require.NoError(t, os.WriteFile(filepath.Join(dir, oldSmall), []byte("{}"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(dir, recentLarge), make([]byte, 200), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(dir, recentSmall), []byte("{}"), 0o640))

	p := filepruner.New(filepruner.Any(
		filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year},
		filepruner.FileSizeStrategy{MaxBytes: 100},
	))
	require.NoError(t, p.Prune(dir, "*.jsonl", 50))

	assert.NoFileExists(t, filepath.Join(dir, oldSmall), "old — must be pruned")
	assert.NoFileExists(t, filepath.Join(dir, recentLarge), "large — must be pruned")
	assert.FileExists(t, filepath.Join(dir, recentSmall), "recent and small — must be kept")
}

func Test_FilenameTimestampStrategy_KeepsStrategyErrorFilesOutOfCap(t *testing.T) {
	dir := t.TempDir()
	// no-timestamp file: strategy returns error → protected, never cap-pruned
	protected := "consensus-migrate-events.jsonl"
	recent1 := tsName(-90*24*time.Hour, "v0.73.0")
	recent2 := tsName(-60*24*time.Hour, "v0.74.0")
	recent3 := tsName(-30*24*time.Hour, "v0.75.0")
	writeFiles(t, dir, []string{protected, recent1, recent2, recent3})

	// keep=2 should only cap-prune from the 3 timestamped eligible files, not the protected one
	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year})
	require.NoError(t, p.Prune(dir, "*.jsonl", 2))

	assert.FileExists(t, filepath.Join(dir, protected), "strategy-error file must never be cap-pruned")
	assert.NoFileExists(t, filepath.Join(dir, recent1), "oldest eligible removed to satisfy cap")
	assert.FileExists(t, filepath.Join(dir, recent2))
	assert.FileExists(t, filepath.Join(dir, recent3))
}

func Test_FilenameTimestampStrategy_RejectsEmptyLayout(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, []string{"consensus-migrate-events.jsonl"})

	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: "", MaxAge: year})
	// Prune itself returns no error (strategy errors keep files), but the file must not be deleted
	require.NoError(t, p.Prune(dir, "*.jsonl", 50))
	assert.FileExists(t, filepath.Join(dir, "consensus-migrate-events.jsonl"), "empty Layout must not delete files")
}

func Test_Prune_RejectsNegativeKeep(t *testing.T) {
	p := filepruner.New(filepruner.ModTimeStrategy{MaxAge: year})
	err := p.Prune(t.TempDir(), "*.jsonl", -1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "keep must be >= 0")
}

func Test_Prune_RejectsNilStrategy(t *testing.T) {
	p := filepruner.New(nil)
	err := p.Prune(t.TempDir(), "*.jsonl", 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil strategy")
}

func Test_ModTimeStrategy_RemovesOldFilesAndEnforcesCap(t *testing.T) {
	dir := t.TempDir()
	names := []string{"events-a.jsonl", "events-b.jsonl", "events-c.jsonl", "events-d.jsonl"}
	writeFiles(t, dir, names)

	past := time.Now().Add(-400 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, names[0]), past, past))
	require.NoError(t, os.Chtimes(filepath.Join(dir, names[1]), past, past))

	p := filepruner.New(filepruner.ModTimeStrategy{MaxAge: year})
	require.NoError(t, p.Prune(dir, "events-*.jsonl", 3))

	assert.NoFileExists(t, filepath.Join(dir, names[0]))
	assert.NoFileExists(t, filepath.Join(dir, names[1]))
	assert.FileExists(t, filepath.Join(dir, names[2]))
	assert.FileExists(t, filepath.Join(dir, names[3]))
}
