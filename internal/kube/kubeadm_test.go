package kube

import (
	"net"
	"regexp"
	"testing"
)

func TestGenerateKubeadmToken(t *testing.T) {
	token, err := GenerateKubeadmToken()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Token should match the format: [a-z0-9]{6}\.[a-z0-9]{16}
	re := regexp.MustCompile(`^[a-z0-9]{6}\.[a-z0-9]{16}$`)
	if !re.MatchString(token) {
		t.Errorf("token %q does not match expected format", token)
	}
}

func TestGetMachineIP_Integration(t *testing.T) {
	ip, err := getMachineIP()
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
