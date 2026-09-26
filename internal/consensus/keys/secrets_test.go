// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSecretClient is an in-memory SecretClient for tests.
type fakeSecretClient struct {
	store       map[string]map[string][]byte // key: namespace/name
	labels      map[string]map[string]string
	annotations map[string]map[string]string
	applyCalls  int
}

func newFakeSecretClient() *fakeSecretClient {
	return &fakeSecretClient{
		store:       map[string]map[string][]byte{},
		labels:      map[string]map[string]string{},
		annotations: map[string]map[string]string{},
	}
}

func (f *fakeSecretClient) key(ns, name string) string { return ns + "/" + name }

func (f *fakeSecretClient) ApplySecret(_ context.Context, ns, name string, data map[string][]byte, labels, annotations map[string]string) error {
	cp := make(map[string][]byte, len(data))
	for k, v := range data {
		cp[k] = append([]byte(nil), v...)
	}
	f.applyCalls++
	f.store[f.key(ns, name)] = cp
	f.labels[f.key(ns, name)] = labels
	f.annotations[f.key(ns, name)] = annotations
	return nil
}

func (f *fakeSecretClient) GetSecretData(_ context.Context, ns, name string) (map[string][]byte, bool, error) {
	d, ok := f.store[f.key(ns, name)]
	return d, ok, nil
}

func (f *fakeSecretClient) GetSecretInfo(_ context.Context, ns, name string) (*kube.SecretInfo, bool, error) {
	if _, ok := f.store[f.key(ns, name)]; !ok {
		return nil, false, nil
	}
	return &kube.SecretInfo{Name: name, Labels: f.labels[f.key(ns, name)], Annotations: f.annotations[f.key(ns, name)]}, true, nil
}

func (f *fakeSecretClient) PatchSecretLabels(_ context.Context, ns, name string, labels map[string]string) error {
	merged := map[string]string{}
	for k, v := range f.labels[f.key(ns, name)] {
		merged[k] = v
	}
	for k, v := range labels {
		merged[k] = v
	}
	f.labels[f.key(ns, name)] = merged
	return nil
}

func (f *fakeSecretClient) ListSecrets(_ context.Context, ns, selector string) ([]kube.SecretInfo, error) {
	var out []kube.SecretInfo
	for k := range f.store {
		if !strings.HasPrefix(k, ns+"/") {
			continue
		}
		labels := f.labels[k]
		match := true
		for _, term := range strings.Split(selector, ",") {
			if term == "" {
				continue
			}
			kv := strings.SplitN(term, "=", 2)
			if labels[kv[0]] != kv[1] {
				match = false
			}
		}
		if match {
			out = append(out, kube.SecretInfo{Name: strings.TrimPrefix(k, ns+"/"), Labels: labels, Annotations: f.annotations[k]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// onlySecret returns the single secret in the fake whose name starts with prefix.
func (f *fakeSecretClient) onlySecret(t *testing.T, ns, prefix string) string {
	t.Helper()
	var found []string
	for k := range f.store {
		if strings.HasPrefix(k, ns+"/"+prefix) {
			found = append(found, strings.TrimPrefix(k, ns+"/"))
		}
	}
	require.Len(t, found, 1, "secrets with prefix %q: %v", prefix, found)
	return found[0]
}

// writeGeneratedFolder generates the gossip and gRPC keys, adds an admin public key, and writes them to a temp dir.
func writeGeneratedFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "keys")
	set, err := Generate(GenerateOptions{MachineID: "m"})
	require.NoError(t, err)
	require.NoError(t, set.WriteToDir(dir))
	writeAdminPub(t, dir)
	return dir
}

// writeAdminPub drops an admin public key (hex) into dir, as an operator would
// after creating the key in an external wallet.
func writeAdminPub(t *testing.T, dir string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileAdminPublic), []byte(hex.EncodeToString(pub)+"\n"), 0o644))
}

