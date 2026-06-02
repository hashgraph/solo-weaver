// SPDX-License-Identifier: Apache-2.0

package network

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

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
	return "", errorx.IllegalState.New("no connected network interface found")
}

// ProbeTCP opens a TCP connection to addr and closes it immediately. On failure
// it retries with retryDelay between attempts until overallTimeout elapses (or
// the parent context is cancelled). Each individual dial uses dialTimeout.
//
// Returns the number of attempts made and the last dial error if all attempts
// failed; returns (n, nil) on the first successful connect.
//
// Use for "is this service reachable from here" probes where MetalLB ARP
// convergence or Cilium reconciler latency may delay first-byte arrival but
// silent failure (eBPF table miss, dropped SYN) needs to surface as an error.
func ProbeTCP(ctx context.Context, addr string, overallTimeout, dialTimeout, retryDelay time.Duration) (int, error) {
	probeCtx, cancel := context.WithTimeout(ctx, overallTimeout)
	defer cancel()

	var lastErr error
	attempts := 0
	for {
		attempts++
		dialCtx, dialCancel := context.WithTimeout(probeCtx, dialTimeout)
		var d net.Dialer
		conn, err := d.DialContext(dialCtx, "tcp", addr)
		dialCancel()
		if err == nil {
			_ = conn.Close()
			return attempts, nil
		}
		lastErr = err

		retry := time.NewTimer(retryDelay)
		select {
		case <-probeCtx.Done():
			retry.Stop()
			return attempts, lastErr
		case <-retry.C:
		}
	}
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
