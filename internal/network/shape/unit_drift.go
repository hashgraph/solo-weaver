// SPDX-License-Identifier: Apache-2.0

package shape

import (
	"github.com/hashgraph/solo-weaver/internal/network/unitconv"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
)

// TcEgressUnitNeedsConverge reports whether the shaper unit needs rewriting or
// re-enabling (#980). Read-only and filesystem-only: no systemd round trip.
func TcEgressUnitNeedsConverge() (bool, error) {
	return tcEgressUnitNeedsConverge(TcEgressServiceUnitPath, TcEgressScriptPath, unitEnabledAtBoot)
}

// tcEgressUnitNeedsConverge is TcEgressUnitNeedsConverge with its paths and
// probe injected, so tests need neither /usr/lib nor systemd.
func tcEgressUnitNeedsConverge(unitPath, scriptPath string, enabled unitconv.EnabledProbe) (bool, error) {
	embedded, err := templates.Files.ReadFile(tcEgressServiceTemplate)
	if err != nil {
		return false, errorx.InternalError.Wrap(err, "failed to read embedded %s", tcEgressServiceTemplate)
	}

	// The boot script is the guard, so hosts that never shaped traffic get no unit.
	return unitconv.NeedsConverge(unitPath, embedded, []string{scriptPath}, enabled)
}

// unitEnabledAtBoot reports whether systemd will start the shaper unit at boot.
// Filesystem-only, so this file needs no build tag.
func unitEnabledAtBoot() (bool, error) {
	return unitconv.EnabledAtBoot(TcEgressService)
}
