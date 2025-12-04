// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/bluet/syspkg/manager"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-weaver/pkg/hardware"
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

// Distribution detection helpers for testing
type Distribution string

const (
	Ubuntu Distribution = "ubuntu"
	Debian Distribution = "debian"
	RHEL   Distribution = "rhel"
	Oracle Distribution = "oracle"
	CentOS Distribution = "centos"
	Fedora Distribution = "fedora"
)

// getDistribution detects the current Linux distribution
func getDistribution(t *testing.T) Distribution {
	requireLinux(t)

	hostProfile := hardware.GetHostProfile()
	vendor := strings.ToLower(hostProfile.GetOSVendor())

	switch {
	case strings.Contains(vendor, "ubuntu"):
		return Ubuntu
	case strings.Contains(vendor, "debian"):
		return Debian
	case strings.Contains(vendor, "red hat") || strings.Contains(vendor, "rhel"):
		return RHEL
	case strings.Contains(vendor, "oracle"):
		return Oracle
	case strings.Contains(vendor, "centos"):
		return CentOS
	case strings.Contains(vendor, "fedora"):
		return Fedora
	default:
		t.Skipf("Unsupported distribution: %s", vendor)
		return ""
	}
}

// testPackageInstallUninstall is a generic test function for package install/uninstall operations
func testPackageInstallUninstall(t *testing.T, createPackage func() (Package, error), expectedName string) {
	requireLinux(t)
	requireRoot(t)

	// Refresh package index to ensure we have the latest package information
	// This is especially important in CI environments where the package cache may be stale
	err := RefreshPackageIndex()
	require.NoError(t, err, "failed to refresh package index")

	pkg, err := createPackage()
	require.NoError(t, err, "failed to create %s package", expectedName)
	require.NotNil(t, pkg, "package should not be nil")
	require.Equal(t, expectedName, pkg.Name(), "package name mismatch")

	// Ensure clean state: uninstall the package if it's already installed
	if pkg.IsInstalled() {
		t.Logf("Package %s is already installed, uninstalling for clean test state", expectedName)
		_, err := pkg.Uninstall()
		require.NoError(t, err, "failed to uninstall pre-existing %s", expectedName)
	}

	// Test installation
	info, err := pkg.Install()
	require.NoError(t, err, "failed to install %s", expectedName)
	require.Equal(t, expectedName, info.Name, "package name mismatch after installation")
	require.Equal(t, manager.PackageStatusInstalled, info.Status, "package should be installed after installation")
	require.True(t, pkg.IsInstalled(), "package should be installed after installation")

	// Verify package info
	verifyInfo, err := pkg.Info()
	t.Logf("Package info after install: %+v", verifyInfo)
	require.NoError(t, err, "failed to get package info")
	require.Equal(t, expectedName, verifyInfo.Name, "package name mismatch in info")

	// Test IsInstalled()
	require.True(t, pkg.IsInstalled(), "IsInstalled should return true after installation")

	// Test uninstallation
	info, err = pkg.Uninstall()
	t.Logf("Package info after uninstall: %+v", info)
	require.NoError(t, err, "failed to uninstall %s", expectedName)
	require.Equal(t, expectedName, info.Name, "package name mismatch after uninstallation")
	require.NotEqual(t, manager.PackageStatusInstalled, info.Status, "package status should be empty after uninstallation")
	require.False(t, pkg.IsInstalled(), "package should not be installed after uninstallation")

	// Test IsInstalled() after uninstall
	require.False(t, pkg.IsInstalled(), "IsInstalled should return false after uninstallation")
}

func TestIptablesInstallUninstall(t *testing.T) {
	testPackageInstallUninstall(t, NewIptables, "iptables")
}

func TestGpgInstallUninstall(t *testing.T) {
	testPackageInstallUninstall(t, NewGpg, "gpg")
}

func TestConntrackInstallUninstall(t *testing.T) {
	testPackageInstallUninstall(t, NewConntrack, "conntrack")
}

func TestSocatInstallUninstall(t *testing.T) {
	testPackageInstallUninstall(t, NewSocat, "socat")
}

func TestEbtablesInstallUninstall(t *testing.T) {
	testPackageInstallUninstall(t, NewEbtables, "ebtables")
}

func TestNftablesInstallUninstall(t *testing.T) {
	testPackageInstallUninstall(t, NewNftables, "nftables")
}

func TestContainerdInstallUninstall(t *testing.T) {
	testPackageInstallUninstall(t, NewContainerd, "containerd")
}

