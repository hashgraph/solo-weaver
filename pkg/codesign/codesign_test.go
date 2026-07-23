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

// Test_EmbeddedKey_VerifiesRealReleaseSignature proves the key compiled into the
// binary is the one that actually signs releases: it verifies a real detached
// signature captured from a published release (testdata/release-asset.sha256*).
func Test_EmbeddedKey_VerifiesRealReleaseSignature(t *testing.T) {
	content, err := os.Open("testdata/release-asset.sha256")
	require.NoError(t, err)
	defer content.Close()
	sig, err := os.Open("testdata/release-asset.sha256.asc")
	require.NoError(t, err)
	defer sig.Close()

	require.NoError(t, Verify(content, sig), "embedded release key must verify a real release signature")
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

	const primaryKeyID = 0xDB125DC2EB561F1C
	_, ok := trusted[primaryKeyID]
	require.True(t, ok, "embedded primary key id DB125DC2EB561F1C must be present")
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
