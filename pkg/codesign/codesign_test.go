// SPDX-License-Identifier: Apache-2.0

package codesign

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/stretchr/testify/require"
)

// Test_EmbeddedKey_VerifiesRealReleaseSignature proves the keys compiled into the
// binary are the ones that actually sign releases: it verifies real detached
// signatures captured from published releases, one per signing key the project
// has used. Every key in the embedded set needs a fixture here — dropping a key
// (or, as in #1036, forgetting to add one after a rotation) then fails offline
// instead of surfacing only when an operator hits the auto-download path.
func Test_EmbeddedKey_VerifiesRealReleaseSignature(t *testing.T) {
	tests := []struct {
		name     string
		asset    string
		sig      string
		signedBy string
	}{
		{
			name:     "pre-v0.28.0 key DB125DC2EB561F1C",
			asset:    "testdata/release-asset.sha256",
			sig:      "testdata/release-asset.sha256.asc",
			signedBy: "DB125DC2EB561F1C",
		},
		{
			name:     "v0.28.0 onwards key F9423513CFB6304E",
			asset:    "testdata/release-asset-v0.28.1.sha256",
			sig:      "testdata/release-asset-v0.28.1.sha256.asc",
			signedBy: "F9423513CFB6304E",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			content, err := os.Open(tc.asset)
			require.NoError(t, err)
			defer content.Close()
			sig, err := os.Open(tc.sig)
			require.NoError(t, err)
			defer sig.Close()

			require.NoError(t, Verify(content, sig),
				"embedded keyring must verify a real release signature made by %s", tc.signedBy)
		})
	}
}

func Test_EmbeddedKey_RejectsTamperedContent(t *testing.T) {
	original, err := os.ReadFile("testdata/release-asset.sha256")
	require.NoError(t, err)
	sig, err := os.ReadFile("testdata/release-asset.sha256.asc")
	require.NoError(t, err)

	tampered := append([]byte("x"), original...)
	err = Verify(bytes.NewReader(tampered), bytes.NewReader(sig))
	require.Error(t, err, "tampered content must not verify")
	require.Contains(t, err.Error(), "verification failed")
}

// Test_LoadTrustedKeys_LoadsEmbeddedUIDlessKey confirms the embedded key (which
// carries no user-id packet) loads at the packet level — ReadArmoredKeyRing would
// reject it — and exposes the expected primary + subkey ids.
func Test_LoadTrustedKeys_LoadsEmbeddedUIDlessKey(t *testing.T) {
	trusted, err := loadTrustedKeys()
	require.NoError(t, err)
	require.NotEmpty(t, trusted)

	// Both release signing keys stay embedded: the current one verifies new
	// releases, the previous one still verifies a pinned older version.
	for name, keyID := range map[string]uint64{
		"DB125DC2EB561F1C": 0xDB125DC2EB561F1C,
		"F9423513CFB6304E": 0xF9423513CFB6304E,
	} {
		_, ok := trusted[keyID]
		require.True(t, ok, "embedded primary key id %s must be present", name)
	}
}

func Test_VerifyWith_GeneratedKey(t *testing.T) {
	entity, err := openpgp.NewEntity("solo-weaver test", "codesign unit test", "test@example.com", nil)
	require.NoError(t, err)

	trusted := publicKeyMapFromEntity(t, entity)
	payload := []byte("solo-provisioner-daemon fake binary bytes")

	var sigBuf bytes.Buffer
	require.NoError(t, openpgp.ArmoredDetachSign(&sigBuf, entity, bytes.NewReader(payload), nil))

	t.Run("valid signature passes", func(t *testing.T) {
		err := verifyWith(trusted, bytes.NewReader(payload), bytes.NewReader(sigBuf.Bytes()))
		require.NoError(t, err)
	})

	t.Run("tampered content fails", func(t *testing.T) {
		err := verifyWith(trusted, strings.NewReader("different bytes"), bytes.NewReader(sigBuf.Bytes()))
		require.Error(t, err)
		require.Contains(t, err.Error(), "verification failed")
	})

	t.Run("signature by an untrusted key is rejected", func(t *testing.T) {
		other, err := openpgp.NewEntity("attacker", "", "attacker@example.com", nil)
		require.NoError(t, err)
		var otherSig bytes.Buffer
		require.NoError(t, openpgp.ArmoredDetachSign(&otherSig, other, bytes.NewReader(payload), nil))

		err = verifyWith(trusted, bytes.NewReader(payload), bytes.NewReader(otherSig.Bytes()))
		require.Error(t, err)
		require.Contains(t, err.Error(), "not an embedded trust anchor")
	})
}

func Test_Verify_MalformedSignature(t *testing.T) {
	err := Verify(strings.NewReader("payload"), strings.NewReader("not an armored signature"))
	require.Error(t, err)
}

// publicKeyMapFromEntity serializes an entity's public half, round-trips it
// through collectPublicKeys (the same parser used for the embedded keys), and
// returns the resulting key-id -> public key map.
func publicKeyMapFromEntity(t *testing.T, entity *openpgp.Entity) map[uint64]*packet.PublicKey {
	t.Helper()
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	require.NoError(t, err)
	require.NoError(t, entity.Serialize(w))
	require.NoError(t, w.Close())

	out := map[uint64]*packet.PublicKey{}
	require.NoError(t, collectPublicKeys(buf.Bytes(), out))
	require.NotEmpty(t, out)
	return out
}
