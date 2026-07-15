// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"fmt"
	"os"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"

	"gopkg.in/yaml.v3"
)

// PrintWorkflowReport serializes the workflow execution report as YAML. If
// fileName is provided the report is written to that file; otherwise it is
// printed to standard output. It returns a non-nil error when the report cannot
// be marshaled or written so callers can avoid reporting a successful save that
// never happened.
var PrintWorkflowReport = func(report *automa.Report, fileName string) error {
	b, err := yaml.Marshal(report)
	if err != nil {
		return errorx.IllegalFormat.Wrap(err, "failed to marshal workflow report")
	}

	if fileName != "" {
		if err := os.WriteFile(fileName, b, models.DefaultFilePerm); err != nil {
			return errorx.ExternalError.Wrap(err, "failed to write workflow report to %s", fileName)
		}
		return nil
	}

	fmt.Printf("Workflow Execution Report:%s\n", b)
	return nil
}
