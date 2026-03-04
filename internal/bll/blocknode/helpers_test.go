// SPDX-License-Identifier: Apache-2.0

package blocknode

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

func TestPrepareBlocknodeEffectiveInputs_NilInputs(t *testing.T) {
	// inputs == nil should return an error
	_, err := prepareBlocknodeEffectiveInputs(nil, nil, nil)
	if err == nil {
		t.Fatalf("expected error when inputs is nil, got nil")
	}
}

func TestPrepareBlocknodeEffectiveInputs_RuntimeStateNil_ReturnsSamePointer(t *testing.T) {
	in := &models.UserInputs[models.BlocknodeInputs]{}
	// effective == nil should return inputs as-is (no resolution attempted)
	out, err := prepareBlocknodeEffectiveInputs(nil, in, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != in {
		t.Fatalf("expected returned pointer to be the same as input; got different pointers")
	}
}
