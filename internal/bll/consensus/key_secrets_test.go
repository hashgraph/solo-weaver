// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	cnkeys "github.com/hashgraph/solo-weaver/internal/consensus/keys"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeKeyClient is an in-memory keyCluster. capsule maps a capsule spec field
// (e.g. "signingPrivateKey") to the secret name the live capsule references.
type fakeKeyClient struct {
	data    map[string]map[string][]byte
	labels  map[string]map[string]string
	capsule map[string]string
}

func (f *fakeKeyClient) GetResourceNestedString(_ context.Context, _, _, _, _ string, fields ...string) (string, error) {
	// fields: spec, secrets, secretResources, <field>, secretName
	return f.capsule[fields[3]], nil
}

func newFakeKeyClient() *fakeKeyClient {
	return &fakeKeyClient{data: map[string]map[string][]byte{}, labels: map[string]map[string]string{}, capsule: map[string]string{}}
}

func (f *fakeKeyClient) ApplySecret(_ context.Context, ns, name string, data map[string][]byte, labels, _ map[string]string) error {
	f.data[ns+"/"+name] = data
	f.labels[ns+"/"+name] = labels
	return nil
}

func (f *fakeKeyClient) GetSecretData(_ context.Context, ns, name string) (map[string][]byte, bool, error) {
	d, ok := f.data[ns+"/"+name]
	return d, ok, nil
}

func (f *fakeKeyClient) GetSecretInfo(_ context.Context, ns, name string) (*kube.SecretInfo, bool, error) {
	if _, ok := f.data[ns+"/"+name]; !ok {
		return nil, false, nil
	}
	return &kube.SecretInfo{Name: name, Labels: f.labels[ns+"/"+name]}, true, nil
}

func (f *fakeKeyClient) PatchSecretLabels(_ context.Context, ns, name string, labels map[string]string) error {
	merged := map[string]string{}
	for k, v := range f.labels[ns+"/"+name] {
		merged[k] = v
	}
	for k, v := range labels {
		merged[k] = v
	}
	f.labels[ns+"/"+name] = merged
	return nil
}

func (f *fakeKeyClient) ListSecrets(_ context.Context, ns, selector string) ([]kube.SecretInfo, error) {
	var out []kube.SecretInfo
	for k := range f.data {
		if !strings.HasPrefix(k, ns+"/") {
			continue
		}
		match := true
		for _, term := range strings.Split(selector, ",") {
			if kv := strings.SplitN(term, "=", 2); len(kv) == 2 && f.labels[k][kv[0]] != kv[1] {
				match = false
			}
		}
		if match {
			out = append(out, kube.SecretInfo{Name: strings.TrimPrefix(k, ns+"/"), Labels: f.labels[k]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func genAndImport(t *testing.T, client *fakeKeyClient, nodeID int64, sel cnkeys.Selectors) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "keys")
	set, err := cnkeys.Generate(cnkeys.GenerateOptions{MachineID: "m"})
	require.NoError(t, err)
	require.NoError(t, set.WriteToDir(dir))
	require.NoError(t, os.Chmod(dir, 0o700))
	_, err = cnkeys.Import(context.Background(), client, cnkeys.ImportOptions{Namespace: "ns", NodeId: nodeID, FromDir: dir, Selectors: sel})
	require.NoError(t, err)
}

func TestResolveKeySecrets_FillsUnnamedSecrets(t *testing.T) {
	client := newFakeKeyClient()
	genAndImport(t, client, 0, cnkeys.Selectors{})
	genAndImport(t, client, 1, cnkeys.Selectors{}) // another node in the same namespace

	custom := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0}
	require.NoError(t, resolveKeySecrets(context.Background(), client, custom))

	assert.Regexp(t, `^node0-gossip-keys-\d{8}-[0-9a-f]{8}$`, custom.SigningSecret)
	assert.Regexp(t, `^node0-grpc-tls-keys-\d{8}-[0-9a-f]{8}$`, custom.GrpcTlsSecret)
	assert.Regexp(t, `^node0-admin-pubkey-[0-9a-f]{8}$`, custom.AdminKeySecret)
}

func TestResolveKeySecrets_KeepsNamedSecrets(t *testing.T) {
	custom := &models.ConsensusNodeInputs{
		Namespace: "ns", NodeId: 0,
		SigningSecret: "mine-gossip", GrpcTlsSecret: "mine-grpc", AdminKeySecret: "mine-admin",
	}
	require.NoError(t, resolveKeySecrets(context.Background(), newFakeKeyClient(), custom))
	assert.Equal(t, "mine-gossip", custom.SigningSecret)
	assert.Equal(t, "mine-grpc", custom.GrpcTlsSecret)
	assert.Equal(t, "mine-admin", custom.AdminKeySecret)
}

func TestResolveKeySecrets_MissingSigningOrGrpcFails(t *testing.T) {
	custom := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0}
	require.Error(t, resolveKeySecrets(context.Background(), newFakeKeyClient(), custom))
}

func TestResolveKeySecrets_AdminKeyIsOptional(t *testing.T) {
	client := newFakeKeyClient()
	genAndImport(t, client, 0, cnkeys.Selectors{Gossip: true, Grpc: true})

	custom := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0}
	require.NoError(t, resolveKeySecrets(context.Background(), client, custom))
	assert.NotEmpty(t, custom.SigningSecret)
	assert.Empty(t, custom.AdminKeySecret)
}

