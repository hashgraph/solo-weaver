// SPDX-License-Identifier: Apache-2.0

package kube

import (
	"errors"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/hashgraph/solo-weaver/pkg/security/principal"
)

func TestKubeConfigManager_Configure(t *testing.T) {
	tests := []struct {
		name          string
		customKubeDir string
		setupMocks    func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager)
		expectError   bool
		errorContains string
	}{
		{
			name:          "success - configures kubeconfig successfully",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)

				// Expect root directory operations
				fm.EXPECT().CreateDirectory("/root/.kube", false).Return(nil)
				fm.EXPECT().CopyFile(kubeConfigSourcePath, "/root/.kube/config", true).Return(nil)

				// Expect root user/group lookup and ownership setting
				mockRootUser := principal.NewMockUser(ctrl)
				mockRootGroup := principal.NewMockGroup(ctrl)
				pm.EXPECT().LookupUserByName("root").Return(mockRootUser, nil)
				pm.EXPECT().LookupGroupByName("root").Return(mockRootGroup, nil)
				fm.EXPECT().WriteOwner("/root/.kube", mockRootUser, mockRootGroup, true).Return(nil)
			},
			expectError: false,
		},
		{
			name:          "error - lookup user fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()
				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(nil, errors.New("user not found"))
			},
			expectError:   true,
			errorContains: "failed to lookup weaver user",
		},
		{
			name:          "error - lookup group fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(nil, errors.New("group not found"))
			},
			expectError:   true,
			errorContains: "failed to lookup weaver group",
		},
		{
			name:          "error - create directory fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(errors.New("permission denied"))
			},
			expectError:   true,
			errorContains: "failed to create",
		},
		{
			name:          "error - copy file fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(errors.New("source file not found"))
			},
			expectError:   true,
			errorContains: "failed to copy kubeconfig file",
		},
		{
			name:          "error - write owner fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(errors.New("chown failed"))
			},
			expectError:   true,
			errorContains: "failed to set ownership",
		},
		{
			name:          "success - with custom kubeDir",
			customKubeDir: "/custom/kube/path",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				customKubeDir := "/custom/kube/path"

				fm.EXPECT().CreateDirectory(customKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(customKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(customKubeDir, mockUser, mockGroup, true).Return(nil)

				// Expect root directory operations
				fm.EXPECT().CreateDirectory("/root/.kube", false).Return(nil)
				fm.EXPECT().CopyFile(kubeConfigSourcePath, "/root/.kube/config", true).Return(nil)

				// Expect root user/group lookup and ownership setting
				mockRootUser := principal.NewMockUser(ctrl)
				mockRootGroup := principal.NewMockGroup(ctrl)
				pm.EXPECT().LookupUserByName("root").Return(mockRootUser, nil)
				pm.EXPECT().LookupGroupByName("root").Return(mockRootGroup, nil)
				fm.EXPECT().WriteOwner("/root/.kube", mockRootUser, mockRootGroup, true).Return(nil)
			},
			expectError: false,
		},
		{
			name:          "error - create root directory fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)

				// Root directory creation fails
				fm.EXPECT().CreateDirectory("/root/.kube", false).Return(errors.New("permission denied"))
			},
			expectError:   true,
			errorContains: "failed to create /root/.kube directory",
		},
		{
			name:          "error - copy to root directory fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)

				// Root directory operations
				fm.EXPECT().CreateDirectory("/root/.kube", false).Return(nil)
				fm.EXPECT().CopyFile(kubeConfigSourcePath, "/root/.kube/config", true).Return(errors.New("disk full"))
			},
			expectError:   true,
			errorContains: "failed to copy kubeconfig file",
		},
		{
			name:          "error - root user lookup fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)

				// Root directory operations
				fm.EXPECT().CreateDirectory("/root/.kube", false).Return(nil)
				fm.EXPECT().CopyFile(kubeConfigSourcePath, "/root/.kube/config", true).Return(nil)

				// Root user lookup fails
				pm.EXPECT().LookupUserByName("root").Return(nil, errors.New("root user not found"))
			},
			expectError:   true,
			errorContains: "failed to lookup root user",
		},
		{
			name:          "error - root group lookup fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)

				// Root directory operations
				fm.EXPECT().CreateDirectory("/root/.kube", false).Return(nil)
				fm.EXPECT().CopyFile(kubeConfigSourcePath, "/root/.kube/config", true).Return(nil)

				// Root user lookup succeeds, but group lookup fails
				mockRootUser := principal.NewMockUser(ctrl)
				pm.EXPECT().LookupUserByName("root").Return(mockRootUser, nil)
				pm.EXPECT().LookupGroupByName("root").Return(nil, errors.New("root group not found"))
			},
			expectError:   true,
			errorContains: "failed to lookup root group",
		},
		{
			name:          "error - root WriteOwner fails",
			customKubeDir: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				svcAcc := core.ServiceAccount()

				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/weaver").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)

				pm.EXPECT().LookupUserByName(svcAcc.UserName).Return(mockUser, nil)
				pm.EXPECT().LookupGroupByName(svcAcc.GroupName).Return(mockGroup, nil)

				expectedKubeDir := path.Join("/home/weaver", ".kube")
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)

				// Root directory operations
				fm.EXPECT().CreateDirectory("/root/.kube", false).Return(nil)
				fm.EXPECT().CopyFile(kubeConfigSourcePath, "/root/.kube/config", true).Return(nil)

				// Root user/group lookup succeeds, but WriteOwner fails
				mockRootUser := principal.NewMockUser(ctrl)
				mockRootGroup := principal.NewMockGroup(ctrl)
				pm.EXPECT().LookupUserByName("root").Return(mockRootUser, nil)
				pm.EXPECT().LookupGroupByName("root").Return(mockRootGroup, nil)
				fm.EXPECT().WriteOwner("/root/.kube", mockRootUser, mockRootGroup, true).Return(errors.New("chown failed"))
			},
			expectError:   true,
			errorContains: "failed to set ownership of root .kube directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockFsxManager := fsx.NewMockManager(ctrl)
			mockPrincipalManager := principal.NewMockManager(ctrl)

			// Ensure SUDO_USER is not set for these tests to avoid unexpected behavior
			// from configureCurrentUserKubeConfig()
			originalSudoUser := os.Getenv("SUDO_USER")
			_ = os.Unsetenv("SUDO_USER")
			defer func() {
				if originalSudoUser != "" {
					_ = os.Setenv("SUDO_USER", originalSudoUser)
				}
			}()

			// Setup mocks
			tt.setupMocks(ctrl, mockFsxManager, mockPrincipalManager)

			// Create manager with mocked dependencies
			mgr := KubeConfigManager{
				fsManager:        mockFsxManager,
				principalManager: mockPrincipalManager,
			}

			if tt.customKubeDir != "" {
				mgr.SetKubeDir(tt.customKubeDir)
			}

			// Call Configure
			err := mgr.Configure()

			// Verify results
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if tt.errorContains != "" {
					errMsg := err.Error()
					if !strings.Contains(errMsg, tt.errorContains) {
						t.Errorf("expected error to contain %q, but got %q", tt.errorContains, errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestKubeConfigManager_ConfigureCurrentUserKubeConfig(t *testing.T) {
	tests := []struct {
		name          string
		sudoUser      string
		setupMocks    func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager)
		expectError   bool
		errorContains string
	}{
		{
			name:     "success - configures kubeconfig for sudo user",
			sudoUser: "testuser",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/testuser").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

				pm.EXPECT().LookupUserByName("testuser").Return(mockUser, nil)

				expectedKubeDir := "/home/testuser/.kube"
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)
			},
			expectError: false,
		},
		{
			name:     "skip - SUDO_USER is root",
			sudoUser: "root",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed as this should return early
			},
			expectError: false,
		},
		{
			name:     "skip - SUDO_USER is empty",
			sudoUser: "",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed as this should return early
			},
			expectError: false,
		},
		{
			name:     "error - lookup user fails",
			sudoUser: "testuser",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				pm.EXPECT().LookupUserByName("testuser").Return(nil, errors.New("user not found"))
			},
			expectError:   true,
			errorContains: "failed to lookup current user",
		},
		{
			name:     "error - user has no primary group",
			sudoUser: "testuser",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(nil).AnyTimes()

				pm.EXPECT().LookupUserByName("testuser").Return(mockUser, nil)
			},
			expectError:   true,
			errorContains: "has no primary group",
		},
		{
			name:     "error - create directory fails",
			sudoUser: "testuser",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/testuser").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

				pm.EXPECT().LookupUserByName("testuser").Return(mockUser, nil)

				expectedKubeDir := "/home/testuser/.kube"
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(errors.New("permission denied"))
			},
			expectError:   true,
			errorContains: "failed to create",
		},
		{
			name:     "error - copy file fails",
			sudoUser: "testuser",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/testuser").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

				pm.EXPECT().LookupUserByName("testuser").Return(mockUser, nil)

				expectedKubeDir := "/home/testuser/.kube"
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(errors.New("copy failed"))
			},
			expectError:   true,
			errorContains: "failed to copy kubeconfig file",
		},
		{
			name:     "error - write owner fails",
			sudoUser: "testuser",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/testuser").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

				pm.EXPECT().LookupUserByName("testuser").Return(mockUser, nil)

				expectedKubeDir := "/home/testuser/.kube"
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(errors.New("chown failed"))
			},
			expectError:   true,
			errorContains: "failed to set ownership of current user .kube directory",
		},

		// Security test cases - path traversal attempts
		{
			name:     "security - SUDO_USER with path traversal (../)",
			sudoUser: "../etc/passwd",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "path traversal sequences",
		},
		{
			name:     "security - SUDO_USER with forward slash",
			sudoUser: "user/admin",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with backslash",
			sudoUser: "user\\admin",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with absolute path",
			sudoUser: "/etc/passwd",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with double dots",
			sudoUser: "user..admin",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "path traversal sequences",
		},

		// Security test cases - shell metacharacters (command injection attempts)
		{
			name:     "security - SUDO_USER with semicolon (command separator)",
			sudoUser: "user;rm -rf /",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with pipe",
			sudoUser: "user|cat /etc/passwd",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with ampersand",
			sudoUser: "user&command",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with dollar sign (variable expansion)",
			sudoUser: "user$PATH",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with backtick (command substitution)",
			sudoUser: "user`whoami`",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with greater than (redirection)",
			sudoUser: "user>file",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with less than (redirection)",
			sudoUser: "user<file",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with parentheses (subshell)",
			sudoUser: "user(command)",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with braces",
			sudoUser: "user{test}",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with brackets",
			sudoUser: "user[test]",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with asterisk (glob)",
			sudoUser: "user*",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with question mark (glob)",
			sudoUser: "user?",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - SUDO_USER with tilde (home expansion)",
			sudoUser: "user~",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},

		// Security test cases - special characters
		{
			name:     "security - SUDO_USER with spaces",
			sudoUser: "user name",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with at sign",
			sudoUser: "user@host",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with hash",
			sudoUser: "user#comment",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with percent",
			sudoUser: "user%test",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with exclamation",
			sudoUser: "user!test",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with plus",
			sudoUser: "user+test",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with equals",
			sudoUser: "user=test",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with comma",
			sudoUser: "user,admin",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with period",
			sudoUser: "user.admin",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - SUDO_USER with colon",
			sudoUser: "user:admin",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},

		// Security test cases - attack vectors
		{
			name:     "security - SQL injection attempt in SUDO_USER",
			sudoUser: "admin' OR '1'='1",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},
		{
			name:     "security - command injection with semicolon",
			sudoUser: "user; cat /etc/shadow",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "shell metacharacters",
		},
		{
			name:     "security - newline injection",
			sudoUser: "user\nadmin",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				// No mocks needed - should fail validation before any operations
			},
			expectError:   true,
			errorContains: "invalid characters",
		},

		// Valid edge cases that should pass
		{
			name:     "valid - username with underscore",
			sudoUser: "user_name",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/user_name").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

				pm.EXPECT().LookupUserByName("user_name").Return(mockUser, nil)

				expectedKubeDir := "/home/user_name/.kube"
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)
			},
			expectError: false,
		},
		{
			name:     "valid - username with hyphen",
			sudoUser: "user-name",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/user-name").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

				pm.EXPECT().LookupUserByName("user-name").Return(mockUser, nil)

				expectedKubeDir := "/home/user-name/.kube"
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)
			},
			expectError: false,
		},
		{
			name:     "valid - username with numbers",
			sudoUser: "user123",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/user123").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

				pm.EXPECT().LookupUserByName("user123").Return(mockUser, nil)

				expectedKubeDir := "/home/user123/.kube"
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)
			},
			expectError: false,
		},
		{
			name:     "valid - username with mixed case",
			sudoUser: "UserName",
			setupMocks: func(ctrl *gomock.Controller, fm *fsx.MockManager, pm *principal.MockManager) {
				mockUser := principal.NewMockUser(ctrl)
				mockUser.EXPECT().HomeDir().Return("/home/UserName").AnyTimes()

				mockGroup := principal.NewMockGroup(ctrl)
				mockUser.EXPECT().PrimaryGroup().Return(mockGroup).AnyTimes()

				pm.EXPECT().LookupUserByName("UserName").Return(mockUser, nil)

				expectedKubeDir := "/home/UserName/.kube"
				fm.EXPECT().CreateDirectory(expectedKubeDir, false).Return(nil)

				expectedConfigDest := path.Join(expectedKubeDir, "config")
				fm.EXPECT().CopyFile(kubeConfigSourcePath, expectedConfigDest, true).Return(nil)

				fm.EXPECT().WriteOwner(expectedKubeDir, mockUser, mockGroup, true).Return(nil)
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the SUDO_USER environment variable for the test
			originalSudoUser := os.Getenv("SUDO_USER")
			if tt.sudoUser != "" {
				_ = os.Setenv("SUDO_USER", tt.sudoUser)
			} else {
				_ = os.Unsetenv("SUDO_USER")
			}
			defer func() {
				if originalSudoUser != "" {
					_ = os.Setenv("SUDO_USER", originalSudoUser)
				} else {
					_ = os.Unsetenv("SUDO_USER")
				}
			}()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockFsxManager := fsx.NewMockManager(ctrl)
			mockPrincipalManager := principal.NewMockManager(ctrl)

			// Setup mocks
			tt.setupMocks(ctrl, mockFsxManager, mockPrincipalManager)

			// Create manager with mocked dependencies
			mgr := KubeConfigManager{
				fsManager:        mockFsxManager,
				principalManager: mockPrincipalManager,
			}

			// Call configureCurrentUserKubeConfig
			err := mgr.configureCurrentUserKubeConfig()

			// Verify results
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if tt.errorContains != "" {
					errMsg := err.Error()
					if !strings.Contains(errMsg, tt.errorContains) {
						t.Errorf("expected error to contain %q, but got %q", tt.errorContains, errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}
