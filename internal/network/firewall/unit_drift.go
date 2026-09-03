// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/hashgraph/solo-weaver/internal/network/unitconv"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
)

// NetworkNftUnitNeedsConverge reports whether the loader unit needs rewriting or
// re-enabling (#982). Read-only and filesystem-only: no systemd round trip.
func NetworkNftUnitNeedsConverge() (bool, error) {
	return nftUnitNeedsConverge(NetworkNftServiceUnitPath, []string{HostNftPath, WeaverNftPath}, unitEnabledAtBoot)
}

// nftUnitNeedsConverge is NetworkNftUnitNeedsConverge with its paths and probe
// injected, so tests need neither /usr/lib nor systemd.
func nftUnitNeedsConverge(unitPath string, artifacts []string, enabled unitconv.EnabledProbe) (bool, error) {
	embedded, err := templates.Files.ReadFile(networkNftServiceTemplate)
	if err != nil {
		return false, errorx.InternalError.Wrap(err, "failed to read embedded %s", networkNftServiceTemplate)
	}

	// Either persisted nft document means a unit is needed to replay it at boot.
	return unitconv.NeedsConverge(unitPath, embedded, artifacts, enabled)
}

// unitEnabledAtBoot reports whether systemd will start the loader unit at boot.
// Filesystem-only, so this file needs no build tag.
func unitEnabledAtBoot() (bool, error) {
	return unitconv.EnabledAtBoot(NetworkNftService)
}
