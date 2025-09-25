package steps

import (
	"bytes"
	"github.com/automa-saga/automa"
	"os"
	"regexp"
	"testing"
)

func TestPrintWorkflowReport(t *testing.T) {
	report := &automa.Report{
		Status: automa.StatusSuccess,
	}
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	PrintWorkflowReport(report)

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

func TestRunCmd_Success(t *testing.T) {
	err := RunCmd("echo", "hello")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestRunCmd_Failure(t *testing.T) {
	err := RunCmd("false")
	if err == nil {
		t.Errorf("expected error for failing command, got nil")
	}
}

func TestBashCommandOutput_Success(t *testing.T) {
	out, err := runCmdOutput("echo hello")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if out != "hello" {
		t.Errorf("expected 'hello', got %q", out)
	}
}

func TestBashCommandOutput_Failure(t *testing.T) {
	_, err := runCmdOutput("exit 1")
	if err == nil {
		t.Errorf("expected error for failing command, got nil")
	}
}

func TestGenerateKubeadmToken_Format(t *testing.T) {
	token := generateKubeadmToken()
	// Token should match: 6 hex chars . 16 hex chars
	re := regexp.MustCompile(`^[a-f0-9]{6}\.[a-f0-9]{16}$`)
	if !re.MatchString(token) {
		t.Errorf("token format invalid: %q", token)
	}
}
