// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/hashgraph/solo-weaver/internal/core"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/joomcode/errorx"
)

// GetMachineIP retrieves the first non-loopback IP address of the machine
func GetMachineIP() (string, error) {
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

// CheckEndpointReachable verifies that a URL endpoint is reachable.
// It extracts the base URL (scheme + host) and performs a simple HTTP HEAD request.
// Returns nil if reachable, error otherwise.
func CheckEndpointReachable(ctx context.Context, urlStr string, timeout time.Duration) error {
	if urlStr == "" {
		return nil
	}

	// Parse URL to get base (scheme + host)
	parsed, err := url.Parse(urlStr)
	if err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid URL %q", urlStr)
	}

	// Build base URL for health check (just scheme + host, no path)
	baseURL := parsed.Scheme + "://" + parsed.Host

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: timeout,
		// Don't follow redirects for health checks
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, baseURL, nil)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to create request for %s", baseURL)
	}

	// Perform request
	resp, err := client.Do(req)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "endpoint %s is not reachable", baseURL)
	}
	defer resp.Body.Close()

	// Any response (even 4xx/5xx) means the endpoint is reachable
	// We're not checking auth here, just network connectivity
	return nil
}

// InstallCrioRegistriesConf installs the custom registries.conf with registry mirror configuration
// This enables CRI-O to use a local registry mirror for caching Kubernetes images
// This is typically called during integration test setup when cache proxy is available
func InstallCrioRegistriesConf() error {
	// Read the custom registries.conf template
	content, err := templates.Read("files/crio/registries.conf")
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to read registries.conf template")
	}

	// Build the registries.conf path inline to avoid import cycle
	// This is equivalent to software.GetRegistriesConfPath()
	registriesConfPath := filepath.Join(core.Paths().SandboxDir, "etc", "containers", "registries.conf.d", "registries.conf")

	// if the directory does not exist, create it
	err = os.MkdirAll(filepath.Dir(registriesConfPath), core.DefaultDirOrExecPerm)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create registries.conf.d directory")
	}

	// Write to the sandbox registries.conf.d directory
	err = os.WriteFile(registriesConfPath, []byte(content), core.DefaultFilePerm)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write custom registries.conf")
	}

	return nil
}
