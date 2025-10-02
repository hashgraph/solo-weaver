package steps

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// generateKubeadmToken generates a random kubeadm token in the format [a-z0-9]{6}.[a-z0-9]{16}
var generateKubeadmToken = func() (string, error) {
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
