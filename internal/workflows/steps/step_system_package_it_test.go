// SPDX-License-Identifier: Apache-2.0

//go:build integration

package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/testutil"
	"github.com/stretchr/testify/require"
)

// isModuleLoaded checks if a kernel module is currently loaded
func isModuleLoaded(t *testing.T, moduleName string) bool {
	t.Helper()

	// Check via /sys/module directory
	if _, err := os.Stat("/sys/module/" + moduleName); err == nil {
		return true
	}

	// Fallback: check /proc/modules
	content, err := os.ReadFile("/proc/modules")
	if err != nil {
		t.Logf("Warning: could not read /proc/modules: %v", err)
		return false
	}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, moduleName+" ") {
			return true
		}
	}
	return false
}

// isModulePersisted checks if a kernel module is configured to load at boot
func isModulePersisted(t *testing.T, moduleName string) bool {
	t.Helper()

	confPath := filepath.Join("/etc/modules-load.d", moduleName+".conf")
	content, err := os.ReadFile(confPath)
	if err != nil {
		return false
	}

	return strings.Contains(string(content), moduleName)
}

// cleanupKernelModule ensures a kernel module is unloaded and unpersisted
func cleanupKernelModule(t *testing.T, moduleName string) {
	t.Helper()

	// Remove persistence configuration first
	confPath := filepath.Join("/etc/modules-load.d", moduleName+".conf")
	if _, err := os.Stat(confPath); err == nil {
		rmCmd := exec.Command("rm", "-f", confPath)
		rmCmd = testutil.Sudo(rmCmd)
		if err := rmCmd.Run(); err != nil {
			t.Logf("Warning: failed to remove persistence config for module %s: %v", moduleName, err)
		}
	}

	// Unload the module if it's currently loaded
	if isModuleLoaded(t, moduleName) {
		rmmodCmd := exec.Command("/usr/sbin/rmmod", moduleName)
		rmmodCmd = testutil.Sudo(rmmodCmd)
		if err := rmmodCmd.Run(); err != nil {
			t.Logf("Warning: failed to unload module %s: %v", moduleName, err)
		}
	}
}

// preLoadModule loads a kernel module without persisting it
func preLoadModule(t *testing.T, moduleName string) {
	t.Helper()

	modprobeCmd := exec.Command("/usr/sbin/modprobe", moduleName)
	modprobeCmd = testutil.Sudo(modprobeCmd)
	err := modprobeCmd.Run()
	require.NoError(t, err, "Failed to pre-load module %s via modprobe", moduleName)
}

// prePersistModule creates persistence configuration for a kernel module without loading it
func prePersistModule(t *testing.T, moduleName string) {
	t.Helper()

	confPath := filepath.Join("/etc/modules-load.d", moduleName+".conf")
	confDir := filepath.Dir(confPath)

	// Ensure directory exists
	mkdirCmd := exec.Command("mkdir", "-p", confDir)
	mkdirCmd = testutil.Sudo(mkdirCmd)
	err := mkdirCmd.Run()
	require.NoError(t, err, "Failed to create modules-load.d directory")

	// Write module name to config file
	writeCmd := exec.Command("sh", "-c", fmt.Sprintf("echo '%s' > %s", moduleName, confPath))
	writeCmd = testutil.Sudo(writeCmd)
	err = writeCmd.Run()
	require.NoError(t, err, "Failed to write persistence config")
}

// preLoadAndPersistModule loads a kernel module and creates persistence configuration
func preLoadAndPersistModule(t *testing.T, moduleName string) {
	t.Helper()

	// First load the module
	preLoadModule(t, moduleName)

	// Then persist it
	prePersistModule(t, moduleName)
}

