package software

import (
	"github.com/bluet/syspkg/manager"
	"github.com/stretchr/testify/require"
	"os"
	"runtime"
	"testing"
)

func requireLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only runs on Linux")
	}
}

func requireRoot(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root privileges")
	}
}

func TestIptablesInstallUninstall(t *testing.T) {
	requireLinux(t)
	requireRoot(t)

	pkg, err := NewIptables()
	if err != nil {
		t.Fatalf("failed to create iptables package: %v", err)
	}

	info, err := pkg.Install()
	require.NoError(t, err, "failed to install iptables")
	require.Equal(t, "iptables", info.Name, "package name mismatch after installation")
	require.Equal(t, manager.PackageStatusInstalled, info.Status, "package should be installed after installation")

	info, err = pkg.Uninstall()
	require.NoError(t, err, "failed to uninstall iptables")
	require.Equal(t, "iptables", info.Name, "package name mismatch after installation")
	require.Empty(t, info.Status, "package should be installed after installation")
	require.True(t, !pkg.IsInstalled(), "package should not be installed after uninstallation")
}

func TestGnupg2InstallUninstall(t *testing.T) {
	requireLinux(t)
	requireRoot(t)

	pkg, err := NewGnupg2()
	if err != nil {
		t.Fatalf("failed to create gnupg2 package: %v", err)
	}

	info, err := pkg.Install()
	require.NoError(t, err)
	require.Equal(t, "gnupg2", info.Name)
	require.Equal(t, manager.PackageStatusInstalled, info.Status)

	info, err = pkg.Uninstall()
	require.NoError(t, err)
	require.Equal(t, "gnupg2", info.Name)
	require.Empty(t, info.Status)
	require.False(t, pkg.IsInstalled())
}
