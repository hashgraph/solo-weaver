// SPDX-License-Identifier: Apache-2.0

//go:build linux

package unitconv

import (
	"context"

	"github.com/automa-saga/logx"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
)

// EnableIfDisabled re-enables service if systemd would not start it at boot.
// Failures only warn; the drift probe reports the host again next time.
func EnableIfDisabled(ctx context.Context, service string) error {
	enabled, err := soos.IsServiceEnabled(ctx, service)
	if err != nil {
		logx.As().Warn().Err(err).Str("service", service).
			Msg("could not read whether the service is enabled at boot")
		return nil
	}
	if enabled {
		return nil
	}

	logx.As().Info().Str("service", service).
		Msg("unit is current but disabled; enabling it so its state is replayed after a reboot")
	if err := soos.EnableService(ctx, service); err != nil {
		logx.As().Warn().Err(err).Str("service", service).Msg("could not enable service at boot")
	}
	return nil
}
