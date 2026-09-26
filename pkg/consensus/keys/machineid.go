// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"os"
	"strings"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// machineIDPaths are the host files consulted, in order, for a stable machine
// UUID. /etc/machine-id (systemd) and its dbus twin are the primary sources; the
// DMI product_uuid is a hardware fallback. This must be read HOST-side at
// keygen time: the consensus node runs in a pod whose identity is ephemeral, so
// the pod's own machine-id would not be a stable hardware anchor.
var machineIDPaths = []string{
	"/etc/machine-id",
	"/var/lib/dbus/machine-id",
	"/sys/class/dmi/id/product_uuid",
}

// MachineUUID resolves a stable machine UUID from the host, used as the gossip
// certificate CN. It returns an error (never a random value) if no source is
// available, so the caller can require an explicit --machine-id / --identity
// rather than silently minting an unstable identity.
func MachineUUID() (string, error) {
	return machineUUIDFrom(machineIDPaths)
}

func machineUUIDFrom(paths []string) (string, error) {
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id, nil
		}
	}
	return "", errx.Decorate(
		errorx.IllegalState.New("no machine UUID source found (tried %v)", paths),
		reasons.FileMissing,
		"pass an explicit machine identity via --machine-id, or run key generation on the target host where /etc/machine-id exists",
	)
}
