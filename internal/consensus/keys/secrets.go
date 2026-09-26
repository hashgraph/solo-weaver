// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
)

// SecretClient is the subset of the Kubernetes client the import/export flows
// need. *kube.Client satisfies it; tests use a fake.
type SecretClient interface {
	ApplySecret(ctx context.Context, namespace, name string, data map[string][]byte, labels map[string]string) error
	GetSecretData(ctx context.Context, namespace, name string) (map[string][]byte, bool, error)
}

// Per-node secret name suffixes. They combine with the node scope (node<N>) to
// form the secret names; the suffixes match `consensus node install` defaults so
// import and install agree without extra flags.
const (
	gossipSecretSuffix = "-gossip-keys"
	grpcSecretSuffix   = "-grpc-tls-keys"
	adminSecretSuffix  = "-admin-pubkey"
)

// SecretNames holds the three per-node secret names.
type SecretNames struct {
	Gossip string
	Grpc   string
	Admin  string
}

// SecretNamesFor returns the per-node secret names for nodeId (0-based). Each
// name is already node-scoped, so the secret inner-keys stay neutral/index-free.
func SecretNamesFor(nodeId int64) SecretNames {
	scope := models.ConsensusNodeScope(nodeId)
	return SecretNames{
		Gossip: scope + gossipSecretSuffix,
		Grpc:   scope + grpcSecretSuffix,
		Admin:  scope + adminSecretSuffix,
	}
}

// privateFiles are the inner-key names that hold private material and must be
// written with 0600 permissions on export.
var privateFiles = map[string]bool{
	FileGossipPrivate: true,
	FileGrpcKey:       true,
}

// ImportOptions parameterizes creating the per-node secrets from a folder.
type ImportOptions struct {
	Namespace string
	NodeId    int64
	FromDir   string
	Selectors Selectors
	// Force overwrites an existing secret whose material differs. Re-importing
	// identical material is idempotent regardless; a differing gossip secret is
	// the node's network identity, so overwriting it needs Force.
	Force  bool
	Labels map[string]string
}

// ImportResult reports which secrets were created/updated vs left unchanged.
type ImportResult struct {
	Applied []string // created or overwritten
	Skipped []string // already up to date
}