func Test_KernelModuleStep_Overlay_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	testutil.Reset(t)

	moduleName := "overlay"

	// Cleanup before test
	cleanupKernelModule(t, moduleName)

	// Ensure module is not loaded initially
	require.False(t, isModuleLoaded(t, moduleName),
		"Module %s should not be loaded initially", moduleName)

	// Build and execute just the kernel module step
	step := InstallKernelModule(moduleName)
	stepWorkflow, err := automa.NewWorkflowBuilder().WithId("test-overlay-module").
		Steps(step).
		Build()
	require.NoError(t, err, "Failed to build step workflow")

	report := stepWorkflow.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to execute overlay module step")
	require.Equal(t, automa.StatusSuccess, report.Status,
		"Overlay module step should complete successfully")

	// Verify module is loaded and persisted
	require.True(t, isModuleLoaded(t, moduleName),
		"Module %s should be loaded after step execution", moduleName)
	require.True(t, isModulePersisted(t, moduleName),
		"Module %s should be persisted after step execution", moduleName)

	// Cleanup after test
	t.Cleanup(func() {
		cleanupKernelModule(t, moduleName)
	})
}

func Test_KernelModuleStep_BrNetfilter_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	moduleName := "br_netfilter"

	// Cleanup before test
	cleanupKernelModule(t, moduleName)

	// Ensure module is not loaded initially
	require.False(t, isModuleLoaded(t, moduleName),
		"Module %s should not be loaded initially", moduleName)

	// Build and execute just the kernel module step
	step := InstallKernelModule(moduleName)
	stepWorkflow, err := automa.NewWorkflowBuilder().WithId("test-br-netfilter-module").
		Steps(step).
		Build()
	require.NoError(t, err, "Failed to build step workflow")

	report := stepWorkflow.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to execute br_netfilter module step")
	require.Equal(t, automa.StatusSuccess, report.Status,
		"br_netfilter module step should complete successfully")

	// Verify module is loaded and persisted
	require.True(t, isModuleLoaded(t, moduleName),
		"Module %s should be loaded after step execution", moduleName)
	require.True(t, isModulePersisted(t, moduleName),
		"Module %s should be persisted after step execution", moduleName)

	// Cleanup after test
	t.Cleanup(func() {
		cleanupKernelModule(t, moduleName)
	})
}

func Test_KernelModulesStep_Combined_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	modules := []string{"overlay", "br_netfilter"}

	// Cleanup before test
	for _, moduleName := range modules {
		cleanupKernelModule(t, moduleName)
	}

	// Ensure modules are not loaded initially
	for _, moduleName := range modules {
		require.False(t, isModuleLoaded(t, moduleName),
			"Module %s should not be loaded initially", moduleName)
	}

	// Build workflow with both kernel module steps
	workflow, err := automa.NewWorkflowBuilder().WithId("test-combined-modules").
		Steps(
			InstallKernelModule("overlay"),
			InstallKernelModule("br_netfilter"),
		).
		Build()
	require.NoError(t, err, "Failed to build combined workflow")

	report := workflow.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to execute combined module workflow")
	require.Equal(t, automa.StatusSuccess, report.Status,
		"Combined module workflow should complete successfully")

	// Verify both modules are loaded and persisted
	for _, moduleName := range modules {
		require.True(t, isModuleLoaded(t, moduleName),
			"Module %s should be loaded after workflow execution", moduleName)
		require.True(t, isModulePersisted(t, moduleName),
			"Module %s should be persisted after workflow execution", moduleName)
	}

	// Cleanup after test
	t.Cleanup(func() {
		for _, moduleName := range modules {
			cleanupKernelModule(t, moduleName)
		}
	})
}

func Test_KernelModuleStep_AlreadyLoaded_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	moduleName := "overlay"

	// Cleanup before test
	cleanupKernelModule(t, moduleName)

	// Pre-load the module manually (but don't persist it)
	preLoadModule(t, moduleName)

	// Verify module is loaded but not persisted
	require.True(t, isModuleLoaded(t, moduleName),
		"Module %s should be loaded before test", moduleName)
	require.False(t, isModulePersisted(t, moduleName),
		"Module %s should not be persisted before test", moduleName)

	// Build and execute the kernel module step
	step := InstallKernelModule(moduleName)
	stepWorkflow, err := automa.NewWorkflowBuilder().WithId("test-already-loaded-module").
		Steps(step).
		Build()
	require.NoError(t, err, "Failed to build step workflow")

	report := stepWorkflow.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to execute module step on already loaded module")
	require.Equal(t, automa.StatusSuccess, report.Status,
		"Module step should complete successfully even when module is already loaded")

	// Verify module is still loaded and now persisted
	require.True(t, isModuleLoaded(t, moduleName),
		"Module %s should remain loaded after step execution", moduleName)
	require.True(t, isModulePersisted(t, moduleName),
		"Module %s should be persisted after step execution", moduleName)

	// Cleanup after test
	t.Cleanup(func() {
		cleanupKernelModule(t, moduleName)
	})
}

