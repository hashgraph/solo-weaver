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

func AllProfiles() []string {
	return []string{
		ProfileLocal,
		ProfilePerfnet,
		ProfileTestnet,
		ProfilePreviewnet,
		ProfileMainnet,
	}
}