func TestImport_AllSecrets(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()

	res, err := Import(context.Background(), client, ImportOptions{
		Namespace: "orbit-1",
		NodeId:    0,
		FromDir:   dir,
	})
	require.NoError(t, err)
	require.Len(t, res.Applied, 3)

	gossipName := client.onlySecret(t, "orbit-1", "node0-gossip-keys-")
	client.onlySecret(t, "orbit-1", "node0-grpc-tls-keys-")
	adminName := client.onlySecret(t, "orbit-1", "node0-admin-pubkey-")
	assert.ElementsMatch(t, []string{gossipName, client.onlySecret(t, "orbit-1", "node0-grpc-tls-keys-"), adminName}, res.Applied)

	// Gossip secret carries both signing files.
	gossip, ok, _ := client.GetSecretData(context.Background(), "orbit-1", gossipName)
	require.True(t, ok)
	assert.Contains(t, gossip, FileGossipPrivate)
	assert.Contains(t, gossip, FileGossipCert)

	// Admin secret carries ONLY the public key.
	admin, ok, _ := client.GetSecretData(context.Background(), "orbit-1", adminName)
	require.True(t, ok)
	assert.Contains(t, admin, FileAdminPublic)
	assert.Len(t, admin, 1)
}

// Names carry the certificate date and the SHA-384 fingerprint; labels and
// annotations let the operator match a secret to a roster entry.
func TestImport_NamesLabelsAndAnnotations(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 3, FromDir: dir})
	require.NoError(t, err)

	certPEM, err := os.ReadFile(filepath.Join(dir, FileGossipCert))
	require.NoError(t, err)
	gen, err := certGeneration(KeyTypeGossip, certPEM)
	require.NoError(t, err)

	name := client.onlySecret(t, "ns", "node3-gossip-keys-")
	assert.Equal(t, "node3-gossip-keys-"+gen.Date+"-"+gen.SHA384[:8], name)
	assert.Regexp(t, `^\d{8}$`, gen.Date)

	labels := client.labels["ns/"+name]
	assert.Equal(t, "node3", labels[LabelNode])
	assert.Equal(t, "gossip", labels[LabelKeyType])
	assert.Equal(t, gen.SHA384[:32], labels[LabelFingerprint])
	assert.Len(t, labels[LabelFingerprint], 32)
	ann := client.annotations["ns/"+name]
	assert.Equal(t, gen.SHA384, ann[AnnotationSHA384])
	assert.NotEmpty(t, ann[AnnotationNotAfter])

	// The admin key has no date: name is <base>-<fp8>.
	admin := client.onlySecret(t, "ns", "node3-admin-pubkey-")
	assert.Regexp(t, `^node3-admin-pubkey-[0-9a-f]{8}$`, admin)
	assert.Empty(t, client.annotations["ns/"+admin][AnnotationNotAfter])
}

// A reset cluster reprovisioned from the same folder gets identical names, so the
// date never depends on when the import ran.
func TestImport_NamesAreDeterministic(t *testing.T) {
	dir := writeGeneratedFolder(t)
	names := func() []string {
		c := newFakeSecretClient()
		res, err := Import(context.Background(), c, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir})
		require.NoError(t, err)
		return res.Applied
	}
	assert.Equal(t, names(), names())
}

func TestImport_SuffixReplacesDate(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Suffix: "v0.75.6"})
	require.NoError(t, err)
	assert.Regexp(t, `^node0-gossip-keys-v0\.75\.6-[0-9a-f]{8}$`, client.onlySecret(t, "ns", "node0-gossip-keys-"))

	_, err = Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Suffix: "Bad Suffix"})
	require.Error(t, err)
}

