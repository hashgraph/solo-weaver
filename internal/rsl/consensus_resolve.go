// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/manifests"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

func resolveImageFromManifest(pkgDir string) (repo string, tag string, err error) {
	path := filepath.Join(pkgDir, "manifests", "consensus-node-components.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", errorx.ExternalError.Wrap(err, "reading manifest")
	}

	doc, err := manifests.ParseConsensusNodeComponents(data)
	if err != nil {
		return "", "", errorx.ExternalError.Wrap(err, "parsing manifest")
	}

	cn := doc.Images.ConsensusNode
	if cn == nil {
		return "", "", errorx.IllegalState.New("manifest missing images.consensusNode")
	}

	tag = cn.Version
	if tag == "" {
		return "", "", errorx.IllegalState.New("manifest missing images.consensusNode.version")
	}

	if len(cn.Registries) == 0 {
		return "", "", errorx.IllegalState.New("manifest missing images.consensusNode.registries")
	}

	full := cn.Registries[0].Image
	if idx := strings.LastIndex(full, ":"); idx != -1 {
		repo = full[:idx]
	} else {
		repo = full
	}

	return repo, tag, nil
}

// resolveConsensusImageSource parses the manifest and returns the consensus
// node's multi-registry image source, or nil when the manifest shape is not
// representable (see buildImageSource).
func resolveConsensusImageSource(pkgDir string) (*models.ImageSource, error) {
	path := filepath.Join(pkgDir, "manifests", "consensus-node-components.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "reading manifest")
	}

	doc, err := manifests.ParseConsensusNodeComponents(data)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "parsing manifest")
	}

	return buildImageSource(doc.Images.ConsensusNode), nil
}

// buildImageSource maps a manifest Image onto a models.ImageSource. It returns
// nil for any shape the operator cannot represent — a single registry, a
// non-deterministic image (per-registry hashes), or no hashes — since the
// operator carries one shared verification spec across all candidate registries.
func buildImageSource(img *manifests.Image) *models.ImageSource {
	if img == nil || len(img.Registries) < 2 {
		return nil
	}
	if img.Deterministic == nil || !img.Deterministic.Supported || len(img.Deterministic.LayerHashes) == 0 {
		return nil
	}

	repos := make([]models.ImageRepositoryRef, 0, len(img.Registries))
	for _, reg := range img.Registries {
		repo, name := splitRepoName(stripImageTag(reg.Image))
		repos = append(repos, models.ImageRepositoryRef{
			Repository: repo,
			ImageName:  name,
			ImageTag:   img.Version, // manifest version is the authoritative tag
		})
	}

	hashes := make(map[string][]string, len(img.Deterministic.LayerHashes))
	for platform, hs := range img.Deterministic.LayerHashes {
		cp := make([]string, len(hs))
		copy(cp, hs)
		hashes[platform] = cp
	}

	return &models.ImageSource{Repositories: repos, LayerHashes: hashes}
}

// stripImageTag removes a trailing ":tag", treating a colon as a tag separator
// only after the last "/" (so a "host:port/repo" port is not mistaken for a tag).
func stripImageTag(image string) string {
	slash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > slash {
		return image[:colon]
	}
	return image
}

// splitRepoName splits "repository/imageName" at the last "/"; with no "/" the
// whole string is the image name.
func splitRepoName(repoPath string) (repository, imageName string) {
	if i := strings.LastIndex(repoPath, "/"); i != -1 {
		return repoPath[:i], repoPath[i+1:]
	}
	return "", repoPath
}

func resolvePropertiesValue(path, key string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		k := strings.TrimSpace(parts[0])
		if k == key && len(parts) == 2 {
			return strings.TrimSpace(parts[1]), nil
		}
	}
	return "", scanner.Err()
}

func resolveLedgerAndChain(pkgDir string) (ledgerId string, chainId string, err error) {
	path := filepath.Join(pkgDir, "data", "config", "application.properties")

	ledgerId, err = resolvePropertiesValue(path, "ledger.id")
	if err != nil {
		return "", "", errorx.ExternalError.Wrap(err, "reading application.properties")
	}

	chainId, err = resolvePropertiesValue(path, "contracts.chainId")
	if err != nil {
		return "", "", errorx.ExternalError.Wrap(err, "reading application.properties")
	}

	return ledgerId, chainId, nil
}

func readFileFromPackage(pkgDir, relPath string) (string, error) {
	p := filepath.Join(pkgDir, relPath)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

func blockNodesRelPath(nodeId int64) string {
	return filepath.Join("block-nodes", "config", fmt.Sprintf("block-nodes-%d.json", nodeId))
}
