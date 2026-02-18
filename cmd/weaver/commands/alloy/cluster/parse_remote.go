// SPDX-License-Identifier: Apache-2.0

package cluster

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/config"
)

// keyPattern matches a comma followed by a valid key name and equals sign.
// Key names must start with a letter and contain only alphanumeric characters.
var keyPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]*=`)

// parseRemoteFlags parses the repeatable remote flags in key=value format.
// Format: "name=<name>,url=<url>,username=<username>"
// Example: "name=primary,url=https://prom:9090/api/v1/write,username=user1"
// The username field is optional.
func parseRemoteFlags(flags []string) ([]config.AlloyRemoteConfig, error) {
	if len(flags) == 0 {
		return nil, nil
	}

	var remotes []config.AlloyRemoteConfig

	for _, flag := range flags {
		remote := config.AlloyRemoteConfig{}

		for _, pair := range splitKeyValuePairs(flag) {
			eqIdx := strings.Index(pair, "=")
			if eqIdx == -1 {
				return nil, fmt.Errorf("invalid key=value pair %q in %q, expected format: name=<name>,url=<url>,username=<username>", pair, flag)
			}

			key := strings.TrimSpace(pair[:eqIdx])
			value := strings.TrimSpace(pair[eqIdx+1:])

			switch strings.ToLower(key) {
			case "name":
				remote.Name = value
			case "url":
				remote.URL = value
			case "username":
				remote.Username = value
			default:
				return nil, fmt.Errorf("unknown key %q in %q, valid keys are: name, url, username", key, flag)
			}
		}

		// Validate required fields
		if remote.Name == "" {
			return nil, fmt.Errorf("missing required 'name' in %q", flag)
		}
		if remote.URL == "" {
			return nil, fmt.Errorf("missing required 'url' in %q", flag)
		}

		remotes = append(remotes, remote)
	}

	return remotes, nil
}

// splitKeyValuePairs splits a string into key=value pairs, handling commas within values.
// It splits on commas that are followed by any valid key pattern (word followed by =).
// This allows URLs to contain commas in query parameters while still detecting unknown keys.
func splitKeyValuePairs(s string) []string {
	var pairs []string
	start := 0

	for i := 0; i < len(s); i++ {
		if s[i] == ',' && i+1 < len(s) {
			remaining := s[i+1:]
			// Split if the remaining string starts with a key= pattern
			if keyPattern.MatchString(remaining) {
				pairs = append(pairs, s[start:i])
				start = i + 1
			}
		}
	}

	// Add the last pair
	if start < len(s) {
		pairs = append(pairs, s[start:])
	}

	return pairs
}
