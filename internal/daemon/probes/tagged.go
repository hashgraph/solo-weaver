// SPDX-License-Identifier: Apache-2.0

package probes

import (
	"context"

	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// TaggedProbe wraps a leaf Probe and attaches an operator-facing Reason code
// and Resolution hint to any error it returns via errorx properties. Use it
// inside RequiredProbe() implementations so that every disk-prerequisite
// failure carries context-specific guidance for the operator.
//
// The reason and resolution are stored as models.ErrPropertyReason and
// models.ErrPropertyResolution on the returned errorx error. Callers extract
// them via errorx.ExtractProperty.
//
// Example:
//
//	&probes.TaggedProbe{
//	    Inner:      &probes.DiskOwnershipProbe{Path: upgradeRoot, ...},
//	    Reason:     "UpgradeRootOwnershipCheckFailed",
//	    Resolution: "sudo chown hedera:hedera " + upgradeRoot,
//	}
type TaggedProbe struct {
	Inner      Probe
	Reason     string
	Resolution string
}

// Probe implements Probe. Delegates to Inner; on failure wraps the error as an
// errorx.ExternalError carrying ErrPropertyReason and ErrPropertyResolution so
// that runComponentProbes can build a rich daemonkit.StatusError without additional
// type assertions.
func (p *TaggedProbe) Probe(ctx context.Context) error {
	if err := p.Inner.Probe(ctx); err != nil {
		return errorx.ExternalError.New("%s", err.Error()).
			WithProperty(models.ErrPropertyReason, p.Reason).
			WithProperty(models.ErrPropertyResolution, []string{p.Resolution})
	}
	return nil
}
