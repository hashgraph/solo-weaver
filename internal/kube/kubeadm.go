package kube

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"path"
	"strconv"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/templates"
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

// GenerateKubeadmToken generates a random kubeadm token in the format [a-z0-9]{6}.[a-z0-9]{16}
var GenerateKubeadmToken = func() (string, error) {
	const allowedChars = "abcdefghijklmnopqrstuvwxyz0123456789"
	const part1Len = 6
	const part2Len = 16
	tokenPart := func(length int) (string, error) {
		b := make([]byte, length)
		for i := range b {
			nBig, err := rand.Int(rand.Reader, big.NewInt(int64(len(allowedChars))))
			if err != nil {
				return "", fmt.Errorf("failed to generate random int for kubeadm token: %w", err)
			}
			b[i] = allowedChars[nBig.Int64()]
		}
		return string(b), nil
	}
	part1, err := tokenPart(part1Len)
	if err != nil {
		return "", err
	}
	part2, err := tokenPart(part2Len)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s", part1, part2), nil
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

// getMachineIP retrieves the first non-loopback IP address of the machine
func getMachineIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		// check if the interface is up and not a loopback
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no connected network interface found")
}

// ConfigureKubeadmInit generates the kubeadm init configuration file
// It retrieves the machine IP, generates a kubeadm token, and gets the hostname
// It then renders the kubeadm-init.yaml template with the retrieved values
func ConfigureKubeadmInit(kubernetesVersion string) error {
	machineIp, err := getMachineIP()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get machine IP address")
	}

	kubeadmToken, err := GenerateKubeadmToken()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to generate kubeadm token")
	}

	hostname, err := os.Hostname()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to get hostname")
	}

	tmplData := templates.KubeadmInitData{
		KubeBootstrapToken: kubeadmToken,
		SandboxDir:         core.Paths().SandboxDir,
		MachineIP:          machineIp,
		Hostname:           hostname,
		KubernetesVersion:  kubernetesVersion,
	}

	rendered, err := templates.Render("files/kubeadm/kubeadm-init.yaml", tmplData)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render kubeadm init configuration template")
	}

	fm, err := fsx.NewManager()
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create file manager")
	}

	err = fm.WriteFile(path.Join(core.Paths().SandboxDir, "/etc/provisioner/kubeadm-init.yaml"), []byte(rendered))
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write kubeadm init configuration file")
	}

	return nil
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
