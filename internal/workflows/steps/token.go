package steps

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// generateKubeadmToken generates a random kubeadm token in the format [a-z0-9]{6}.[a-z0-9]{16}
var generateKubeadmToken = func() string {
	// 3 bytes = 6 hex chars, 8 bytes = 16 hex chars
	r := make([]byte, 11)
	_, err := rand.Read(r)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s.%s", hex.EncodeToString(r[:3]), hex.EncodeToString(r[3:]))
}
