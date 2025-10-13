package steps

import (
	"bytes"
	"os"
	"testing"

	"github.com/automa-saga/automa"
)

func TestPrintWorkflowReport(t *testing.T) {
	report := &automa.Report{
		Status: automa.StatusSuccess,
	}
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	PrintWorkflowReport(report, "")

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