// With several generations of a required key type, the install stops and names
// them. Several admin generations are skipped: the capsule just gets no admin
// reference, unless one is named.
func TestResolveKeySecrets_SeveralGenerations(t *testing.T) {
	client := newFakeKeyClient()
	genAndImport(t, client, 0, cnkeys.Selectors{})
	genAndImport(t, client, 0, cnkeys.Selectors{Admin: true}) // second admin generation only

	custom := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0}
	require.NoError(t, resolveKeySecrets(context.Background(), client, custom))
	assert.NotEmpty(t, custom.SigningSecret)
	assert.Empty(t, custom.AdminKeySecret, "an ambiguous admin key must be skipped")

	custom = &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0, AdminKeySecret: "chosen"}
	require.NoError(t, resolveKeySecrets(context.Background(), client, custom))
	assert.Equal(t, "chosen", custom.AdminKeySecret)

	// Two signing generations are not skipped.
	genAndImport(t, client, 0, cnkeys.Selectors{Gossip: true})
	custom = &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0}
	err := resolveKeySecrets(context.Background(), client, custom)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key secrets found")
}

// A reinstall keeps the secrets the live capsule references, even when a second
// generation is staged beside them.
func TestResolveKeySecrets_KeepsWhatTheLiveCapsuleUses(t *testing.T) {
	client := newFakeKeyClient()
	genAndImport(t, client, 0, cnkeys.Selectors{})
	genAndImport(t, client, 0, cnkeys.Selectors{}) // staged rotation: two generations now exist
	client.capsule = map[string]string{
		"signingPrivateKey": "node0-gossip-keys-live",
		"grpcTlsPrivateKey": "node0-grpc-tls-keys-live",
		"adminPublicKey":    "node0-admin-pubkey-live",
	}

	custom := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0}
	require.NoError(t, resolveKeySecrets(context.Background(), client, custom))
	assert.Equal(t, "node0-gossip-keys-live", custom.SigningSecret)
	assert.Equal(t, "node0-grpc-tls-keys-live", custom.GrpcTlsSecret)
	assert.Equal(t, "node0-admin-pubkey-live", custom.AdminKeySecret)
}

// A capsule with no admin reference falls back to the labelled admin secret.
func TestResolveKeySecrets_CapsuleWithoutAdminFallsBackToLabels(t *testing.T) {
	client := newFakeKeyClient()
	genAndImport(t, client, 0, cnkeys.Selectors{})
	client.capsule = map[string]string{
		"signingPrivateKey": "node0-gossip-keys-live",
		"grpcTlsPrivateKey": "node0-grpc-tls-keys-live",
	}

	custom := &models.ConsensusNodeInputs{Namespace: "ns", OrbitName: "ns", NodeId: 0}
	require.NoError(t, resolveKeySecrets(context.Background(), client, custom))
	assert.Equal(t, "node0-gossip-keys-live", custom.SigningSecret)
	assert.Regexp(t, `^node0-admin-pubkey-[0-9a-f]{8}$`, custom.AdminKeySecret)
}
