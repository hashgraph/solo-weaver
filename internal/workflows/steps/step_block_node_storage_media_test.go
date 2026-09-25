// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package steps

import (
	"context"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/mount/mountinfo"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── BlockNodeStorageMediaStep: the exported, production-wired entry point ──
//
// These exercise the glue BlockNodeStorageMediaStep adds on top of the
// classifier-injected checkStorageMediaStep tested above: resolving storage
// paths via blocknode.StoragePathsByVolume and swallowing that resolution's
// error into a skip rather than propagating it.

func TestBlockNodeStorageMediaStep_Id(t *testing.T) {
	b := BlockNodeStorageMediaStep(models.BlockNodeStorage{BasePath: "/opt/hedera/blocknode"}, "0.37.0")
	assert.Equal(t, CheckStorageMediaStepId, b.Id())
}

// TestBlockNodeStorageMediaStep_UnresolvableStorageSkipsInsteadOfFailing covers
// the path-resolution failure BlockNodeStorageMediaStep is meant to swallow: no
// base path and no individual paths cannot name any volume, so the built step
// must still build and run to a skip, never propagate the error out of workflow
// construction (this step is warn-only and must never block install/upgrade).
func TestBlockNodeStorageMediaStep_UnresolvableStorageSkipsInsteadOfFailing(t *testing.T) {
	step, err := BlockNodeStorageMediaStep(models.BlockNodeStorage{}, "0.37.0").Build()
	require.NoError(t, err)

	report := step.Execute(context.Background())
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSkipped, report.Status)
	assert.Equal(t, "not determined: the storage paths could not be resolved (see the log)",
		report.Metadata[models.ReportMetaStorageMedia])
}

func runStorageMediaStep(t *testing.T, paths map[string]string, classify storageClassifier) *automa.Report {
	t.Helper()
	step, err := checkStorageMediaStep(paths, classify).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

var testPaths = map[string]string{"archive": "/opt/hedera/blocknode/archive"}

func TestCheckStorageMediaStep_LocalStorageSucceedsWithoutWarning(t *testing.T) {
	report := runStorageMediaStep(t, testPaths, func(map[string]string) ([]mountinfo.Finding, error) {
		return []mountinfo.Finding{{
			MountPoint: "/opt/hedera/blocknode",
			FSType:     "xfs",
			Volumes:    []string{"archive"},
			Local:      true,
		}}, nil
	})

	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.Equal(t, "xfs", report.Metadata["storage.archive.fstype"])
	assert.Equal(t, "/opt/hedera/blocknode", report.Metadata["storage.archive.mountpoint"])
	assert.NotContains(t, report.Metadata, models.ReportMetaWarning)
	// AC1: the console renders this key, so it must be present on a clean run.
	assert.Equal(t, "archive: xfs on /opt/hedera/blocknode",
		report.Metadata[models.ReportMetaStorageMedia])
}

func TestCheckStorageMediaStep_NonLocalStorageWarnsButSucceeds(t *testing.T) {
	report := runStorageMediaStep(t, testPaths, func(map[string]string) ([]mountinfo.Finding, error) {
		return []mountinfo.Finding{{
			MountPoint: "/opt/hedera/blocknode",
			FSType:     "fuse.s3fs",
			Volumes:    []string{"archive"},
			Local:      false,
		}}, nil
	})

	// The check informs the operator; it must never block a deployment.
	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSuccess, report.Status)
	assert.Contains(t, report.Metadata[models.ReportMetaWarning], "not local block storage")
	assert.Equal(t, "fuse.s3fs", report.Metadata["storage.archive.fstype"])
	assert.Equal(t, "archive: fuse.s3fs on /opt/hedera/blocknode (not local block storage)",
		report.Metadata[models.ReportMetaStorageMedia])
}

func TestCheckStorageMediaStep_ClassifierErrorSkips(t *testing.T) {
	report := runStorageMediaStep(t, testPaths, func(map[string]string) ([]mountinfo.Finding, error) {
		return nil, errorx.ExternalError.New("mount table unreadable")
	})

	require.NoError(t, report.Error)
	assert.Equal(t, automa.StatusSkipped, report.Status)
	assert.Contains(t, report.Metadata[models.ReportMetaStorageMedia],
		"not determined: the mount table could not be read")
}

func TestCheckStorageMediaStep_SkipsWhenNothingToCheck(t *testing.T) {
	noFindings := func(map[string]string) ([]mountinfo.Finding, error) { return nil, nil }

	for name, tc := range map[string]struct {
		paths    map[string]string
		classify storageClassifier
		reason   string
	}{
		"no paths resolved":     {nil, noFindings, "the storage paths could not be resolved"},
		"no classifier wired":   {testPaths, nil, "no storage classifier is wired"},
		"no path matched mount": {testPaths, noFindings, "no storage path resolved to a known mount"},
	} {
		t.Run(name, func(t *testing.T) {
			report := runStorageMediaStep(t, tc.paths, tc.classify)
			require.NoError(t, report.Error)
			assert.Equal(t, automa.StatusSkipped, report.Status)
			// Silence on the console would read as "every path is fine".
			assert.Equal(t, "not determined: "+tc.reason+" (see the log)",
				report.Metadata[models.ReportMetaStorageMedia])
		})
	}
}
