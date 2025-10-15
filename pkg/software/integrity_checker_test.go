package software

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"os"
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func TestFileDownloader_VerifyChecksum(t *testing.T) {
	// Create temporary file with known content
	testContent := "Hello, World!"
	tmpFile, err := os.CreateTemp("", "test_checksum_*.txt")
	require.NoError(t, err, "Failed to create temp file")
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString(testContent)
	require.NoError(t, err, "Failed to write test content to temp file")
	tmpFile.Close()

	// Test MD5 algorithm
	hash := md5.New()
	hash.Write([]byte(testContent))
	expectedMD5 := fmt.Sprintf("%x", hash.Sum(nil))

	err = VerifyChecksum(tmpFile.Name(), expectedMD5, "md5")
	require.NoError(t, err, "MD5 verification through VerifyChecksum failed")

	// Test SHA256 algorithm
	hash256 := sha256.New()
	hash256.Write([]byte(testContent))
	expectedSHA256 := fmt.Sprintf("%x", hash256.Sum(nil))

	err = VerifyChecksum(tmpFile.Name(), expectedSHA256, "sha256")
	require.NoError(t, err, "SHA256 verification through VerifyChecksum failed")

	// Test SHA512 algorithm
	hash512 := sha512.New()
	hash512.Write([]byte(testContent))
	expectedSHA512 := fmt.Sprintf("%x", hash512.Sum(nil))

	err = VerifyChecksum(tmpFile.Name(), expectedSHA512, "sha512")
	require.NoError(t, err, "SHA512 verification through VerifyChecksum failed")

	// Test with wrong checksum
	err = VerifyChecksum(tmpFile.Name(), "wronghash", "sha256")
	require.Error(t, err, "VerifyChecksum should fail with wrong checksum")
	require.True(t, errorx.IsOfType(err, ChecksumError), "Error should be of type ChecksumError")

	// Test with unsupported algorithm
	err = VerifyChecksum(tmpFile.Name(), expectedSHA256, "sha1")
	require.Error(t, err, "VerifyChecksum should fail with unsupported algorithm")
	require.True(t, errorx.IsOfType(err, ChecksumError), "Error should be of type ChecksumError")

	// Test with non-existent file
	err = VerifyChecksum("/non/existent/file", expectedSHA256, "sha256")
	require.Error(t, err, "VerifyChecksum should fail with non-existent file")

	require.True(t, errorx.IsOfType(err, FileNotFoundError), "Error should be of type FileNotFoundError")
}
