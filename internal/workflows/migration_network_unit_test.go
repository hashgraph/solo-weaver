// SPDX-License-Identifier: Apache-2.0

package workflows

import (
	"context"
	"reflect"
	"runtime"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/network/firewall"
	"github.com/hashgraph/solo-weaver/internal/network/shape"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNetworkUnitMigration_Metadata pins the IDs and descriptions of both units:
// the IDs are recorded in migration state, so a rename would re-run them.
func TestNetworkUnitMigration_Metadata(t *testing.T) {
	nft := NewNetworkNftUnitMigration()
	assert.Equal(t, "network-nft-loader-unit", nft.ID())
	assert.Contains(t, nft.Description(), "solo-provisioner-network-nft.service")

	shaper := NewNetworkShaperUnitMigration()
	assert.Equal(t, "network-bandwidth-shaper-unit", shaper.ID())
	assert.Contains(t, shaper.Description(), "solo-provisioner-bandwidth-shaper.service")
}

// funcName returns the fully-qualified name of f, so the wiring below is checked
// by identity — a crossed probe is still non-nil, so NotNil would not catch it.
func funcName(f any) string {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Func || v.IsNil() {
		return ""
	}
	return runtime.FuncForPC(v.Pointer()).Name()
}

// TestNetworkUnitMigration_WiresBothPlanes checks each constructor reaches its
// own plane's probe and ensure, so the two are not crossed.
func TestNetworkUnitMigration_WiresBothPlanes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		m       *NetworkUnitMigration
		service string
		probe   func() (bool, error)
		ensure  func(context.Context) error
	}{
		{
			name:    "nft loader",
			m:       NewNetworkNftUnitMigration(),
			service: "solo-provisioner-network-nft.service",
			probe:   firewall.NetworkNftUnitNeedsConverge,
			ensure:  firewall.EnsureNetworkNftUnit,
		},
		{
			name:    "bandwidth shaper",
			m:       NewNetworkShaperUnitMigration(),
			service: "solo-provisioner-bandwidth-shaper.service",
			probe:   shape.TcEgressUnitNeedsConverge,
			ensure:  shape.EnsureTcEgressUnit,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.service, tc.m.spec.service)
			assert.Equal(t, funcName(tc.probe), funcName(tc.m.spec.probe),
				"the migration must probe its own plane's unit")
			assert.Equal(t, funcName(tc.ensure), funcName(tc.m.spec.ensure),
				"the migration must converge its own plane's unit")
		})
	}
}

// stubbedUnitMigration builds a migration whose probe answers as scripted and
// pins the caller as root, so no host state is touched.
func stubbedUnitMigration(t *testing.T, needsConverge bool, err error) *NetworkUnitMigration {
	t.Helper()
	stubUnitEuid(t, 0)
	return &NetworkUnitMigration{spec: networkUnitSpec{
		id:      "test-unit",
		service: "test-unit.service",
		probe:   func() (bool, error) { return needsConverge, err },
		ensure:  func(context.Context) error { return nil },
	}}
}

// stubUnitEuid pins the effective uid Applies sees.
func stubUnitEuid(t *testing.T, euid int) {
	t.Helper()
	orig := networkUnitGeteuid
	t.Cleanup(func() { networkUnitGeteuid = orig })
	networkUnitGeteuid = func() int { return euid }
}

func TestNetworkUnitMigration_Applies(t *testing.T) {
	tests := []struct {
		name          string
		needsConverge bool
		err           error
		want          bool
	}{
		{"host already on the current unit, enabled, is skipped", false, nil, false},
		// True covers both drift shapes: wrong bytes, and byte-current-but-disabled.
		{"host needing the unit converged is picked up", true, nil, true},
		// A probe error must never propagate to RunStartupMigrations.
		{"probe failure is skipped, not fatal", true, errorx.ExternalError.New("boom"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := stubbedUnitMigration(t, tc.needsConverge, tc.err)
			got, err := m.Applies(newMctx("0.28.1", "0.29.0"))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestNetworkUnitMigration_AppliesSkipsUnprivilegedCaller checks that a non-root
// caller skips without probing.
func TestNetworkUnitMigration_AppliesSkipsUnprivilegedCaller(t *testing.T) {
	probed := false
	m := stubbedUnitMigration(t, true, nil)
	m.spec.probe = func() (bool, error) { probed = true; return true, nil }
	stubUnitEuid(t, 1000) // after stubbedUnitMigration, which pins root

	got, err := m.Applies(newMctx("0.28.1", "0.29.0"))
	require.NoError(t, err)
	assert.False(t, got, "an unprivileged caller must not attempt the /usr/lib write")
	assert.False(t, probed, "the unit probe must be short-circuited by the privilege gate")
}

// TestNetworkUnitMigration_AppliesIsVersionIndependent checks Applies fires
// regardless of the installed/current version pair.
func TestNetworkUnitMigration_AppliesIsVersionIndependent(t *testing.T) {
	m := stubbedUnitMigration(t, true, nil)

	for _, mctx := range []struct {
		installed, current string
	}{
		{"0.28.1", "0.29.0"}, // upgrade
		{"0.29.0", "0.29.0"}, // same version, re-run
		{"", "0.29.0"},       // no recorded version
	} {
		got, err := m.Applies(newMctx(mctx.installed, mctx.current))
		require.NoError(t, err)
		assert.True(t, got, "installed=%q current=%q", mctx.installed, mctx.current)
	}
}

func TestNetworkUnitMigration_Execute(t *testing.T) {
	t.Run("converges the unit", func(t *testing.T) {
		calls := 0
		m := stubbedUnitMigration(t, true, nil)
		m.spec.ensure = func(context.Context) error { calls++; return nil }
		require.NoError(t, m.Execute(context.Background(), newMctx("0.28.1", "0.29.0")))
		assert.Equal(t, 1, calls)
	})

	// A failure must not propagate; it would fail every command on the host.
	t.Run("a failure is warned about, not returned", func(t *testing.T) {
		m := stubbedUnitMigration(t, true, nil)
		m.spec.ensure = func(context.Context) error { return errorx.ExternalError.New("read-only fs") }
		require.NoError(t, m.Execute(context.Background(), newMctx("0.28.1", "0.29.0")))
	})
}

func TestNetworkUnitMigration_RollbackIsNoOp(t *testing.T) {
	m := stubbedUnitMigration(t, true, nil)
	require.NoError(t, m.Rollback(context.Background(), newMctx("0.28.1", "0.29.0")))
}
