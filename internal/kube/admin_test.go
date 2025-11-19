package kube

import (
	"errors"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/pkg/fsx"
	"golang.hedera.com/solo-weaver/pkg/security/principal"
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
