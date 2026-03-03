// SPDX-License-Identifier: Apache-2.0

package bll

import "github.com/joomcode/errorx"

// ErrPropertyResolution is the errorx property key used to attach remediation
// hints to precondition errors — mirrors doctor.ErrPropertyResolution without
// creating a circular import.
var ErrPropertyResolution = errorx.RegisterProperty("resolution")
