// SPDX-License-Identifier: Apache-2.0

package proxy

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/templates"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// Activate enables proxy mode by setting environment variables, updating the
// global config, and installing the CRI-O container registry proxy configuration.
func Activate(cfg models.ProxyConfig) error {
	if err := cfg.Validate(); err != nil {
		return errorx.IllegalArgument.Wrap(err, "invalid proxy configuration")
	}

	// Apply default NoProxy if not specified
	noProxy := cfg.NoProxy
	if noProxy == "" {
		noProxy = models.DefaultNoProxy
	}

	logx.As().Info().
		Str("url", redactURL(cfg.URL)).
		Str("sslCertFile", cfg.SSLCertFile).
		Str("containerRegistryProxy", cfg.ContainerRegistryProxy).
		Msg("Activating proxy configuration")

	// Set environment variables so http.ProxyFromEnvironment picks them up
	if cfg.URL != "" {
		proxyURL := fmt.Sprintf("http://%s", cfg.URL)
		if err := os.Setenv("HTTP_PROXY", proxyURL); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set HTTP_PROXY")
		}
		if err := os.Setenv("HTTPS_PROXY", proxyURL); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set HTTPS_PROXY")
		}
	}
	if err := os.Setenv("NO_PROXY", noProxy); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to set NO_PROXY")
	}
	if cfg.SSLCertFile != "" {
		if err := os.Setenv("SSL_CERT_FILE", cfg.SSLCertFile); err != nil {
			return errorx.IllegalState.Wrap(err, "failed to set SSL_CERT_FILE")
		}
	}

	// Store in global config so downstream code can query proxy state
	cfg.Enabled = true
	config.SetProxy(cfg)

	// Install CRI-O registries.conf for container registry proxy support
	if cfg.ContainerRegistryProxy != "" {
		if err := InstallRegistriesConf(cfg.ContainerRegistryProxy); err != nil {
			logx.As().Warn().Err(err).Msg("Failed to install CRI-O registries.conf (non-fatal)")
		}
	}

	return nil
}

// InstallRegistriesConf installs the custom registries.conf with registry mirror configuration.
// The containerRegistryProxy parameter specifies the host:port of the pull-through cache.
func InstallRegistriesConf(containerRegistryProxy string) error {
	data := struct {
		ContainerRegistryProxy string
	}{
		ContainerRegistryProxy: containerRegistryProxy,
	}

	content, err := templates.Render("files/crio/registries.conf", data)
	if err != nil {
		return errorx.IllegalState.Wrap(err, "failed to render registries.conf template")
	}

	registriesConfPath := filepath.Join(models.Paths().SandboxDir, "etc", "containers", "registries.conf.d", "registries.conf")

	if err := os.MkdirAll(filepath.Dir(registriesConfPath), models.DefaultDirOrExecPerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to create registries.conf.d directory")
	}

	if err := os.WriteFile(registriesConfPath, []byte(content), models.DefaultFilePerm); err != nil {
		return errorx.IllegalState.Wrap(err, "failed to write custom registries.conf")
	}

	return nil
}

// redactURL strips any userinfo (credentials) from a proxy URL or host:port,
// returning only scheme + host:port for safe logging.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse("http://" + raw)
	if err != nil {
		return "<invalid>"
	}
	parsed.User = nil
	return parsed.Host
}
