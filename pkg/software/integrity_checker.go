// SPDX-License-Identifier: Apache-2.0

package software

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"io"
	"os"
)

// IsSupportedAlgorithm reports whether VerifyChecksum can verify the given algorithm.
func IsSupportedAlgorithm(algorithm string) bool {
	switch algorithm {
	case "md5", "sha256", "sha512":
		return true
	default:
		return false
	}
}

// VerifyChecksum dynamically verifies the checksum of a file using the specified algorithm
func VerifyChecksum(filePath string, expectedValue string, algorithm string) error {
	switch algorithm {
	case "md5":
		return checksum(filePath, expectedValue, algorithm, md5.New())
	case "sha256":
		return checksum(filePath, expectedValue, algorithm, sha256.New())
	case "sha512":
		return checksum(filePath, expectedValue, algorithm, sha512.New())
	default:
		return NewChecksumError(filePath, algorithm, expectedValue, "")
	}
}

// checksum verifies the hash of a file.
// algorithm is the name of the hash function (e.g. "sha256"), used only for error
// reporting; hashFunction is the matching hash implementation (md5.New(), etc.).
func checksum(filePath string, expectedHash string, algorithm string, hashFunction hash.Hash) error {
	file, err := os.Open(filePath)
	if err != nil {
		return NewFileNotFoundError(filePath)
	}
	defer file.Close()

	if _, err := io.Copy(hashFunction, file); err != nil {
		return NewChecksumError(filePath, algorithm, expectedHash, "")
	}

	calculatedHash := fmt.Sprintf("%x", hashFunction.Sum(nil))
	if calculatedHash != expectedHash {
		return NewChecksumError(filePath, algorithm, expectedHash, calculatedHash)
	}

	return nil
}
