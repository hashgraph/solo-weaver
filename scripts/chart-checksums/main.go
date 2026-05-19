// SPDX-License-Identifier: Apache-2.0

// chart-checksums recomputes the integrity values stored in
// pkg/software/infrastructure-catalog.yaml under cluster:*.versions.
//
// For classic charts it prints the SHA256 of the chart .tgz fetched with
// `helm pull`. For OCI charts it prints the OCI manifest digest reported
// by `helm pull`.
//
// The chart list and version list are read from the catalog itself, so
// every version recorded under each cluster: entry is regenerated on
// every run. To add a chart or bump a version, edit the catalog and
// re-run this program.
//
// Invoke via the Taskfile:
//
//	task chart-checksums
//
// Requires `helm` on PATH.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/software"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("'helm' is required on PATH: %w", err)
	}

	catalog, err := software.LoadInfrastructureCatalog()
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}

	workdir, err := os.MkdirTemp("", "chart-checksums-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workdir)

	h, err := newHelmRunner(workdir)
	if err != nil {
		return err
	}

	if err := h.configureClassicRepos(catalog.Cluster); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("=== Classic charts (SHA256 of .tgz) ===")
	for _, chart := range catalog.Cluster {
		if chart.Type != software.ChartTypeClassic {
			continue
		}
		for version := range chart.Versions {
			digest, err := h.classicDigest(chart.Chart, string(version))
			if err != nil {
				return fmt.Errorf("classic %s %s: %w", chart.Name, version, err)
			}
			printRow(chart.Name, string(version), digest)
		}
	}

	fmt.Println()
	fmt.Println("=== OCI charts (manifest digest) ===")
	for _, chart := range catalog.Cluster {
		if chart.Type != software.ChartTypeOCI {
			continue
		}
		for version := range chart.Versions {
			digest, err := h.ociDigest(chart.Chart, string(version))
			if err != nil {
				return fmt.Errorf("oci %s %s: %w", chart.Name, version, err)
			}
			printRow(chart.Name, string(version), digest)
		}
	}

	return nil
}

// helmRunner wraps a workdir-scoped helm environment. Setting
// HELM_CONFIG_HOME / HELM_CACHE_HOME / HELM_DATA_HOME under workdir means
// every `helm repo add` and `helm pull` mutates only the temporary tree
// — the developer's global repositories.yaml (typically under
// ~/Library/Preferences/helm on macOS or ~/.config/helm on Linux) is
// untouched, so we never overwrite an alias they already use locally and
// nothing is left behind when workdir is removed.
type helmRunner struct {
	workdir string
	env     []string
}

func newHelmRunner(workdir string) (*helmRunner, error) {
	configHome := filepath.Join(workdir, "helm-config")
	cacheHome := filepath.Join(workdir, "helm-cache")
	dataHome := filepath.Join(workdir, "helm-data")
	for _, d := range []string{configHome, cacheHome, dataHome} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, err
		}
	}
	env := append(os.Environ(),
		"HELM_CONFIG_HOME="+configHome,
		"HELM_CACHE_HOME="+cacheHome,
		"HELM_DATA_HOME="+dataHome,
		"HELM_REPOSITORY_CONFIG="+filepath.Join(configHome, "repositories.yaml"),
		"HELM_REPOSITORY_CACHE="+filepath.Join(cacheHome, "repository"),
	)
	return &helmRunner{workdir: workdir, env: env}, nil
}

func (h *helmRunner) command(args ...string) *exec.Cmd {
	cmd := exec.Command("helm", args...)
	cmd.Dir = h.workdir
	cmd.Env = h.env
	return cmd
}

// runChecked invokes helm with the workdir-scoped environment, captures
// combined output, and returns any non-zero exit as an error that also
// includes the captured output. Errors from helm — bad alias, network
// failures, signature mismatches — are surfaced rather than silently
// swallowed.
func (h *helmRunner) runChecked(args ...string) error {
	cmd := h.command(args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("helm %s: %w\n%s",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// configureClassicRepos invokes `helm repo add --force-update` for every
// distinct (alias, repo URL) pair declared under cluster: entries of type
// classic, then runs `helm repo update`. Two catalog entries that share
// an alias but disagree on the repo URL are reported as a catalog bug
// before any network call. `--force-update` is defensive: the sandboxed
// config starts empty but this keeps the helper correct even if a future
// caller points HELM_REPOSITORY_CONFIG at a pre-populated file.
func (h *helmRunner) configureClassicRepos(charts []software.ChartMetadata) error {
	fmt.Fprintln(os.Stderr, "Configuring classic Helm repositories (scoped to a temp dir)...")
	aliasToRepo := map[string]string{}
	for _, c := range charts {
		if c.Type != software.ChartTypeClassic {
			continue
		}
		alias, _, _ := strings.Cut(c.Chart, "/")
		if existing, ok := aliasToRepo[alias]; ok {
			if existing != c.Repo {
				return fmt.Errorf(
					"catalog conflict: cluster alias %q is declared with both %q and %q "+
						"— rename one of the charts so each alias resolves to a single repo URL",
					alias, existing, c.Repo)
			}
			continue
		}
		aliasToRepo[alias] = c.Repo
		if err := h.runChecked("repo", "add", "--force-update", alias, c.Repo); err != nil {
			return err
		}
	}
	if err := h.runChecked("repo", "update"); err != nil {
		return err
	}
	return nil
}

// classicDigest pulls a classic chart and returns the SHA256 of the
// resulting .tgz archive. Helm names the archive after the last path
// segment of the chart reference (e.g. `helm pull foo/bar/baz`
// produces `baz-<version>.tgz`), so we use path.Base — not the first
// `strings.Cut` segment — to derive the filename. `path.Base` is
// preferable to `filepath.Base` here because chart references use
// forward slashes regardless of OS.
func (h *helmRunner) classicDigest(chartRef, version string) (string, error) {
	if err := h.runChecked("pull", chartRef, "--version", version); err != nil {
		return "", err
	}
	tgz := filepath.Join(h.workdir, fmt.Sprintf("%s-%s.tgz", path.Base(chartRef), version))
	return sha256File(tgz)
}

var ociDigestRE = regexp.MustCompile(`Digest:\s*sha256:([a-f0-9]{64})`)

// ociDigest pulls an OCI chart and returns the manifest digest reported
// by `helm pull` on stdout.
func (h *helmRunner) ociDigest(chartRef, version string) (string, error) {
	cmd := h.command("pull", chartRef, "--version", version)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("helm pull: %w: %s", err, out.String())
	}
	m := ociDigestRE.FindSubmatch(out.Bytes())
	if m == nil {
		return "", fmt.Errorf("no Digest line in helm pull output:\n%s", out.String())
	}
	return string(m[1]), nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func printRow(name, version, digest string) {
	fmt.Printf("%-32s %s\n", fmt.Sprintf("%s (%s)", name, version), digest)
}