// The operator hex-encodes the admin secret value itself, so the secret must hold
// the raw key bytes, not the hex text from admin.pub.
func TestImport_AdminSecretHoldsRawBytes(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Selectors: Selectors{Admin: true}})
	require.NoError(t, err)

	file, err := os.ReadFile(filepath.Join(dir, FileAdminPublic))
	require.NoError(t, err)
	want, err := hex.DecodeString(strings.TrimSpace(string(file)))
	require.NoError(t, err)

	admin, _, _ := client.GetSecretData(context.Background(), "ns", client.onlySecret(t, "ns", "node0-admin-pubkey-"))
	assert.Equal(t, want, admin[FileAdminPublic])
	assert.Len(t, admin[FileAdminPublic], 32)
}

func TestImport_RejectsBadAdminPublicKey(t *testing.T) {
	for name, content := range map[string]string{"not hex": "zzzz\n", "wrong length": "deadbeef\n"} {
		dir := writeGeneratedFolder(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, FileAdminPublic), []byte(content), 0o644))
		_, err := Import(context.Background(), newFakeSecretClient(), ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Selectors: Selectors{Admin: true}})
		require.Error(t, err, name)
	}
}

func TestExport_AdminRoundTripsToHexFile(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Selectors: Selectors{Admin: true}})
	require.NoError(t, err)

	out := filepath.Join(t.TempDir(), "export")
	_, err = Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out, Selectors: Selectors{Admin: true}})
	require.NoError(t, err)

	orig, err := os.ReadFile(filepath.Join(dir, FileAdminPublic))
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(out, FileAdminPublic))
	require.NoError(t, err)
	assert.Equal(t, string(orig), string(got))
}

func TestExport_RejectsHexTextAdminSecret(t *testing.T) {
	client := newFakeSecretClient()
	require.NoError(t, client.ApplySecret(context.Background(), "ns", "node0-admin-pubkey-abcd1234",
		map[string][]byte{FileAdminPublic: []byte(strings.Repeat("ab", 32))},
		map[string]string{LabelNode: "node0", LabelKeyType: "admin"}, nil))
	_, err := Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: t.TempDir(), Selectors: Selectors{Admin: true}})
	require.Error(t, err)
}

// Different key material gets a different name, so a rotation never overwrites an
// earlier generation, and re-importing the same material is a no-op.
func TestImport_NewMaterialStagesBesideTheOld(t *testing.T) {
	client := newFakeSecretClient()
	sel := Selectors{Gossip: true}
	first := writeGeneratedFolder(t)
	second := writeGeneratedFolder(t)

	r1, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 1, FromDir: first, Selectors: sel})
	require.NoError(t, err)
	again, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 1, FromDir: first, Selectors: sel})
	require.NoError(t, err)
	assert.Empty(t, again.Applied)
	assert.Equal(t, r1.Applied, again.Skipped)

	r2, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 1, FromDir: second, Selectors: sel})
	require.NoError(t, err)
	require.Len(t, r2.Applied, 1)
	assert.NotEqual(t, r1.Applied[0], r2.Applied[0])

	// Both generations remain in the cluster.
	_, ok, _ := client.GetSecretData(context.Background(), "ns", r1.Applied[0])
	assert.True(t, ok, "the earlier generation must be untouched")
}

// A secret whose content was edited under its original name is refused on import
// unless forced.
func TestImport_EditedSecretIsRefused(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	opts := ImportOptions{Namespace: "ns", NodeId: 1, FromDir: dir, Selectors: Selectors{Grpc: true}}
	res, err := Import(context.Background(), client, opts)
	require.NoError(t, err)
	name := res.Applied[0]

	client.store["ns/"+name][FileGrpcKey] = []byte("tampered")
	_, err = Import(context.Background(), client, opts)
	require.Error(t, err)

	opts.Force = true
	_, err = Import(context.Background(), client, opts)
	require.NoError(t, err)
}

func TestImport_MissingFile(t *testing.T) {
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{
		Namespace: "ns", NodeId: 0, FromDir: t.TempDir(), Selectors: Selectors{Gossip: true},
	})
	require.Error(t, err)
}

