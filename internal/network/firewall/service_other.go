// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package firewall

import (
	"context"

	"github.com/automa-saga/logx"
)

// defaultApplyViaService is the non-Linux stub. systemd is Linux-only; on
// other platforms (e.g. macOS dev/test builds) there is no service to restart,
// so this only logs.
func defaultApplyViaService(_ context.Context) error {
	logx.As().Debug().Str("service", NetworkNftService).Msg("service apply is a no-op on non-Linux builds")
	return nil
}
