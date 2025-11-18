package steps

import (
	"fmt"
	"os"

	"github.com/automa-saga/automa"
	"golang.hedera.com/solo-weaver/internal/core"
	"gopkg.in/yaml.v3"
)

// PrintWorkflowReport prints the workflow execution report in YAML format
// If fileName is provided, it writes the report to the specified file
// Otherwise, it prints the report to standard output
var PrintWorkflowReport = func(report *automa.Report, fileName string) {
	b, err := yaml.Marshal(report)
	if err != nil {
		fmt.Printf("Failed to marshal report: %v\n", err)
		return
	}

	if fileName != "" {
		err := os.WriteFile(fileName, b, core.DefaultFilePerm)
		if err != nil {
			fmt.Printf("Failed to write report to file: %v\n", err)
			return
		}
	} else {
		fmt.Printf("Workflow Execution Report:%s\n", b)
	}
}