func Test_KernelModuleStep_AlreadyPersisted_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	moduleName := "br_netfilter"

	// Cleanup before test
	cleanupKernelModule(t, moduleName)

	// Pre-persist the module manually (but don't load it at runtime)
	prePersistModule(t, moduleName)

	// Verify module is persisted but not loaded
	require.False(t, isModuleLoaded(t, moduleName),
		"Module %s should not be loaded before test", moduleName)
	require.True(t, isModulePersisted(t, moduleName),
		"Module %s should be persisted before test", moduleName)

	// Build and execute the kernel module step
	step := InstallKernelModule(moduleName)
	stepWorkflow, err := automa.NewWorkflowBuilder().WithId("test-already-persisted-module").
		Steps(step).
		Build()
	require.NoError(t, err, "Failed to build step workflow")

	report := stepWorkflow.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to execute module step on already persisted module")
	require.Equal(t, automa.StatusSuccess, report.Status,
		"Module step should complete successfully even when module is already persisted")

	// Verify module is now loaded and still persisted
	require.True(t, isModuleLoaded(t, moduleName),
		"Module %s should be loaded after step execution", moduleName)
	require.True(t, isModulePersisted(t, moduleName),
		"Module %s should remain persisted after step execution", moduleName)

	// Cleanup after test
	t.Cleanup(func() {
		cleanupKernelModule(t, moduleName)
	})
}

func Test_KernelModuleStep_AlreadyLoadedAndPersisted_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	moduleName := "overlay"

	// Cleanup before test
	cleanupKernelModule(t, moduleName)

	// Preload and persist the module manually
	preLoadAndPersistModule(t, moduleName)

	// Verify module is both loaded and persisted
	require.True(t, isModuleLoaded(t, moduleName),
		"Module %s should be loaded before test", moduleName)
	require.True(t, isModulePersisted(t, moduleName),
		"Module %s should be persisted before test", moduleName)

	// Build and execute the kernel module step
	step := InstallKernelModule(moduleName)
	stepWorkflow, err := automa.NewWorkflowBuilder().WithId("test-already-setup-module").
		Steps(step).
		Build()
	require.NoError(t, err, "Failed to build step workflow")

	report := stepWorkflow.Execute(context.Background())
	require.NoError(t, report.Error, "Failed to execute module step on already setup module")
	require.Equal(t, automa.StatusSuccess, report.Status,
		"Module step should complete successfully even when module is already loaded and persisted")

	// Verify module remains loaded and persisted
	require.True(t, isModuleLoaded(t, moduleName),
		"Module %s should remain loaded after step execution", moduleName)
	require.True(t, isModulePersisted(t, moduleName),
		"Module %s should remain persisted after step execution", moduleName)

	// Cleanup after test
	t.Cleanup(func() {
		cleanupKernelModule(t, moduleName)
	})
}

func Test_KernelModuleStep_IdempotencyCheck_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	moduleName := "overlay"

	// Cleanup before test
	cleanupKernelModule(t, moduleName)

	// Build the kernel module step
	step := InstallKernelModule(moduleName)
	stepWorkflow, err := automa.NewWorkflowBuilder().WithId("test-idempotency-module").
		Steps(step).
		Build()
	require.NoError(t, err, "Failed to build step workflow")

	// Execute the step multiple times to test idempotency
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("execution_%d", i+1), func(t *testing.T) {
			report := stepWorkflow.Execute(context.Background())
			require.NoError(t, report.Error, "Failed to execute module step on iteration %d", i+1)
			require.Equal(t, automa.StatusSuccess, report.Status,
				"Module step should complete successfully on iteration %d", i+1)

			// Verify module is loaded and persisted after each execution
			require.True(t, isModuleLoaded(t, moduleName),
				"Module %s should be loaded after iteration %d", moduleName, i+1)
			require.True(t, isModulePersisted(t, moduleName),
				"Module %s should be persisted after iteration %d", moduleName, i+1)
		})
	}

	// Cleanup after test
	t.Cleanup(func() {
		cleanupKernelModule(t, moduleName)
	})
}

