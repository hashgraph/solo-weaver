package kube

import (
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
