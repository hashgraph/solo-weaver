// SPDX-License-Identifier: Apache-2.0

package labels

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RenderLabelRules renders labels as Alloy relabel rule blocks.
// Output is suitable for injection into prometheus.relabel,
// prometheus.operator.servicemonitors, and loki.relabel blocks.
//
// All labels in the map are rendered, including "cluster".
//
// Example output:
//
//	rule {
//	  target_label = "cluster"
//	  replacement  = "my-cluster"
//	}
//	rule {
//	  target_label = "environment"
//	  replacement  = "previewnet"
//	}
func RenderLabelRules(labels map[string]string) string {
	keys := sortedKeys(labels)
	if len(keys) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("  rule {\n    target_label = %q\n    replacement  = %q\n  }\n", k, labels[k]))
	}
	return sb.String()
}

var labelNameRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func isValidLabelName(name string) bool {
	return labelNameRE.MatchString(name)
}

// RenderStaticLabels renders labels as Alloy static label entries.
// Output is suitable for injection into loki.source.journal labels blocks.
//
// All labels in the map are rendered, including "cluster".
//
// Example output:
//
//	cluster        = "my-cluster",
//	environment    = "previewnet",
//	instance       = "lfh02",
func RenderStaticLabels(labels map[string]string) string {
	keys := sortedKeys(labels)
	if len(keys) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, k := range keys {
		if !isValidLabelName(k) {
			continue
		}
		sb.WriteString(fmt.Sprintf("    %s = %q,\n", k, labels[k]))
	}
	return sb.String()
}

// sortedKeys returns all keys of m sorted alphabetically.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