// Import reads the selected key material from a folder and creates the per-node
// Kubernetes secrets. Only the admin PUBLIC key is imported — the admin private
// key (admin.key) is never read or pushed to the cluster.
//
// Import is idempotent: re-applying identical material is a no-op. A secret that
// already exists with DIFFERENT material is refused unless Force is set (for the
// gossip secret this protects the node's network identity).
func Import(ctx context.Context, client SecretClient, opts ImportOptions) (*ImportResult, error) {
	sel := NormalizeSelectors(opts.Selectors)
	names := SecretNamesFor(opts.NodeId)
	result := &ImportResult{}

	apply := func(name string, data map[string][]byte) error {
		existing, exists, err := client.GetSecretData(ctx, opts.Namespace, name)
		if err != nil {
			return err
		}
		log := logx.As().With().
			Str("namespace", opts.Namespace).
			Int64("nodeId", opts.NodeId).
			Str("secret", name).
			Str("keysDir", opts.FromDir).
			Bool("force", opts.Force).
			Logger()
		switch {
		case exists && equalSecretData(existing, data):
			log.Info().Msg("Skipping import; secret already up to date")
			result.Skipped = append(result.Skipped, name)
			return nil
		case exists && !opts.Force:
			return errorx.IllegalState.New("secret %q already exists with different material; pass --force to overwrite", name)
		case exists:
			log.Warn().Msg("Overwriting existing secret with different material (--force)")
		default:
			log.Info().Msg("Creating secret")
		}
		if err := client.ApplySecret(ctx, opts.Namespace, name, data, opts.Labels); err != nil {
			return err
		}
		result.Applied = append(result.Applied, name)
		return nil
	}

	if sel.Gossip {
		data, err := readFiles(opts.FromDir, FileGossipPrivate, FileGossipCert)
		if err != nil {
			return nil, err
		}
		if err := apply(names.Gossip, data); err != nil {
			return nil, err
		}
	}
	if sel.Grpc {
		data, err := readFiles(opts.FromDir, FileGrpcKey, FileGrpcCert)
		if err != nil {
			return nil, err
		}
		if err := apply(names.Grpc, data); err != nil {
			return nil, err
		}
	}
	if sel.Admin {
		// Only the public key — never admin.key.
		data, err := readFiles(opts.FromDir, FileAdminPublic)
		if err != nil {
			return nil, err
		}
		if err := apply(names.Admin, data); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// ExportOptions parameterizes writing the per-node secrets back to a folder.
type ExportOptions struct {
	Namespace string
	NodeId    int64
	OutDir    string
	Selectors Selectors
	// Force overwrites an existing file whose content differs. Without it, export
	// is idempotent (identical files are left as-is) and refuses to clobber a file
	// that holds different content.
	Force bool
}

// ExportResult reports which secrets were written vs left unchanged.
type ExportResult struct {
	Exported []string // at least one file written
	Skipped  []string // all files already up to date
}

// Export reads the selected per-node secrets from the cluster and writes their
// contents to a folder (a backup). The admin secret holds only the public key,
// so a full round-trip of the admin private key is not possible from the cluster.
func Export(ctx context.Context, client SecretClient, opts ExportOptions) (*ExportResult, error) {
	sel := NormalizeSelectors(opts.Selectors)
	names := SecretNamesFor(opts.NodeId)
	result := &ExportResult{}

	if err := os.MkdirAll(opts.OutDir, dirPerm); err != nil {
		return nil, errorx.ExternalError.Wrap(err, "create export directory %q", opts.OutDir)
	}

	targets := []struct {
		enabled bool
		name    string
	}{
		{sel.Gossip, names.Gossip},
		{sel.Grpc, names.Grpc},
		{sel.Admin, names.Admin},
	}

	for _, t := range targets {
		if !t.enabled {
			continue
		}
		data, exists, err := client.GetSecretData(ctx, opts.Namespace, t.name)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, errorx.IllegalState.New("secret %q not found in namespace %q", t.name, opts.Namespace)
		}
		wrote := false
		for key, value := range data {
			perm := publicPerm
			if privateFiles[key] {
				perm = privatePerm
			}
			changed, err := writeFileGuarded(opts.OutDir, key, value, perm, opts.Force)
			if err != nil {
				return nil, err
			}
			wrote = wrote || changed
		}
		log := logx.As().With().
			Str("namespace", opts.Namespace).
			Int64("nodeId", opts.NodeId).
			Str("secret", t.name).
			Str("dir", opts.OutDir).
			Bool("force", opts.Force).
			Logger()
		if wrote {
			log.Info().Msg("Exported secret to folder")
			result.Exported = append(result.Exported, t.name)
		} else {
			log.Info().Msg("Skipping export; folder already up to date")
			result.Skipped = append(result.Skipped, t.name)
		}
	}

	return result, nil
}

// writeFileGuarded writes payload to dir/name and reports whether it changed the
// file. An identical existing file is left untouched (changed=false, idempotent);
// a differing file is refused unless force is set, and its overwrite is logged.
func writeFileGuarded(dir, name string, payload []byte, perm os.FileMode, force bool) (bool, error) {
	path := filepath.Join(dir, name)
	if existing, err := os.ReadFile(path); err == nil {
		if bytes.Equal(existing, payload) {
			return false, nil
		}
		if !force {
			return false, errorx.IllegalState.New("%q already exists with different content; pass --force to overwrite", path)
		}
		logx.As().Warn().Str("path", path).Msg("Overwriting existing file with different content (--force)")
	}
	if err := writeFile(dir, name, payload, perm); err != nil {
		return false, err
	}
	return true, nil
}

// equalSecretData reports whether two secret data maps have identical keys and
// values.
func equalSecretData(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, av := range a {
		if !bytes.Equal(av, b[k]) {
			return false
		}
	}
	return true
}

// readFiles reads the named files from dir into a data map keyed by file name.
func readFiles(dir string, names ...string) (map[string][]byte, error) {
	data := make(map[string][]byte, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, errorx.IllegalArgument.Wrap(err, "required key file %q not found", path)
			}
			return nil, errorx.ExternalError.Wrap(err, "read %q", path)
		}
		data[name] = content
	}
	return data, nil
}
