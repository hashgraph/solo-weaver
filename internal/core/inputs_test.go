package core

import (
	"strings"
	"testing"

	"github.com/automa-saga/automa"
)

func TestCommonInputsValidate_Valid(t *testing.T) {
	modes := AllExecutionModes()
	if len(modes) == 0 {
		t.Skip("no execution modes defined")
	}
	nodeTypes := AllNodeTypes()
	if len(nodeTypes) == 0 {
		t.Skip("no node types defined")
	}

	c := CommonInputs{
		NodeType: nodeTypes[0],
		ExecutionOptions: WorkflowExecutionOptions{
			ExecutionMode: modes[0],
			RollbackMode:  modes[0],
		},
	}

	if err := c.Validate(); err != nil {
		t.Fatalf("expected valid CommonInputs, got error: %v", err)
	}
}

func TestCommonInputsValidate_InvalidExecutionModes(t *testing.T) {
	// pick values that are very unlikely to be in AllExecutionModes()
	c := CommonInputs{
		ExecutionOptions: WorkflowExecutionOptions{
			ExecutionMode: automa.TypeMode(99),
			RollbackMode:  automa.TypeMode(99),
		},
	}

	if err := c.Validate(); err == nil {
		t.Fatalf("expected error for invalid execution/rollback modes, got nil")
	}
}

func TestUserInputs_NoCustomValidator(t *testing.T) {
	modes := AllExecutionModes()
	if len(modes) == 0 {
		t.Skip("no execution modes defined")
	}

	u := UserInputs[int]{
		Common: CommonInputs{
			ExecutionOptions: WorkflowExecutionOptions{
				ExecutionMode: modes[0],
				RollbackMode:  modes[0],
			},
		},
		Custom: 123,
	}

	if err := u.Validate(); err != nil {
		t.Fatalf("expected no error for non-validator custom type, got: %v", err)
	}
}

func TestBlocknodeInputs_ValidZeroValue(t *testing.T) {
	// zero-value should pass validations that are not strict (profile/version/... empty)
	b := BlocknodeInputs{}
	if err := b.Validate(); err != nil {
		t.Fatalf("expected zero-value BlocknodeInputs to validate, got: %v", err)
	}
}

func TestBlocknodeInputs_InvalidProfile(t *testing.T) {
	b := BlocknodeInputs{
		Profile: "invalid/profile!",
	}

	err := b.Validate()
	if err == nil {
		t.Fatalf("expected error for invalid profile, got nil")
	}
	if !strings.Contains(err.Error(), "invalid profile") {
		t.Fatalf("expected error message to mention 'invalid profile', got: %v", err)
	}
}

// minimal custom error type used in tests
type customErr struct{ s string }

func (e *customErr) Error() string { return e.s }
