// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"os"
	"strings"
	"testing"

	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActivate_SetsEnvVars(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE"} {
		t.Setenv(key, "")
	}

	cfg := models.ProxyConfig{
		Enabled:                true,
		URL:                    "127.0.0.1:3128",
		NoProxy:                "localhost,127.0.0.1",
		SSLCertFile:            "/etc/ssl/certs/ca-certificates.crt",
		ContainerRegistryProxy: "localhost:5050",
	}
	err := Activate(cfg)
	require.NoError(t, err)

	assert.Equal(t, "http://127.0.0.1:3128", os.Getenv("HTTP_PROXY"))
	assert.Equal(t, "http://127.0.0.1:3128", os.Getenv("HTTPS_PROXY"))
	assert.Equal(t, "localhost,127.0.0.1", os.Getenv("NO_PROXY"))
	assert.Equal(t, "/etc/ssl/certs/ca-certificates.crt", os.Getenv("SSL_CERT_FILE"))
	assert.True(t, config.IsProxyEnabled())
}

func TestActivate_DefaultNoProxy(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE"} {
		t.Setenv(key, "")
	}

	cfg := models.ProxyConfig{
		URL: "127.0.0.1:3128",
	}
	err := Activate(cfg)
	require.NoError(t, err)

	assert.Equal(t, models.DefaultNoProxy, os.Getenv("NO_PROXY"))
}

func TestActivate_PartialConfig(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE"} {
		t.Setenv(key, "")
	}

	cfg := models.ProxyConfig{
		URL: "myproxy:8080",
	}
	err := Activate(cfg)
	require.NoError(t, err)

	assert.Equal(t, "http://myproxy:8080", os.Getenv("HTTP_PROXY"))
	assert.Equal(t, "http://myproxy:8080", os.Getenv("HTTPS_PROXY"))
	assert.True(t, config.IsProxyEnabled())
}

func TestActivate_EmptyURL(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE"} {
		t.Setenv(key, "")
	}

	cfg := models.ProxyConfig{}
	err := Activate(cfg)
	require.NoError(t, err)

	assert.Equal(t, "", os.Getenv("HTTP_PROXY"))
	assert.Equal(t, "", os.Getenv("HTTPS_PROXY"))
	assert.True(t, config.IsProxyEnabled())
}

// CRI-O enforces unambiguous short image names, so the proxy's registries.conf
// must list a single unqualified search registry or short names fail to pull.
func TestRegistriesConf_OneUnqualifiedSearchRegistry(t *testing.T) {
	content, err := templates.Render("files/crio/registries.conf", struct{ ContainerRegistryProxy string }{"localhost:5050"})
	require.NoError(t, err)

	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "unqualified-search-registries") {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	assert.Equal(t, []string{`unqualified-search-registries = ["docker.io"]`}, lines)
	assert.Contains(t, content, `location = "localhost:5050"`)
}
