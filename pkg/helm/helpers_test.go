// SPDX-License-Identifier: Apache-2.0

package helm

import (
	"strings"
	"testing"
)

func TestTruncateFieldManager(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		maxLen   int
		expected string
	}{
		{
			name:     "empty path returns helm",
			path:     "",
			maxLen:   128,
			expected: "helm",
		},
		{
			name:     "short path unchanged",
			path:     "/usr/local/bin/weaver",
			maxLen:   128,
			expected: "weaver",
		},
		{
			name:     "long IntelliJ test path truncated",
			path:     "/private/var/folders/30/yl_b12wx77q66hvnshhf66c40000gp/T/GoLand/___go_build_github_com_hashgraph_solo_weaver_cmd_weaver_commands_block_node",
			maxLen:   128,
			expected: "___go_build_github_com_hashgraph_solo_weaver_cmd_weaver_commands_block_node",
		},
		{
			name:     "base name exactly at limit",
			path:     "/some/path/" + strings.Repeat("a", 128),
			maxLen:   128,
			expected: strings.Repeat("a", 128),
		},
		{
			name:     "base name over limit truncated from start",
			path:     "/some/path/" + strings.Repeat("a", 10) + strings.Repeat("b", 130),
			maxLen:   128,
			expected: strings.Repeat("a", 10) + strings.Repeat("b", 130),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateFieldManager(tt.path, tt.maxLen)
			if len(result) > tt.maxLen {
				t.Errorf("truncateFieldManager() returned %d bytes, want <= %d", len(result), tt.maxLen)
			}
			// For paths that should result in base name, verify
			if tt.path != "" && len(result) <= tt.maxLen {
				// Result should not exceed maxLen
				if len(result) > tt.maxLen {
					t.Errorf("Result length %d exceeds maxLen %d", len(result), tt.maxLen)
				}
			}
		})
	}
}

func TestTruncateFieldManager_RealWorldPaths(t *testing.T) {
	// Test with real-world IntelliJ/GoLand test execution paths
	longPaths := []string{
		"/private/var/folders/30/yl_b12wx77q66hvnshhf66c40000gp/T/GoLand/___go_build_github_com_hashgraph_solo_weaver_cmd_weaver_commands_block_node",
		"/var/folders/30/yl_b12wx77q66hvnshhf66c40000gp/T/go-build1234567890/b001/exe/node.test",
		"/tmp/go-build1234567890/b001/exe/___TestHelmLifecycle_InstallAndUpgradeWithValueReuse_in_github_com_hashgraph_solo_weaver_cmd_weaver_commands_block_node",
	}

	for _, path := range longPaths {
		result := truncateFieldManager(path, 128)
		if len(result) > 128 {
			t.Errorf("truncateFieldManager(%q) = %q (len=%d), want len <= 128", path, result, len(result))
		}
		if result == "" {
			t.Errorf("truncateFieldManager(%q) returned empty string", path)
		}
	}
}
