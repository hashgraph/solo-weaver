// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/automa-saga/automa"
	"github.com/stretchr/testify/assert"
)

func newReport() *automa.Report {
	now := time.Now()
	return &automa.Report{StartTime: now, EndTime: now.Add(time.Second)}
}

func TestRenderSummaryTable_IncludesDaemonLogWhenNonEmpty(t *testing.T) {
	out := RenderSummaryTable(newReport(), time.Second, "/logs/report.yaml", "/logs/solo-provisioner.log", "/logs/solo-provisioner-daemon.log")
	assert.Contains(t, out, "Daemon log:")
	assert.Contains(t, out, "/logs/solo-provisioner-daemon.log")
}

func TestRenderSummaryTable_OmitsDaemonLogWhenEmpty(t *testing.T) {
	out := RenderSummaryTable(newReport(), time.Second, "/logs/report.yaml", "/logs/solo-provisioner.log", "")
	assert.False(t, strings.Contains(out, "Daemon log:"), "daemon log line should be absent when daemonLogPath is empty")
}
