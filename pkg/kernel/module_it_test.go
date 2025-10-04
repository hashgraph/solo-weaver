//go:build integration
// +build integration

package kernel

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModule_Load_Integration tests the actual Module Load function with persistence
// This test requires root privileges and a Linux system with modprobe
func TestModule_Load_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	tests := []struct {
		name        string
		moduleName  string
		persist     bool
		expectError bool
	}{
		{
			name:        "load dummy module without persistence",
			moduleName:  "dummy",
			persist:     false,
			expectError: false,
		},
		{
			name:        "load dummy module with persistence",
			moduleName:  "dummy",
			persist:     true,
			expectError: false,
		},
		{
			name:        "load nonexistent module",
			moduleName:  "nonexistent_module_12345",
			persist:     false,
			expectError: true,
		},
		{
			name:        "load invalid module name with persistence",
			moduleName:  "invalid-module!@#",
			persist:     true,
			expectError: true,
		},
		{
			name:        "load overlay module with persistence",
			moduleName:  "overlay",
			persist:     true,
			expectError: false,
		},
		{
			name:        "load overlay module without persistence",
			moduleName:  "overlay",
			persist:     false,
			expectError: false,
		},
		{
			name:        "load br_netfilter module persistence",
			moduleName:  "br_netfilter",
			persist:     true,
			expectError: false,
		},
		{
			name:        "load br_netfilter module without persistence",
			moduleName:  "br_netfilter",
			persist:     false,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			module, err := NewModule(tt.moduleName)
			if err != nil {
				t.Fatalf("failed to create module: %v", err)
			}

			_ = module.Unload(true) // ignore errors on cleanup

			err = module.Load(tt.persist)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error loading module %s, but got none", tt.moduleName)
				} else {
					t.Logf("expected error occurred: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error loading module %s: %v", tt.moduleName, err)
				} else {
					// Verify the module is actually loaded
					loaded, persisted, checkErr := module.IsLoaded()
					if checkErr != nil {
						t.Errorf("error checking if module is loaded: %v", checkErr)
					}

					if !loaded {
						t.Errorf("module %s was not loaded according to IsLoaded()", tt.moduleName)
					}

					// Verify persistence if requested
					if tt.persist && !persisted {
						t.Errorf("module %s was not persisted as requested", tt.moduleName)
					}

					if !tt.persist && persisted {
						t.Errorf("module %s was persisted when not requested", tt.moduleName)
					}

					// Additional verification: check /proc/modules
					if !isModuleLoadedInProc(t, tt.moduleName) {
						t.Errorf("module %s was not found in /proc/modules after loading", tt.moduleName)
					}

					// Verify persistence file if requested
					if tt.persist {
						confPath := filepath.Join("/etc/modules-load.d", tt.moduleName+".conf")
						if _, err := os.Stat(confPath); os.IsNotExist(err) {
							t.Errorf("persistence file %s was not created", confPath)
						}
					}

					// Clean up: unload the module after successful test
					if cleanupErr := module.Unload(true); cleanupErr != nil {
						t.Logf("warning: failed to clean up module %s: %v", tt.moduleName, cleanupErr)
					}
				}
			}
		})
	}
}

// TestModule_Unload_Integration tests the actual Module Unload function with unpersistence
// This test requires root privileges and a Linux system with modprobe
func TestModule_Unload_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	tests := []struct {
		name         string
		moduleName   string
		setupLoaded  bool // whether to load the module first
		setupPersist bool // whether to persist the module during setup
		unpersist    bool // whether to unpersist during unload
		expectError  bool
	}{
		{
			name:         "unload loaded dummy module without unpersist",
			moduleName:   "dummy",
			setupLoaded:  true,
			setupPersist: false,
			unpersist:    false,
			expectError:  false,
		},
		{
			name:         "unload loaded and persisted dummy module with unpersist",
			moduleName:   "dummy",
			setupLoaded:  true,
			setupPersist: true,
			unpersist:    true,
			expectError:  false,
		},
		{
			name:         "unload persisted module with unpersist only",
			moduleName:   "dummy",
			setupLoaded:  false,
			setupPersist: true,
			unpersist:    true,
			expectError:  false,
		},
		{
			name:         "unload nonexistent module",
			moduleName:   "nonexistent_module_12345",
			setupLoaded:  false,
			setupPersist: false,
			unpersist:    false,
			expectError:  false, // unloading non-loaded module should not error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			module, err := NewModule(tt.moduleName)
			if err != nil {
				t.Fatalf("failed to create module: %v", err)
			}

			// Setup: ensure clean state first
			_ = module.Unload(true) // ignore errors

			// Setup: load and/or persist the module if required
			if tt.setupLoaded || tt.setupPersist {
				if err := module.Load(tt.setupPersist); err != nil {
					t.Fatalf("failed to setup test by loading module %s: %v", tt.moduleName, err)
				}

				// Verify setup
				loaded, persisted, checkErr := module.IsLoaded()
				if checkErr != nil {
					t.Fatalf("error checking module state during setup: %v", checkErr)
				}

				if tt.setupLoaded && !loaded {
					t.Fatalf("module %s was not loaded during setup", tt.moduleName)
				}

				if tt.setupPersist && !persisted {
					t.Fatalf("module %s was not persisted during setup", tt.moduleName)
				}
			}

			err = module.Unload(tt.unpersist)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error unloading module %s, but got none", tt.moduleName)
				} else {
					t.Logf("expected error occurred: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error unloading module %s: %v", tt.moduleName, err)
				} else {
					// Verify the module is actually unloaded
					loaded, persisted, checkErr := module.IsLoaded()
					if checkErr != nil {
						t.Errorf("error checking if module is unloaded: %v", checkErr)
					}

					if loaded {
						t.Errorf("module %s is still loaded according to IsLoaded()", tt.moduleName)
					}

					// Verify unpersistence if requested
					if tt.unpersist && persisted {
						t.Errorf("module %s is still persisted after unpersist", tt.moduleName)
					}

					// Additional verification: check /proc/modules
					if isModuleLoadedInProc(t, tt.moduleName) {
						t.Errorf("module %s is still found in /proc/modules after unloading", tt.moduleName)
					}

					// Verify persistence file removal if unpersist was requested
					if tt.unpersist {
						confPath := filepath.Join("/etc/modules-load.d", tt.moduleName+".conf")
						if _, err := os.Stat(confPath); !os.IsNotExist(err) {
							t.Errorf("persistence file %s was not removed after unpersist", confPath)
						}
					}
				}
			}
		})
	}
}

