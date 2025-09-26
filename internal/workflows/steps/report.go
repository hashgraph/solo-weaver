package steps

import (
	"fmt"
	"github.com/automa-saga/automa"
	"gopkg.in/yaml.v3"
)

// PrintWorkflowReport prints the workflow execution report in YAML format
var PrintWorkflowReport = func(report *automa.Report) {
	b, _ := yaml.Marshal(report)
	fmt.Printf("Workflow Execution Report:%s\n", b)
}
