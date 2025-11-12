package kube

import (
	"os"
	"path"
	"strconv"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
)

var (
	kubeDir string
)

// UserKubeDir prepares the .kube directory path based on user's home directory
func UserKubeDir() (string, error) {
	if kubeDir != "" {
		return kubeDir, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	kubeDir = path.Join(homeDir, ".kube")

	return kubeDir, nil
}

// getCurrentUser retrieves the current user using the principal package
func getCurrentUser() (principal.User, error) {
	pm, err := principal.NewManager()
	if err != nil {
		return nil, err
	}

	uid := strconv.FormatUint(uint64(os.Geteuid()), 10)
	currentUser, err := pm.LookupUserById(uid)
	if err != nil {
		return nil, err
	}

	return currentUser, nil
}

// ConfigureKubeConfig copies the kubeconfig file to the user's home directory and sets the ownership
// to the current user. This allows kubectl to be used without requiring root privileges.
func ConfigureKubeConfig() error {
	fm, err := fsx.NewManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create file manager")
	}

	if kubeDir == "" {
		_, err = UserKubeDir()
		if err != nil {
			return errorx.IllegalState.Wrap(err, "failed to get user .kube directory")
		}
	}

	err = fm.CreateDirectory(kubeDir, false)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create %s directory", kubeDir)
	}

	kubeConfigSrc := "/etc/kubernetes/admin.conf"
	kubeConfigDest := path.Join(kubeDir, "config")
	err = fm.CopyFile(kubeConfigSrc, kubeConfigDest, true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to copy kubeconfig file %q to %q", kubeConfigSrc, kubeConfigDest)
	}

	currentUser, err := getCurrentUser()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get current user")
	}

	err = fm.WriteOwner(kubeDir, currentUser, currentUser.PrimaryGroup(), true)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to set ownership of .kube directory: %q", kubeDir)
	}

	return nil
}
