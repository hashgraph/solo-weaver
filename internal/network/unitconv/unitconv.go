// SPDX-License-Identifier: Apache-2.0

// Package unitconv holds the boot-unit convergence shared by the network planes
// that replay their state via a systemd oneshot. See docs/dev/traffic-shaper.md.
package unitconv

import (
	"bytes"
	"os"

	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
)

// EnabledProbe reports whether systemd will start a unit at boot. Injectable so
// tests need no /etc.
type EnabledProbe func() (bool, error)

// NeedsConverge reports, read-only, whether the unit at unitPath needs writing
// or enabling. guards are the persisted artifacts a missing unit would replay.
// See docs/dev/traffic-shaper.md.
func NeedsConverge(unitPath string, embedded []byte, guards []string, enabled EnabledProbe) (bool, error) {
	current, err := os.ReadFile(unitPath)
	switch {
	case err == nil:
		if !bytes.Equal(current, embedded) {
			logx.As().Debug().Str("unit", unitPath).
				Msg("installed unit differs from the embedded copy")
			return true, nil
		}
		// Only once the bytes match: differing bytes are rewritten and re-enabled anyway.
		on, enabledErr := enabled()
		if enabledErr != nil {
			return false, errorx.ExternalError.Wrap(enabledErr,
				"failed to read whether %s is enabled at boot", unitPath)
		}
		if !on {
			logx.As().Debug().Str("unit", unitPath).
				Msg("unit is current but disabled; it will not run at boot")
		}
		return !on, nil
	case os.IsNotExist(err):
		provisioned, statErr := anyExists(guards)
		if statErr != nil {
			return false, statErr
		}
		if provisioned {
			logx.As().Debug().Str("unit", unitPath).
				Msg("persisted artifact with no unit to replay it at boot")
		}
		return provisioned, nil
	default:
		return false, errorx.ExternalError.Wrap(err, "failed to read %s", unitPath)
	}
}

// anyExists reports whether any of the guard artifacts is persisted.
func anyExists(guards []string) (bool, error) {
	for _, p := range guards {
		if _, err := os.Stat(p); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, errorx.ExternalError.Wrap(err, "failed to stat %s", p)
		}
	}
	return false, nil
}