func TestExport_RoundTrip(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 2, FromDir: dir})
	require.NoError(t, err)

	out := filepath.Join(t.TempDir(), "export")
	res, err := Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 2, OutDir: out})
	require.NoError(t, err)
	assert.Len(t, res.Exported, 3)

	// Files land with the neutral names; gossip private key is 0600.
	assert.FileExists(t, filepath.Join(out, FileGossipPrivate))
	assert.FileExists(t, filepath.Join(out, FileGrpcCert))
	assert.FileExists(t, filepath.Join(out, FileAdminPublic))

	fi, err := os.Stat(filepath.Join(out, FileGossipPrivate))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestExport_Idempotent(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	_, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir})
	require.NoError(t, err)

	out := filepath.Join(t.TempDir(), "export")
	first, err := Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out})
	require.NoError(t, err)
	assert.Len(t, first.Exported, 3)

	// Re-export is a no-op: nothing written, all skipped.
	second, err := Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out})
	require.NoError(t, err)
	assert.Empty(t, second.Exported)
	assert.Len(t, second.Skipped, 3)

	// A file with different content is refused without force...
	require.NoError(t, os.WriteFile(filepath.Join(out, FileAdminPublic), []byte("deadbeef\n"), 0o644))
	_, err = Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out, Selectors: Selectors{Admin: true}})
	require.Error(t, err)

	// ...and overwritten with force.
	_, err = Export(context.Background(), client, ExportOptions{Namespace: "ns", NodeId: 0, OutDir: out, Selectors: Selectors{Admin: true}, Force: true})
	require.NoError(t, err)
}

func TestExport_MissingSecret(t *testing.T) {
	client := newFakeSecretClient()
	_, err := Export(context.Background(), client, ExportOptions{
		Namespace: "ns", NodeId: 0, OutDir: t.TempDir(), Selectors: Selectors{Admin: true},
	})
	require.Error(t, err)
}

func importTwoGenerations(t *testing.T) (*fakeSecretClient, []string) {
	t.Helper()
	client := newFakeSecretClient()
	var names []string
	for i := 0; i < 2; i++ {
		res, err := Import(context.Background(), client, ImportOptions{
			Namespace: "ns", NodeId: 0, FromDir: writeGeneratedFolder(t), Selectors: Selectors{Gossip: true},
		})
		require.NoError(t, err)
		names = append(names, res.Applied...)
	}
	return client, names
}

func TestResolveSecret_SingleGenerationNeedsNoOptions(t *testing.T) {
	client := newFakeSecretClient()
	res, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: writeGeneratedFolder(t), Selectors: Selectors{Gossip: true}})
	require.NoError(t, err)

	got, err := ResolveSecret(context.Background(), client, "ns", 0, KeyTypeGossip, ResolveOptions{})
	require.NoError(t, err)
	assert.Equal(t, res.Applied[0], got.Name)
}

func TestResolveSecret_SeveralGenerationsNeedAChoice(t *testing.T) {
	client, names := importTwoGenerations(t)

	_, err := ResolveSecret(context.Background(), client, "ns", 0, KeyTypeGossip, ResolveOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), names[0])
	assert.Contains(t, err.Error(), names[1])

	fp := strings.TrimPrefix(client.labels["ns/"+names[1]][LabelFingerprint], "")
	got, err := ResolveSecret(context.Background(), client, "ns", 0, KeyTypeGossip, ResolveOptions{Fingerprint: fp[:8]})
	require.NoError(t, err)
	assert.Equal(t, names[1], got.Name)

	_, err = ResolveSecret(context.Background(), client, "ns", 0, KeyTypeGossip, ResolveOptions{Fingerprint: "zz"})
	require.Error(t, err)
	_, err = ResolveSecret(context.Background(), client, "ns", 0, KeyTypeGossip, ResolveOptions{Fingerprint: "ffffffff"})
	require.Error(t, err)
}

func TestResolveSecret_NothingFound(t *testing.T) {
	_, err := ResolveSecret(context.Background(), newFakeSecretClient(), "ns", 0, KeyTypeGrpc, ResolveOptions{})
	require.Error(t, err)
}

