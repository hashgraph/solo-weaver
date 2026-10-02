// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package bll

import (
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestAddOutOfBandWarnings_KeepsExistingWarningsAndEachChange(t *testing.T) {
	report := &automa.Report{Metadata: map[string]string{models.ReportMetaWarning: "existing warning"}}
	changes := []drift.Change{
		{Component: "teleport", Field: "nodeAgent.installed", Persisted: "true", Live: "false"},
		{Component: "teleport", Field: "clusterAgent.installed", Persisted: "false", Live: "true"},
	}

	addOutOfBandWarnings(report, changes)

	require.Equal(t, []string{"existing warning", changes[0].String(), changes[1].String()}, ui.CollectWarnings(report))
}

func TestAddOutOfBandWarnings_NoChangesLeavesTheReportAlone(t *testing.T) {
	report := &automa.Report{}

	addOutOfBandWarnings(report, nil)

	require.Nil(t, report.Metadata)
}
