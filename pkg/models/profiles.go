// SPDX-License-Identifier: Apache-2.0

package models

const (
	// Deployment profiles
	ProfileLocal      = "local"
	ProfilePerfnet    = "perfnet"
	ProfileTestnet    = "testnet"
	ProfilePreviewnet = "previewnet"
	ProfileMainnet    = "mainnet"
)

// SupportedProfiles returns all supported deployment profiles ordered by
// environment criticality (production → development). This ordering is
// also used by TUI select prompts, so changing it affects how options
// are displayed to the user.
func SupportedProfiles() []string {
	return []string{
		ProfileMainnet,
		ProfileTestnet,
		ProfilePreviewnet,
		ProfilePerfnet,
		ProfileLocal,
	}
}
