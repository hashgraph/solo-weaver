// SPDX-License-Identifier: Apache-2.0

package featuregate

import (
	"testing"

	gatekit "github.com/automa-saga/gatekit"
	"github.com/stretchr/testify/require"
)

// experimentalReadyIn is the Model C readiness gate: an opt-in (--experimental or the
// env var) may enable an alpha only when its spec is Default:true. A Default:false
// alpha is dormant (unfinished) and cannot be enabled by any opt-in. This pins that
// rule against both Default values without touching the live catalog.
func TestExperimentalReadyIn(t *testing.T) {
	const (
		readyAlpha   gatekit.Feature = "ReadyAlpha"
		dormantAlpha gatekit.Feature = "DormantAlpha"
		gaFeature    gatekit.Feature = "GAFeature"
		futureAlpha  gatekit.Feature = "FutureAlpha"
	)
	reg := gatekit.Registry{
		readyAlpha: {
			Description: "ready alpha",
			Specs:       gatekit.VersionedSpecs{{ActiveFrom: 1, Default: true, Maturity: gatekit.Alpha}},
		},
		dormantAlpha: {
			Description: "dormant alpha",
			Specs:       gatekit.VersionedSpecs{{ActiveFrom: 1, Default: false, Maturity: gatekit.Alpha}},
		},
		gaFeature: {
			Description: "ga feature",
			Specs:       gatekit.VersionedSpecs{{ActiveFrom: 1, Default: true, Maturity: gatekit.GA}},
		},
		futureAlpha: {
			Description: "future alpha",
			Specs:       gatekit.VersionedSpecs{{ActiveFrom: 5, Default: true, Maturity: gatekit.Alpha}},
		},
	}

	cases := []struct {
		name string
		f    gatekit.Feature
		want bool
	}{
		{"Alpha,Default:true is ready (dogfoodable)", readyAlpha, true},
		{"Alpha,Default:false is dormant (no opt-in can enable)", dormantAlpha, false},
		{"GA is ready", gaFeature, true},
		{"not-yet-active spec is not ready", futureAlpha, false},
		{"unknown feature is not ready", gatekit.Feature("Nope"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := experimentalReadyIn(reg, tc.f, 1)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
