// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/automa"
	"gopkg.in/yaml.v3"
)

func TestPrintWorkflowReport(t *testing.T) {
	report := &automa.Report{
		Status: automa.StatusSuccess,
	}
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	if err := PrintWorkflowReport(report, ""); err != nil {
		t.Fatalf("PrintWorkflowReport returned error: %v", err)
	}

	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	if err != nil {
		t.Fatalf("failed to read stdout: %v", err)
	}
	output := buf.String()
	if want := "Workflow Execution Report:"; !bytes.Contains([]byte(output), []byte(want)) {
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

func TestPrintWorkflowReport_WritesYAMLFile(t *testing.T) {
	report := &automa.Report{
		Id:     "block-node-preflight",
		Status: automa.StatusSuccess,
	}

	path := filepath.Join(t.TempDir(), "setup_report.yaml")
	if err := PrintWorkflowReport(report, path); err != nil {
		t.Fatalf("PrintWorkflowReport returned error: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read report file: %v", err)
	}

	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		t.Fatalf("report is not valid YAML: %v\n%s", err, b)
	}
	if m["status"] != automa.StatusSuccess.String() {
		t.Errorf("expected status %q, got %v", automa.StatusSuccess.String(), m["status"])
	}
}
