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

func newHasher(algorithm string) (hash.Hash, bool) {
	switch algorithm {
	case "md5":
		return md5.New(), true
	case "sha256":
		return sha256.New(), true
	case "sha512":
		return sha512.New(), true
	default:
		return nil, false
	}
}

// VerifyChecksum dynamically verifies the checksum of a file using the specified algorithm
func VerifyChecksum(filePath string, expectedValue string, algorithm string) error {
	hashFunction, ok := newHasher(algorithm)
	if !ok {
		return NewChecksumError(filePath, algorithm, expectedValue, "")
	}
	return checksum(filePath, expectedValue, algorithm, hashFunction)
}

// fileDigest returns the lowercase hex digest of the file at filePath.
func fileDigest(filePath string, algorithm string) (string, error) {
	hashFunction, ok := newHasher(algorithm)
	if !ok {
		return "", NewChecksumError(filePath, algorithm, "", "")
	}

	file, err := openForHashing(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(hashFunction, file); err != nil {
		return "", NewChecksumError(filePath, algorithm, "", "")
	}

	return fmt.Sprintf("%x", hashFunction.Sum(nil)), nil
}

// checksum verifies the hash of a file.
// algorithm is the name of the hash function (e.g. "sha256"), used only for error
// reporting; hashFunction is the matching hash implementation (md5.New(), etc.).
func checksum(filePath string, expectedHash string, algorithm string, hashFunction hash.Hash) error {
	file, err := openForHashing(filePath)
	if err != nil {
		return err
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

// openForHashing opens filePath, reporting FileNotFoundError only when the file
// does not exist. Callers treat that type as "absent" (e.g. kube cluster
// uninstall skips the reset), so an existing file that cannot be opened must
// surface as a different error.
func openForHashing(filePath string) (*os.File, error) {
	file, err := os.Open(filePath)
	if err == nil {
		return file, nil
	}
	if os.IsNotExist(err) {
		return nil, NewFileNotFoundError(filePath)
	}
	return nil, NewFileSystemError(err).WithProperty(filePathProperty, filePath)
}
