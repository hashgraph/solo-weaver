// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleSecretsDir holds the Kubernetes Secret manifests used by manual and UAT
// runs. These tests check that weaver's own key code works on those real,
// externally produced certificates and keys, not only on keys it generated.
const sampleSecretsDir = "../../../test/sample-secrets"

// loadSampleNode reads test/sample-secrets/node<N>.yaml and returns its secrets
// by name.
func loadSampleNode(t *testing.T, file string) map[string]map[string][]byte {
	t.Helper()
	raw, err := os.ReadFile(file)
	require.NoError(t, err)

	secrets := map[string]map[string][]byte{}
	dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(raw), 4096)
	for {
		var s corev1.Secret
		if err := dec.Decode(&s); err == io.EOF {
			break
		} else {
			require.NoError(t, err, file)
		}
		if s.Name != "" {
			secrets[s.Name] = s.Data
		}
	}
	return secrets
}

func sampleFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(sampleSecretsDir, "node*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no sample secrets found in %s", sampleSecretsDir)
	return files
}

// The sample secrets must use the neutral inner-key names the capsule step
// references, so a node installed from them is wired the same as one installed
// from `keys import`.
func TestSampleSecrets_UseNeutralKeyNames(t *testing.T) {
	for _, file := range sampleFiles(t) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			secrets := loadSampleNode(t, file)

			var gossip, grpc map[string][]byte
			for name, data := range secrets {
				switch {
				case strings.HasSuffix(name, gossipSecretSuffix):
					gossip = data
				case strings.HasSuffix(name, grpcSecretSuffix):
					grpc = data
				}
			}
			require.NotNil(t, gossip, "gossip secret missing")
			require.NotNil(t, grpc, "gRPC secret missing")
			assert.Contains(t, gossip, FileGossipPrivate)
			assert.Contains(t, gossip, FileGossipCert)
			assert.Contains(t, grpc, FileGrpcKey)
			assert.Contains(t, grpc, FileGrpcCert)
		})
	}
}

// Weaver's parse, inspect, and import code must work on the sample material.
func TestSampleSecrets_WorkWithWeaverKeyCode(t *testing.T) {
	for _, file := range sampleFiles(t) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			secrets := loadSampleNode(t, file)

			dir := t.TempDir()
			for _, data := range secrets {
				for key, value := range data {
					switch key {
					case FileGossipPrivate, FileGrpcKey:
						require.NoError(t, os.WriteFile(filepath.Join(dir, key), value, privatePerm))
					case FileGossipCert, FileGrpcCert:
						require.NoError(t, os.WriteFile(filepath.Join(dir, key), value, publicPerm))
					}
				}
			}
			sel := Selectors{Gossip: true, Grpc: true}

			// inspect reads the certificates.
			ins, err := InspectDir(dir, sel)
			require.NoError(t, err)
			assert.False(t, ins.HasErrors(), "%+v", ins.Keys)
			require.Len(t, ins.Keys, 2)
			for _, k := range ins.Keys {
				require.NotNil(t, k.Certificate, k.File)
				assert.Equal(t, "RSA", k.Certificate.PublicKeyAlgorithm, k.File)
			}

			// verify parses the private keys and checks each matches its cert.
			ver, err := VerifyDir(dir, sel)
			require.NoError(t, err)
			for _, f := range ver.Findings {
				assert.True(t, f.OK, "%s: %s", f.Key, f.Detail)
			}

			// import accepts the folder and creates the secrets.
			client := newFakeSecretClient()
			_, err = Import(context.Background(), client, ImportOptions{Namespace: "ns", NodeId: 0, FromDir: dir, Selectors: sel})
			require.NoError(t, err)
		})
	}
}
