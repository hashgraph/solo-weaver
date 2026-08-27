// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashgraph/solo-weaver/pkg/manifests"
	"github.com/joomcode/errorx"
)

// ResolveImageFromManifest reads manifests/consensus-node-components.yaml from
// the deployment package and returns (repo, tag).
func ResolveImageFromManifest(pkgDir string) (repo string, tag string, err error) {
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

// ResolvePropertiesValue reads a Java .properties file and returns the value
// for the given key, or empty string if not found.
func ResolvePropertiesValue(path, key string) (string, error) {
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

// ResolveLedgerAndChain extracts ledger.id and contracts.chainId from
// data/config/application.properties in the deployment package.
func ResolveLedgerAndChain(pkgDir string) (ledgerId string, chainId string, err error) {
	path := filepath.Join(pkgDir, "data", "config", "application.properties")

	ledgerId, err = ResolvePropertiesValue(path, "ledger.id")
	if err != nil {
		return "", "", errorx.ExternalError.Wrap(err, "reading application.properties")
	}

	chainId, err = ResolvePropertiesValue(path, "contracts.chainId")
	if err != nil {
		return "", "", errorx.ExternalError.Wrap(err, "reading application.properties")
	}

	return ledgerId, chainId, nil
}
