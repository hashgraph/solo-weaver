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

// VerifyChecksum dynamically verifies the checksum of a file using the specified algorithm
func VerifyChecksum(filePath string, expectedValue string, algorithm string) error {
	switch algorithm {
	case "md5":
		return checksum(filePath, expectedValue, md5.New())
	case "sha256":
		return checksum(filePath, expectedValue, sha256.New())
	case "sha512":
		return checksum(filePath, expectedValue, sha512.New())
	default:
		return NewChecksumError(filePath, algorithm, expectedValue, "")
	}
}

// checksum verifies the hash of a file
// hashType is the hash function to use, e.g. md5.New(), sha256.New(), sha512.New()
func checksum(filePath string, expectedHash string, hashFunction hash.Hash) error {
	file, err := os.Open(filePath)
	if err != nil {
		return NewFileNotFoundError(filePath)
	}
	defer file.Close()

	if _, err := io.Copy(hashFunction, file); err != nil {
		return NewChecksumError(filePath, "unknown", expectedHash, "")
	}

	calculatedHash := fmt.Sprintf("%x", hashFunction.Sum(nil))
	if calculatedHash != expectedHash {
		return NewChecksumError(filePath, "unknown", expectedHash, calculatedHash)
	}

	return nil
}