// TestModule_LoadAndUnload_Integration tests the complete load/unload cycle
func TestModule_LoadAndUnload_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	moduleName := "dummy"
	module, err := NewModule(moduleName)
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	// Ensure module is not loaded initially
	_ = module.Unload(true)

	// Test load with persistence
	err = module.Load(true)
	if err != nil {
		t.Fatalf("failed to load module %s: %v", moduleName, err)
	}

	// Verify it's loaded and persisted
	loaded, persisted, err := module.IsLoaded()
	if err != nil {
		t.Fatalf("error checking module state: %v", err)
	}

	if !loaded {
		t.Fatalf("module %s was not loaded", moduleName)
	}

	if !persisted {
		t.Fatalf("module %s was not persisted", moduleName)
	}

	// Test unload with unpersistence
	err = module.Unload(true)
	if err != nil {
		t.Fatalf("failed to unload module %s: %v", moduleName, err)
	}

	// Verify it's unloaded and unpersisted
	loaded, persisted, err = module.IsLoaded()
	if err != nil {
		t.Fatalf("error checking module state after unload: %v", err)
	}

	if loaded {
		t.Fatalf("module %s is still loaded after unload", moduleName)
	}

	if persisted {
		t.Fatalf("module %s is still persisted after unpersist", moduleName)
	}
}

// TestModule_LoadAlreadyLoaded_Integration tests loading a module that's already loaded
func TestModule_LoadAlreadyLoaded_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	moduleName := "dummy"
	module, err := NewModule(moduleName)
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	// Ensure module is not loaded initially
	_ = module.Unload(true)

	// Load the module first time
	err = module.Load(false)
	if err != nil {
		t.Fatalf("failed to load module %s on first attempt: %v", moduleName, err)
	}

	// Load the module second time (should not fail)
	err = module.Load(false)
	if err != nil {
		t.Errorf("failed to load already loaded module %s: %v", moduleName, err)
	}

	// Verify it's still loaded
	loaded, _, err := module.IsLoaded()
	if err != nil {
		t.Errorf("error checking module state: %v", err)
	}

	if !loaded {
		t.Errorf("module %s was not loaded after second load", moduleName)
	}

	// Clean up
	_ = module.Unload(true)
}

// TestModule_PersistenceOnly_Integration tests persistence without loading
func TestModule_PersistenceOnly_Integration(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("This test requires root privileges")
	}

	moduleName := "dummy"
	module, err := NewModule(moduleName)
	if err != nil {
		t.Fatalf("failed to create module: %v", err)
	}

	// Ensure clean state
	_ = module.Unload(true)

	// Load with persistence
	err = module.Load(true)
	if err != nil {
		t.Fatalf("failed to load module with persistence: %v", err)
	}

	// Unload without unpersisting (module should be unloaded but still persisted)
	err = module.Unload(false)
	if err != nil {
		t.Fatalf("failed to unload module without unpersisting: %v", err)
	}

	// Verify state: not loaded but persisted
	loaded, persisted, err := module.IsLoaded()
	if err != nil {
		t.Fatalf("error checking module state: %v", err)
	}

	if loaded {
		t.Errorf("module %s should not be loaded after unload", moduleName)
	}

	if !persisted {
		t.Errorf("module %s should still be persisted", moduleName)
	}

	// Clean up: unpersist
	_ = module.Unload(true)
}

// isModuleLoadedInProc checks if a module is loaded by reading /proc/modules
func isModuleLoadedInProc(t *testing.T, moduleName string) bool {
	f, err := os.Open("/proc/modules")
	if err != nil {
		t.Logf("warning: could not read /proc/modules: %v", err)
		return false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// First column is the module name
		fields := strings.Fields(sc.Text())
		if len(fields) > 0 && fields[0] == moduleName {
			return true
		}
	}
	if err := sc.Err(); err != nil {
		t.Logf("warning: error scanning /proc/modules: %v", err)
	}
	return false
}
