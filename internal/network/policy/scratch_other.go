// SPDX-License-Identifier: Apache-2.0

//go:build !linux

package policy

import (
	"context"

	"github.com/joomcode/errorx"
)

// ListInScratchNetns needs Linux network namespaces.
func ListInScratchNetns(_ context.Context, _, _ string) ([]byte, error) {
	return nil, errorx.IllegalState.New("scratch network namespaces are not supported on this platform")
}
