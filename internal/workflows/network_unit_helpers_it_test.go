// SPDX-License-Identifier: Apache-2.0

//go:build integration

// network_unit_helpers_it_test.go holds the systemd helpers both network boot-unit
// suites use. The two units differ only in their name and path, so the snapshot and
// property reads are parameterised rather than mirrored per plane.

package workflows

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/coreos/go-systemd/v22/dbus"
	soos "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/stretchr/testify/require"
)

// systemdAnalyzeCandidates are absolute paths only, never a bare
// "systemd-analyze" off PATH (see docs/dev/security-model.md).
var systemdAnalyzeCandidates = []string{"/usr/bin/systemd-analyze", "/bin/systemd-analyze"}

func requireRootForUnitTests(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}
}

// backupUnit snapshots the unit installed at unitPath and its enablement and
// restores both afterwards, so a run on a real host changes nothing.
func backupUnit(t *testing.T, unitPath, service string) {
	t.Helper()
	ctx := context.Background()

	original, readErr := os.ReadFile(unitPath)
	existed := readErr == nil
	if !existed {
		require.True(t, os.IsNotExist(readErr), "could not read %s: %v", unitPath, readErr)
	}
	wasEnabled, _ := soos.IsServiceEnabled(ctx, service)

	t.Cleanup(func() {
		restoreCtx := context.Background()
		if existed {
			_ = os.WriteFile(unitPath, original, 0o644)
		} else {
			_ = os.Remove(unitPath)
		}
		_ = soos.DaemonReload(restoreCtx)
		switch {
		case existed && wasEnabled:
			_ = soos.EnableService(restoreCtx, service)
		case !wasEnabled:
			_ = soos.DisableService(restoreCtx, service)
		}
	})
}

// unitProperties returns the Unit- and Service-interface properties systemd
// itself parsed, so a rejected or misplaced directive is absent here while a
// string match on the file would still pass.
func unitProperties(t *testing.T, unit string) map[string]any {
	t.Helper()
	ctx := context.Background()

	conn, err := dbus.NewSystemConnectionContext(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Force systemd to load the unit; an unloaded unit exposes no properties.
	_, err = conn.ListUnitsByNamesContext(ctx, []string{unit})
	require.NoError(t, err)

	props, err := conn.GetUnitPropertiesContext(ctx, unit)
	require.NoError(t, err)

	serviceProps, err := conn.GetUnitTypePropertiesContext(ctx, unit, "Service")
	require.NoError(t, err)
	for k, v := range serviceProps {
		props[k] = v
	}
	return props
}

// unitOrdering returns the After= and Before= lists systemd itself parsed from
// the installed unit.
func unitOrdering(t *testing.T, unit string) (after, before []string) {
	t.Helper()
	props := unitProperties(t, unit)
	return stringList(t, props, "After"), stringList(t, props, "Before")
}

func stringList(t *testing.T, props map[string]any, key string) []string {
	t.Helper()
	value, ok := props[key]
	require.True(t, ok, "unit property %q is absent", key)
	list, ok := value.([]string)
	require.True(t, ok, "unit property %q is %T, not a string list", key, value)
	return list
}

// verifyOrdering runs `systemd-analyze verify` over the given units and returns
// its output lowercased. The exit status is ignored: verify warns about unrelated
// things on a real host, so only the cycle diagnostic is read.
func verifyOrdering(t *testing.T, bin string, units ...string) string {
	t.Helper()
	out, _ := exec.CommandContext(context.Background(), bin,
		append([]string{"verify"}, units...)...).CombinedOutput()
	return strings.ToLower(string(out))
}
