// SPDX-License-Identifier: Apache-2.0

package steps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/hashgraph/solo-weaver/pkg/models"
)

func Test_renderESOSecretManifest(t *testing.T) {
	opts := ESOSecretOptions{
		Name:            "grafana-alloy-secrets",
		Namespace:       "grafana-alloy",
		StoreName:       "vault-store",
		RefreshInterval: "30m",
		Data: []models.ESOSecretDataEntry{
			{SecretKey: "PROMETHEUS_PASSWORD_PRIMARY", RemoteKey: "secret/data/prom/primary", Property: "password"},
			{SecretKey: "LOKI_PASSWORD_PRIMARY", RemoteKey: "secret/data/loki/primary"},
		},
	}

	rendered, err := renderESOSecretManifest(opts)
	require.NoError(t, err)

	// Must be valid YAML and carry the resolved fields.
	var doc map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(rendered), &doc), "rendered manifest must be valid YAML")

	assert.Equal(t, "external-secrets.io/v1", doc["apiVersion"])
	assert.Equal(t, "ExternalSecret", doc["kind"])

	meta := doc["metadata"].(map[string]interface{})
	assert.Equal(t, "grafana-alloy-secrets", meta["name"])
	assert.Equal(t, "grafana-alloy", meta["namespace"])

	spec := doc["spec"].(map[string]interface{})
	assert.Equal(t, "30m", spec["refreshInterval"])
	assert.Equal(t, "vault-store", spec["secretStoreRef"].(map[string]interface{})["name"])
	assert.Equal(t, "ClusterSecretStore", spec["secretStoreRef"].(map[string]interface{})["kind"])
	assert.Equal(t, "grafana-alloy-secrets", spec["target"].(map[string]interface{})["name"])

	data := spec["data"].([]interface{})
	require.Len(t, data, 2)

	first := data[0].(map[string]interface{})
	assert.Equal(t, "PROMETHEUS_PASSWORD_PRIMARY", first["secretKey"])
	firstRef := first["remoteRef"].(map[string]interface{})
	assert.Equal(t, "secret/data/prom/primary", firstRef["key"])
	assert.Equal(t, "password", firstRef["property"])

	second := data[1].(map[string]interface{})
	assert.Equal(t, "LOKI_PASSWORD_PRIMARY", second["secretKey"])
	secondRef := second["remoteRef"].(map[string]interface{})
	assert.Equal(t, "secret/data/loki/primary", secondRef["key"])
	_, hasProperty := secondRef["property"]
	assert.False(t, hasProperty, "entry without #field must omit property")
}

func Test_renderESOSecretManifest_QuotedScalars(t *testing.T) {
	// Values that plain YAML scalars would mangle (parse as int/bool, or reject
	// outright for a leading @) must survive as strings via the quoted template.
	opts := ESOSecretOptions{
		Name:            "123",
		Namespace:       "default",
		StoreName:       "vault-store",
		RefreshInterval: "0",
		Data: []models.ESOSecretDataEntry{
			{SecretKey: "FOO", RemoteKey: "@env/1234", Property: "0"},
		},
	}

	rendered, err := renderESOSecretManifest(opts)
	require.NoError(t, err)

	var doc map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(rendered), &doc), "rendered manifest must be valid YAML")

	meta := doc["metadata"].(map[string]interface{})
	assert.Equal(t, "123", meta["name"], "numeric-looking name must stay a string")

	spec := doc["spec"].(map[string]interface{})
	assert.Equal(t, "0", spec["refreshInterval"], "zero interval must stay a string")

	entry := spec["data"].([]interface{})[0].(map[string]interface{})
	ref := entry["remoteRef"].(map[string]interface{})
	assert.Equal(t, "@env/1234", ref["key"], "leading @ and digits must stay a string")
	assert.Equal(t, "0", ref["property"], "numeric property must stay a string")
}
