// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"bytes"
	"context"
	"os"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
)

// unitEnabledProbe reports whether systemd will start the loader unit at boot.
// Injectable so tests don't need a systemd DBus connection.
type unitEnabledProbe func(ctx context.Context) (bool, error)

// NetworkNftUnitNeedsConverge reports whether the loader unit needs rewriting or
// re-enabling: wrong bytes, a persisted nft artifact with no unit to replay it,
// or the right bytes but disabled at boot — which a byte compare cannot see
// (#982). Read-only.
func NetworkNftUnitNeedsConverge(ctx context.Context) (bool, error) {
	return nftUnitNeedsConverge(ctx, NetworkNftServiceUnitPath, []string{HostNftPath, WeaverNftPath}, unitEnabledAtBoot)
}

// nftUnitNeedsConverge is NetworkNftUnitNeedsConverge with its paths and probe
// injected, so tests need neither /usr/lib nor systemd.
func nftUnitNeedsConverge(ctx context.Context, unitPath string, artifacts []string, enabled unitEnabledProbe) (bool, error) {
	embedded, err := templates.Files.ReadFile(networkNftServiceTemplate)
	if err != nil {
		return false, errorx.InternalError.Wrap(err, "failed to read embedded %s", networkNftServiceTemplate)
	}

	current, err := os.ReadFile(unitPath)
	switch {
	case err == nil:
		if !bytes.Equal(current, embedded) {
			logx.As().Debug().Str("unit", unitPath).
				Msg("installed nftables loader unit differs from the embedded copy")
			return true, nil
		}
		// Bytes match, so now ask systemd. Only here, so a host with no unit at
		// all never opens a DBus connection.
		on, enabledErr := enabled(ctx)
		if enabledErr != nil {
			return false, errorx.ExternalError.Wrap(enabledErr,
				"failed to read whether %s is enabled at boot", NetworkNftService)
		}
		if !on {
			logx.As().Debug().Str("unit", unitPath).
				Msg("nftables loader unit is current but disabled; it will not run at boot")
		}
		return !on, nil
	case os.IsNotExist(err):
		// No unit: install one only where an nft artifact is actually persisted,
		// so hosts that never used the firewall get no boot unit.
		provisioned, statErr := anyNftArtifactExists(artifacts)
		if statErr != nil {
			return false, statErr
		}
		if provisioned {
			logx.As().Debug().Str("unit", unitPath).
				Msg("persisted nft artifact with no loader unit to replay it at boot")
		}
		return provisioned, nil
	default:
		return false, errorx.ExternalError.Wrap(err, "failed to read %s", unitPath)
	}
}

// anyNftArtifactExists reports whether any plane has persisted an nft document
// to replay at boot.
func anyNftArtifactExists(artifacts []string) (bool, error) {
	for _, p := range artifacts {
		if _, err := os.Stat(p); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, errorx.ExternalError.Wrap(err, "failed to stat %s", p)
		}
	}
	return false, nil
}
