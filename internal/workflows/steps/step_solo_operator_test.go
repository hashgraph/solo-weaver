// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"errors"
	"testing"
)

// TestIsHelmOperationInProgress pins the matcher against the exact Helm error that
// a release stuck mid-install produces, so the recovery hint keeps firing.
func TestIsHelmOperationInProgress(t *testing.T) {
	cases := map[string]struct {
		msg  string
		want bool
	}{
		"helm pending lock": {
			"helm.upgrade_failed: failed to run upgrade action, cause: " +
				"another operation (install/upgrade/rollback) is in progress",
			true,
		},
		"unrelated error": {"connection refused", false},
		"partial phrase":  {"another operation happening", false},
		"empty":           {"", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var err error
			if tc.msg != "" {
				err = errors.New(tc.msg)
			}
			if got := isHelmOperationInProgress(err); got != tc.want {
				t.Errorf("isHelmOperationInProgress(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}