func Test_KernelModuleStep_RollbackOnFailure_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	validModule := "overlay"
	// Use a module that doesn't exist to simulate failure
	invalidModule := "nonexistent_module_test_12345"

	// Cleanup before test
	cleanupKernelModule(t, validModule)

	// Ensure valid module is not loaded initially
	require.False(t, isModuleLoaded(t, validModule),
		"Module %s should not be loaded initially", validModule)

	// Build workflow with one valid and one invalid module step
	workflow, err := automa.NewWorkflowBuilder().WithId("test-rollback-on-failure").
		Steps(
			InstallKernelModule(validModule),
			InstallKernelModule(invalidModule), // This should fail
		).
		WithExecutionMode(automa.RollbackOnError).
		Build()
	require.NoError(t, err, "Failed to build workflow")

	// Execute the workflow - should fail on the second step
	report := workflow.Execute(context.Background())
	require.Error(t, report.Error, "Workflow should fail because of error")
	require.Equal(t, automa.StatusFailed, report.Status,
		"Workflow should have failed status")

	// Verify the valid module was loaded but then should be rolled back automatically
	// Note: This depends on the automa framework's behavior on failure
	// The first step succeeds, but when the second fails, rollback should happen automatically
	require.False(t, isModuleLoaded(t, validModule),
		"Module %s should be unloaded after automatic rollback", validModule)
	require.False(t, isModulePersisted(t, validModule),
		"Module %s should be unpersisted after automatic rollback", validModule)

	// Cleanup after test
	t.Cleanup(func() {
		cleanupKernelModule(t, validModule)
	})
}

// Test to verify rollback behavior with mixed pre-existing and new modules
func Test_KernelModuleStep_RollbackMixedScenario_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges to load kernel modules")
	}

	preExistingModule := "overlay"
	newModule := "br_netfilter"

	// Cleanup before test
	cleanupKernelModule(t, preExistingModule)
	cleanupKernelModule(t, newModule)

	// Preload and persist the first module to simulate pre-existing state
	preLoadAndPersistModule(t, preExistingModule)

	// Verify initial state
	require.True(t, isModuleLoaded(t, preExistingModule),
		"Pre-existing module should be loaded initially")
	require.True(t, isModulePersisted(t, preExistingModule),
		"Pre-existing module should be persisted initially")
	require.False(t, isModuleLoaded(t, newModule),
		"New module should not be loaded initially")

	// Build workflow with modules and a failing step
	workflow, err := automa.NewWorkflowBuilder().WithId("test-rollback-mixed").
		Steps(
			InstallKernelModule(preExistingModule),               // Should be no-op
			InstallKernelModule(newModule),                       // Should load the module
			InstallKernelModule("nonexistent_module_mixed_test"), // Should fail
		).
		WithExecutionMode(automa.RollbackOnError).
		Build()
	require.NoError(t, err, "Failed to build workflow")

	// Execute the workflow - should fail on the third step
	report := workflow.Execute(context.Background())
	require.Error(t, report.Error, "Workflow should not fail for an error")
	require.Equal(t, automa.StatusFailed, report.Status,
		"Workflow should have failed status")

	// After rollback:
	// - Pre-existing module should remain loaded/persisted (wasn't loaded by step)
	// - New module should be unloaded/unpersisted (was loaded by step)
	require.True(t, isModuleLoaded(t, preExistingModule),
		"Pre-existing module should remain loaded after rollback")
	require.True(t, isModulePersisted(t, preExistingModule),
		"Pre-existing module should remain persisted after rollback")
	require.False(t, isModuleLoaded(t, newModule),
		"New module should be unloaded after rollback")
	require.False(t, isModulePersisted(t, newModule),
		"New module should be unpersisted after rollback")

	// Cleanup after test
	t.Cleanup(func() {
		cleanupKernelModule(t, preExistingModule)
		cleanupKernelModule(t, newModule)
	})
}
