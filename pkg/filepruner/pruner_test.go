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

func Test_FilenameTimestampStrategy_RemovesOldFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, []string{
		"consensus-upgrade-20240101T000000Z-v0.70.0.jsonl", // old
		"consensus-upgrade-20240601T000000Z-v0.71.0.jsonl", // old
		"consensus-upgrade-20260415T143000Z-v0.75.0.jsonl", // recent
	})

	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year})
	require.NoError(t, p.Prune(dir, "consensus-upgrade-*.jsonl", 50))

	assert.NoFileExists(t, filepath.Join(dir, "consensus-upgrade-20240101T000000Z-v0.70.0.jsonl"))
	assert.NoFileExists(t, filepath.Join(dir, "consensus-upgrade-20240601T000000Z-v0.71.0.jsonl"))
	assert.FileExists(t, filepath.Join(dir, "consensus-upgrade-20260415T143000Z-v0.75.0.jsonl"))
}

func Test_FilenameTimestampStrategy_EnforcesHardCap(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"consensus-upgrade-20260101T000000Z-v0.71.0.jsonl",
		"consensus-upgrade-20260201T000000Z-v0.72.0.jsonl",
		"consensus-upgrade-20260301T000000Z-v0.73.0.jsonl",
		"consensus-upgrade-20260401T000000Z-v0.74.0.jsonl",
		"consensus-upgrade-20260415T143000Z-v0.75.0.jsonl",
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
	// 2 old (2024) + 4 recent (2026); cap=3 → 2 removed by age, 1 more by cap.
	writeFiles(t, dir, []string{
		"consensus-upgrade-20240101T000000Z-v0.70.0.jsonl",
		"consensus-upgrade-20240601T000000Z-v0.71.0.jsonl",
	})
	recent := []string{
		"consensus-upgrade-20260101T000000Z-v0.72.0.jsonl",
		"consensus-upgrade-20260201T000000Z-v0.73.0.jsonl",
		"consensus-upgrade-20260301T000000Z-v0.74.0.jsonl",
		"consensus-upgrade-20260415T143000Z-v0.75.0.jsonl",
	}
	writeFiles(t, dir, recent)

	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year})
	require.NoError(t, p.Prune(dir, "consensus-upgrade-*.jsonl", 3))

	assert.NoFileExists(t, filepath.Join(dir, "consensus-upgrade-20240101T000000Z-v0.70.0.jsonl"))
	assert.NoFileExists(t, filepath.Join(dir, "consensus-upgrade-20240601T000000Z-v0.71.0.jsonl"))
	assert.NoFileExists(t, filepath.Join(dir, recent[0]), "oldest recent should be pruned to satisfy cap")
	assert.FileExists(t, filepath.Join(dir, recent[1]))
	assert.FileExists(t, filepath.Join(dir, recent[2]))
	assert.FileExists(t, filepath.Join(dir, recent[3]))
}

func Test_FilenameTimestampStrategy_KeepsFileWithNoTimestamp(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, []string{
		"consensus-upgrade-20240101T000000Z-v0.70.0.jsonl",
		"consensus-migrate-events.jsonl", // no timestamp — must be kept
	})

	p := filepruner.New(filepruner.FilenameTimestampStrategy{Layout: upgradeLayout, MaxAge: year})
	require.NoError(t, p.Prune(dir, "*.jsonl", 50))

	assert.NoFileExists(t, filepath.Join(dir, "consensus-upgrade-20240101T000000Z-v0.70.0.jsonl"))
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
	oldLarge := "consensus-upgrade-20240101T000000Z-v0.70.0.jsonl"
	oldSmall := "consensus-upgrade-20240601T000000Z-v0.71.0.jsonl"
	recentLarge := "consensus-upgrade-20260415T143000Z-v0.75.0.jsonl"

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
	oldSmall := "consensus-upgrade-20240101T000000Z-v0.70.0.jsonl"
	recentLarge := "consensus-upgrade-20260415T143000Z-v0.75.0.jsonl"
	recentSmall := "consensus-upgrade-20260301T000000Z-v0.74.0.jsonl"

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
