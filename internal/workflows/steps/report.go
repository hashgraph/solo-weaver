package steps

import (
	"fmt"
	"github.com/automa-saga/automa"
	"gopkg.in/yaml.v3"
)

// PrintWorkflowReport prints the workflow execution report in YAML format
var PrintWorkflowReport = func(report *automa.Report) {
	b, err := yaml.Marshal(report)
	if err != nil {
		fmt.Printf("Failed to marshal report: %v\n", err)
		return
	}
	fmt.Printf("Workflow Execution Report:%s\n", b)
}
