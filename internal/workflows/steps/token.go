package steps

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// generateKubeadmToken generates a random kubeadm token in the format [a-z0-9]{6}.[a-z0-9]{16}
func generateKubeadmToken() (string, error) {
	r := make([]byte, 11)
	_, err := rand.Read(r)
	if err != nil {
		return "", fmt.Errorf("failed to generate random bytes for kubeadm token: %w", err)
	}
	return fmt.Sprintf("%s.%s", hex.EncodeToString(r[:3]), hex.EncodeToString(r[3:])), nil
}
