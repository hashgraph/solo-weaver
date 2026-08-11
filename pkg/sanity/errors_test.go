// SPDX-License-Identifier: Apache-2.0

package sanity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

// TestValidatorsAreDecorated asserts every exported validator attaches a reason and a hint.
func TestValidatorsAreDecorated(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantReason errx.Reason
		hintPart   string
	}{
		{"ValidateIdentifier empty", ValidateIdentifier(""), reasons.InvalidArgument, "underscore or hyphen"},
		{"ValidateIdentifier bad char", ValidateIdentifier("bad name"), reasons.InvalidArgument, "underscore or hyphen"},
		{"ValidateDNSName", ValidateDNSName("bad_host!"), reasons.InvalidArgument, "node1.example.com"},
		{"ValidateHexToken", ValidateHexToken("zzz"), reasons.InvalidArgument, "hex digits only"},
		{"ValidateHostPort", ValidateHostPort("host/path"), reasons.InvalidArgument, "no scheme, no path"},
		{"ValidateUsername", ValidateUsername("bad user"), reasons.InvalidArgument, "Linux account name"},
		{"ValidateOperationID", ValidateOperationID("bad id"), reasons.InvalidArgument, "upgrade-v0.76.0"},
		{"ValidateURL", ValidateURL("ftp://x.example.com", nil), reasons.InvalidArgument, "Use an https:// URL"},
		{"ValidateURL AllowHTTP", ValidateURL("ftp://x.example.com", &ValidateURLOptions{AllowHTTP: true}),
			reasons.InvalidArgument, "Use an http:// or https:// URL"},
		{"ValidateURL bad host", ValidateURL("https://", nil), reasons.InvalidArgument, "ASCII hostname"},
		{"ValidateVersion", ValidateVersion("not-a-version"), reasons.InvalidArgument, "semantic version"},
		{"ValidateChartReference", ValidateChartReference("bad chart"), reasons.InvalidArgument, "oci://registry"},
		{"ValidateStorageSize", ValidateStorageSize("5GB"), reasons.InvalidArgument, "Gi, Mi or Ti"},
		{"ValidateStorageSize zero", ValidateStorageSize("0Gi"), reasons.InvalidArgument, "Gi, Mi or Ti"},
		{"ValidateCIDR", ValidateCIDR("10.0.0.0"), reasons.InvalidArgument, "explicit prefix length"},
		{"ValidateIPv4CIDR on IPv6", ValidateIPv4CIDR("2001:db8::/32"), reasons.InvalidArgument, "IPv6 is not supported"},
		{"ValidatePort", ValidatePort("99999"), reasons.InvalidArgument, "between 1 and 65535"},
		{"ValidatePort empty", ValidatePort(""), reasons.InvalidArgument, "between 1 and 65535"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.err)

			reason, ok := errx.ReasonOf(tt.err)
			require.True(t, ok, "no reason attached")
			require.Equal(t, tt.wantReason, reason)

			hints, ok := errx.Hints(tt.err)
			require.True(t, ok, "no hints attached")
			require.Len(t, hints, 1)
			require.Contains(t, hints[0], tt.hintPart)
		})
	}
}

func TestSanitizersAreDecorated(t *testing.T) {
	// All three Sanitize* helpers return the shared ErrInvalidName sentinel.
	for name, err := range map[string]error{
		"SanitizeIdentifier": second(SanitizeIdentifier("!!!")),
		"SanitizeFilename":   second(SanitizeFilename("!!!")),
		"SanitizeModuleName": second(SanitizeModuleName("!!!")),
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, err)
			require.ErrorIs(t, err, ErrInvalidName, "sentinel identity must survive decoration")

			reason, ok := errx.ReasonOf(err)
			require.True(t, ok)
			require.Equal(t, reasons.InvalidArgument, reason)

			hints, ok := errx.Hints(err)
			require.True(t, ok)
			require.NotEmpty(t, hints)
		})
	}
}

func TestSanitizePathIsDecorated(t *testing.T) {
	_, err := SanitizePath("../etc/passwd")
	require.Error(t, err)

	reason, ok := errx.ReasonOf(err)
	require.True(t, ok)
	require.Equal(t, reasons.InvalidArgument, reason)

	hints, ok := errx.Hints(err)
	require.True(t, ok)
	require.Contains(t, hints[0], "absolute path")
}

func TestValidateInputFile_Reasons(t *testing.T) {
	t.Run("missing file is FileMissing, not InvalidArgument", func(t *testing.T) {
		_, err := ValidateInputFile("/nonexistent/solo-weaver-test-file")
		require.Error(t, err)

		reason, ok := errx.ReasonOf(err)
		require.True(t, ok)
		require.Equal(t, reasons.FileMissing, reason)
	})

	t.Run("an unreadable file is PermissionDenied, not Internal", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root bypasses directory permissions")
		}

		dir := t.TempDir()
		target := filepath.Join(dir, "secret.txt")
		require.NoError(t, os.WriteFile(target, []byte("x"), 0o600))
		// Clear +x on the parent so stat on the child fails with EACCES.
		require.NoError(t, os.Chmod(dir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

		_, err := ValidateInputFile(target)
		require.Error(t, err)

		reason, ok := errx.ReasonOf(err)
		require.True(t, ok)
		require.Equal(t, reasons.PermissionDenied, reason,
			"an unreadable path is the operator's to fix, not a solo-weaver bug")

		hints, ok := errx.Hints(err)
		require.True(t, ok, "PermissionDenied is actionable and must carry a hint")
		require.Contains(t, hints[0], "sudo")
	})

	t.Run("empty path is InvalidArgument", func(t *testing.T) {
		_, err := ValidateInputFile("")
		require.Error(t, err)

		reason, ok := errx.ReasonOf(err)
		require.True(t, ok)
		require.Equal(t, reasons.InvalidArgument, reason)
	})

	t.Run("a rejected path keeps SanitizePath's inner hint", func(t *testing.T) {
		// ValidateInputFile wraps without re-decorating; the inner hint must survive.
		_, err := ValidateInputFile("../etc/passwd")
		require.Error(t, err)

		hints, ok := errx.Hints(err)
		require.True(t, ok, "inner hint must not be lost through the wrap")
		require.Contains(t, hints[0], "absolute path")
	})
}

// TestErrorMessageRendering pins that decoration preserves the original text and
// namespace, and appends the printable reason to err.Error() — deliberate, for log
// correlation; doctor.OperatorMessage strips it again on human surfaces.
func TestErrorMessageRendering(t *testing.T) {
	err := ValidateIdentifier("bad name")
	require.Contains(t, err.Error(), "identifier contains invalid characters: bad name",
		"decoration must preserve the original message text")
	require.Contains(t, err.Error(), "{reason: InvalidArgument}",
		"the printable reason property must render in the message")
	require.True(t, errorx.IsOfType(err, errorx.IllegalArgument),
		"decoration must preserve the errorx namespace")
}

func second(_ string, err error) error { return err }
