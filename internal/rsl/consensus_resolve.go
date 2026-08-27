// SPDX-License-Identifier: Apache-2.0

package rsl

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/manifests"
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
