package network

import (
	"net"
	"testing"
)

func TestGetMachineIP_Integration(t *testing.T) {
	ip, err := GetMachineIP()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("returned IP %q is not a valid IP address", ip)
	}
	if parsed.IsLoopback() {
		t.Errorf("returned IP %q is a loopback address", ip)
	}
	if parsed.To4() == nil {
		t.Errorf("returned IP %q is not an IPv4 address", ip)
	}
}
