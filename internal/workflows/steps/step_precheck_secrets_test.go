// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"context"
	"errors"
	"testing"

	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// fakeSecretClient holds Secrets as name -> entry -> value. readErr fails every
// data read.
type fakeSecretClient struct {
	secrets map[string]map[string]string
	readErr error
}

func (f *fakeSecretClient) ResourceExists(_ context.Context, _, _, _, name string) (bool, error) {
	_, ok := f.secrets[name]
	return ok, nil
}

func (f *fakeSecretClient) GetResourceNestedString(_ context.Context, _, _, _, name string, fields ...string) (string, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	if len(fields) != 2 || fields[0] != "data" {
		return "", nil
	}
	return f.secrets[name][fields[1]], nil
}

func (f *fakeSecretClient) ApplyTyped(context.Context, runtime.Object) error { return nil }

func (f *fakeSecretClient) List(context.Context, kube.ResourceKind, string, kube.WaitOptions) (*unstructured.UnstructuredList, error) {
	return &unstructured.UnstructuredList{}, nil
}

func (f *fakeSecretClient) with(secret string, entries ...string) *fakeSecretClient {
	if f.secrets == nil {
		f.secrets = map[string]map[string]string{}
	}
	data := map[string]string{}
	for _, e := range entries {
		data[e] = "eA=="
	}
	f.secrets[secret] = data
	return f
}

func runSecretPrecheck(t *testing.T, fake *fakeSecretClient) *automa.Report {
	t.Helper()
	provider := func(context.Context) (CapsuleKubeClient, error) { return fake, nil }
	step, err := PrecheckConsensusSecrets(secretInputs(), provider).Build()
	require.NoError(t, err)
	return step.Execute(context.Background())
}

func TestPrecheckConsensusSecrets_PassesWithExpectedEntries(t *testing.T) {
	in := secretInputs()
	fake := (&fakeSecretClient{}).
		with(in.SigningSecret, "s-private.pem", "s-public.pem").
		with(in.GrpcTlsSecret, "hedera.key", "hedera.crt")
	report := runSecretPrecheck(t, fake)
	require.Equal(t, automa.StatusSuccess, report.Status, report.Error)
}

// Secrets made before `consensus keys import` use other entry names; the
// capsule would reference entries that do not exist, so preflight must stop.
func TestPrecheckConsensusSecrets_FailsOnMissingEntries(t *testing.T) {
	in := secretInputs()
	fake := (&fakeSecretClient{}).
		with(in.SigningSecret, "private.pem", "public.pem").
		with(in.GrpcTlsSecret, "hedera.key", "hedera.crt")
	report := runSecretPrecheck(t, fake)
	assert.NotEqual(t, automa.StatusSuccess, report.Status)
	require.Error(t, report.Error)
	assert.Contains(t, report.Error.Error(), in.SigningSecret)
	assert.Contains(t, report.Error.Error(), "s-private.pem, s-public.pem")
}

func TestPrecheckConsensusSecrets_FailsWhenSecretMissing(t *testing.T) {
	in := secretInputs()
	fake := (&fakeSecretClient{}).with(in.SigningSecret, "s-private.pem", "s-public.pem")
	report := runSecretPrecheck(t, fake)
	assert.NotEqual(t, automa.StatusSuccess, report.Status)
	require.Error(t, report.Error)
	assert.Contains(t, report.Error.Error(), "not found")
}

// A read failure is a cluster problem, not a missing entry.
func TestPrecheckConsensusSecrets_ReadErrorIsNotMissingEntries(t *testing.T) {
	in := secretInputs()
	fake := (&fakeSecretClient{readErr: errors.New("forbidden")}).
		with(in.SigningSecret, "s-private.pem", "s-public.pem").
		with(in.GrpcTlsSecret, "hedera.key", "hedera.crt")
	report := runSecretPrecheck(t, fake)
	assert.NotEqual(t, automa.StatusSuccess, report.Status)
	require.Error(t, report.Error)
	assert.Contains(t, report.Error.Error(), "failed to read Secret")
	assert.NotContains(t, report.Error.Error(), "missing entries")
}