// Test package creation without installation
func TestPackageCreation(t *testing.T) {
	requireLinux(t) // This will skip the test on non-Linux systems

	packages := []struct {
		name    string
		creator func() (Package, error)
	}{
		{"iptables", NewIptables},
		{"gpg", NewGpg},
		{"conntrack", NewConntrack},
		{"socat", NewSocat},
		{"ebtables", NewEbtables},
		{"nftables", NewNftables},
		{"containerd", NewContainerd},
	}

	for _, pkg := range packages {
		t.Run(pkg.name, func(t *testing.T) {
			p, err := pkg.creator()
			require.NoError(t, err, "failed to create %s package", pkg.name)
			require.NotNil(t, p, "package should not be nil")
			require.Equal(t, pkg.name, p.Name(), "package name mismatch")
		})
	}
}

// Test package manager detection
func TestPackageManagerDetection(t *testing.T) {
	requireLinux(t)
	requireRoot(t) // RefreshPackageIndex requires root privileges

	pm, err := GetPackageManager()
	require.NoError(t, err, "failed to get package manager")
	require.NotNil(t, pm, "package manager should not be nil")

	// Test that we can refresh package index without errors
	err = RefreshPackageIndex()
	require.NoError(t, err, "failed to refresh package index")
}

// Test distribution-specific package managers
func TestDistributionPackageManagers(t *testing.T) {
	requireLinux(t)
	requireRoot(t)

	dist := getDistribution(t)
	pm, err := GetPackageManager()
	require.NoError(t, err, "failed to get package manager")

	t.Logf("Detected distribution: %s", dist)
	t.Logf("Package manager type: %T", pm)

	// Verify package manager works with a simple package query
	pkg, err := NewIptables()
	require.NoError(t, err, "failed to create iptables package")

	// Just check if we can get package info without installing
	info, err := pkg.Info()
	if err != nil {
		t.Logf("Package info error (expected for uninstalled packages): %v", err)
	} else {
		t.Logf("Package info: %+v", info)
	}
}

// Test error handling
func TestPackageNotFound(t *testing.T) {
	requireLinux(t)

	// Test with a non-existent package
	pkg, err := NewPackageInstaller(WithPackageName("this-package-does-not-exist"))
	require.NoError(t, err, "should be able to create installer for non-existent package")
	require.False(t, pkg.IsInstalled(), "non-existent package should not be installed")
}

// Test AutoRemove functionality
func TestAutoRemove(t *testing.T) {
	requireLinux(t)
	requireRoot(t) // AutoRemove requires root privileges

	dist := getDistribution(t)

	// AutoRemove is only supported on apt-based systems
	if dist != Ubuntu && dist != Debian {
		t.Skipf("AutoRemove is only supported on apt-based systems, current distribution: %s", dist)
	}

	// Test that AutoRemove doesn't fail
	err := AutoRemove()
	require.NoError(t, err, "AutoRemove should not fail")
}

// Test AutoRemove on non-apt systems
func TestAutoRemoveNonAptSystem(t *testing.T) {
	requireLinux(t)
	requireRoot(t)

	dist := getDistribution(t)

	// Only run this test on non-apt systems
	if dist == Ubuntu || dist == Debian {
		t.Skip("This test is for non-apt systems only")
	}

	// AutoRemove should fail on non-apt systems
	err := AutoRemove()
	require.Error(t, err, "AutoRemove should fail on non-apt systems")
	require.Contains(t, err.Error(), "autoremove is only supported for apt package manager")
}

// Test AutoRemove without root privileges
func TestAutoRemoveWithoutRoot(t *testing.T) {
	requireLinux(t)

	if os.Geteuid() == 0 {
		t.Skip("This test requires non-root user")
	}

	dist := getDistribution(t)

	// Only test on apt-based systems
	if dist != Ubuntu && dist != Debian {
		t.Skipf("AutoRemove is only supported on apt-based systems, current distribution: %s", dist)
	}

	// AutoRemove should fail without root privileges
	err := AutoRemove()
	require.Error(t, err, "AutoRemove should fail without root privileges")
}

// Test AutoRemove integration with package manager
func TestAutoRemoveWithPackageManager(t *testing.T) {
	requireLinux(t)
	requireRoot(t)

	dist := getDistribution(t)

	// Only test on apt-based systems
	if dist != Ubuntu && dist != Debian {
		t.Skipf("AutoRemove is only supported on apt-based systems, current distribution: %s", dist)
	}

	// Get package manager and verify it's apt
	pm, err := GetPackageManager()
	require.NoError(t, err, "failed to get package manager")
	require.NotNil(t, pm, "package manager should not be nil")

	// Test that we can call AutoRemove multiple times without issues
	err = AutoRemove()
	require.NoError(t, err, "first AutoRemove should not fail")

	err = AutoRemove()
	require.NoError(t, err, "second AutoRemove should not fail")
}
