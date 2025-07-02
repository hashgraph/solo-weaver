/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package backup

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestBackupFilter_ShouldBackup(t *testing.T) {
	var testCases = []struct {
		desc    string
		src     string
		ruleSet SnapshotRuleSet
		include bool
	}{
		{
			desc: "nil ruleset should return true",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: nil,
				Exclude: nil,
			},
			include: true,
		},
		{
			desc: "valid include with explicit filetype should return true",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: []string{
					"?test.txt",
				},
				Exclude: nil,
			},
			include: true,
		},
		{
			desc: "valid include ruleset with wildcard filetype return true",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: []string{
					"?test.*",
				},
				Exclude: nil,
			},
			include: true,
		},
		{
			desc: "valid include ruleset wildcard path should return true",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: []string{
					"/a/*/test.*",
				},
				Exclude: nil,
			},
			include: true,
		},
		{
			desc: "valid include ruleset should take precedence",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: []string{
					"/a/*/test.*",
				},
				Exclude: []string{
					"/a/b/*",
				},
			},
			include: true,
		},
		{
			desc: "valid exclude ruleset for a subtree should succeed",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: []string{
					"/a/*/do-not-match",
				},
				Exclude: []string{
					"/a/b/*/*/*.txt",
				},
			},
			include: false,
		},
		{
			desc: "Invalid include pattern should be ignored",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: []string{
					"INVALID[",
				},
				Exclude: []string{
					"/a/b/c/d/test.*",
				},
			},
			include: false,
		},
		{
			desc: "Invalid exclude pattern should be ignored",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: []string{
					"/a/*/do-not-match",
				},
				Exclude: []string{
					"INVALID[",
				},
			},
			include: true,
		},
		{
			desc: "If include or exclude patterns do not match it should return true",
			src:  "/a/b/c/d/test.txt",
			ruleSet: SnapshotRuleSet{
				Include: []string{
					"/log/test",
				},
				Exclude: []string{
					"/a/b/f/*",
				},
			},
			include: true,
		},
	}

	filter := &backupFilter{}
	for _, test := range testCases {
		filter.ruleSet = test.ruleSet
		output := filter.ShouldBackup(test.src, "")
		assert.Equalf(t, test.include, output, "failed test: %s", test.desc)
	}
}
