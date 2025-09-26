package steps

import (
	"testing"
)

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
	out, err := RunCmdOutput("echo hello")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if out != "hello" {
		t.Errorf("expected 'hello', got %q", out)
	}
}

func TestBashCommandOutput_Failure(t *testing.T) {
	_, err := RunCmdOutput("exit 1")
	if err == nil {
		t.Errorf("expected error for failing command, got nil")
	}
}