// A secret renamed or edited so that its name and label no longer match its key
// must be refused, since this is what would reach the pod.
func TestResolveSecret_RefusesNameThatDoesNotMatchContent(t *testing.T) {
	client := newFakeSecretClient()
	a, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: writeGeneratedFolder(t), Selectors: Selectors{Gossip: true}})
	require.NoError(t, err)
	b := writeGeneratedFolder(t)

	// Put generation B's material under generation A's name and labels.
	other, err := readFiles(b, FileGossipPrivate, FileGossipCert)
	require.NoError(t, err)
	client.store["ns/"+a.Applied[0]] = other

	_, err = ResolveSecret(context.Background(), client, "ns", 0, KeyTypeGossip, ResolveOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match its fingerprint")
}

// Extra labels (for example a reference to an external system) go on every
// secret. Re-importing identical content only adds or changes labels; the data is
// never sent again.
func TestImport_ExtraLabelsAreAddedWithoutRewritingData(t *testing.T) {
	dir := writeGeneratedFolder(t)
	client := newFakeSecretClient()
	opts := ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Selectors: Selectors{Gossip: true}}

	res, err := Import(context.Background(), client, opts)
	require.NoError(t, err)
	name := res.Applied[0]
	callsAfterFirst := client.applyCalls

	opts.Labels = map[string]string{"example.com/external-ref": "ticket-42"}
	res, err = Import(context.Background(), client, opts)
	require.NoError(t, err)
	assert.Equal(t, []string{name}, res.Relabeled)
	assert.Empty(t, res.Applied)
	assert.Equal(t, callsAfterFirst, client.applyCalls, "the secret data must not be re-applied")
	assert.Equal(t, "ticket-42", client.labels["ns/"+name]["example.com/external-ref"])
	assert.NotEmpty(t, client.labels["ns/"+name][LabelFingerprint], "weaver labels are kept")

	// Same labels again is a no-op; a changed value updates only the label.
	res, err = Import(context.Background(), client, opts)
	require.NoError(t, err)
	assert.Equal(t, []string{name}, res.Skipped)

	opts.Labels["example.com/external-ref"] = "ticket-43"
	res, err = Import(context.Background(), client, opts)
	require.NoError(t, err)
	assert.Equal(t, []string{name}, res.Relabeled)
	assert.Equal(t, "ticket-43", client.labels["ns/"+name]["example.com/external-ref"])
	assert.Equal(t, callsAfterFirst, client.applyCalls)
}

func TestImport_RejectsBadOrReservedLabels(t *testing.T) {
	dir := writeGeneratedFolder(t)
	for name, labels := range map[string]map[string]string{
		"reserved key":   {LabelFingerprint: "x"},
		"bad key":        {"bad key!": "v"},
		"bad value":      {"example.com/ref": "has spaces"},
		"value too long": {"example.com/ref": strings.Repeat("a", 64)},
	} {
		_, err := Import(context.Background(), newFakeSecretClient(), ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Labels: labels})
		require.Error(t, err, name)
	}
}

func TestImport_DefaultSkipsAdminWhenNoAdminPub(t *testing.T) {
	dir := writeGeneratedFolder(t)
	require.NoError(t, os.Remove(filepath.Join(dir, FileAdminPublic)))
	client := newFakeSecretClient()

	res, err := Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir})
	require.NoError(t, err)
	assert.Len(t, res.Applied, 2)
	for name := range client.store {
		assert.NotContains(t, name, "node0-admin-pubkey-")
	}
}

func TestImport_ExplicitAdminRequiresAdminPub(t *testing.T) {
	dir := writeGeneratedFolder(t)
	require.NoError(t, os.Remove(filepath.Join(dir, FileAdminPublic)))

	_, err := Import(context.Background(), newFakeSecretClient(), ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Selectors: Selectors{Admin: true}})
	require.Error(t, err)
}
