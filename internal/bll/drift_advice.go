// SPDX-License-Identifier: Apache-2.0

package bll

import (
	"strings"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/drift"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// addDriftAdvice appends, after the out-of-band warnings, the lines naming the
// commands that repair them, from each managed component that has advice.
func addDriftAdvice(report *automa.Report, managed []Component, changes []drift.Change) {
	if report == nil || len(changes) == 0 {
		return
	}
	var lines []string
	for _, c := range managed {
		if c.advise != nil {
			lines = append(lines, c.advise(changes)...)
		}
	}
	if len(lines) == 0 {
		return
	}
	if existing := report.Metadata[models.ReportMetaWarning]; existing != "" {
		lines = append([]string{existing}, lines...)
	}
	if report.Metadata == nil {
		report.Metadata = map[string]string{}
	}
	report.Metadata[models.ReportMetaWarning] = strings.Join(lines, "\n")
}
