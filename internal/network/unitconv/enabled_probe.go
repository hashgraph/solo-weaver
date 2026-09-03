// SPDX-License-Identifier: Apache-2.0

package unitconv

import (
	"os"
	"path/filepath"

	"github.com/joomcode/errorx"
)

// multiUserWantsDir is where `systemctl enable` records a
// WantedBy=multi-user.target unit, which both network boot units declare.
const multiUserWantsDir = "/etc/systemd/system/multi-user.target.wants"

// EnabledAtBoot reports whether systemd will start service at boot, via one
// Lstat rather than a DBus call. Cheap, not authoritative — see
// docs/dev/traffic-shaper.md.
func EnabledAtBoot(service string) (bool, error) {
	return enabledAtBoot(multiUserWantsDir, service)
}

// enabledAtBoot is EnabledAtBoot with the wants directory injected, so tests need
// no /etc.
func enabledAtBoot(wantsDir, service string) (bool, error) {
	link := filepath.Join(wantsDir, service)
	// Lstat, not Stat: a link to a removed unit file is still an enablement record.
	if _, err := os.Lstat(link); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, errorx.ExternalError.Wrap(err, "failed to stat %s", link)
	}
	return true, nil
}
