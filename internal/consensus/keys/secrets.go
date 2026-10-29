// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/kube"
	"github.com/joomcode/errorx"
)

// SecretClient is the subset of the Kubernetes client the import/export flows
// need. *kube.Client satisfies it; tests use a fake.
type SecretClient interface {
	ApplySecret(ctx context.Context, namespace, name string, data map[string][]byte, labels, annotations map[string]string) error
	GetSecretData(ctx context.Context, namespace, name string) (map[string][]byte, bool, error)
	ListSecrets(ctx context.Context, namespace, labelSelector string) ([]kube.SecretInfo, error)
	GetSecretInfo(ctx context.Context, namespace, name string) (*kube.SecretInfo, bool, error)
	// PatchSecretLabels sets labels without sending the Secret's data.
	PatchSecretLabels(ctx context.Context, namespace, name string, labels map[string]string) error
}

// Base secret name suffixes. Each key secret is named <node scope><base>-<date>-<fp8>
// (see Generation.SecretName), so a rotation never overwrites an earlier generation.
// The operator reads the names from the capsule, so the format is weaver's choice.
const (
	gossipSecretSuffix = "-gossip-keys"
	grpcSecretSuffix   = "-grpc-tls-keys"
	adminSecretSuffix  = "-admin-pubkey"
)

// secretFile is one entry of a key secret that export writes to the folder.
type secretFile struct {
	name string
	perm os.FileMode
}

// exportFiles are the only entries export writes for each key type; any other
// entry in the secret is ignored.
var exportFiles = map[KeyType][]secretFile{
	KeyTypeGossip: {{FileGossipPrivate, privatePerm}, {FileGossipCert, publicPerm}},
	KeyTypeGrpc:   {{FileGrpcKey, privatePerm}, {FileGrpcCert, publicPerm}},
	KeyTypeAdmin:  {{FileAdminPublic, publicPerm}},
}

// ImportOptions parameterizes creating the per-node secrets from a folder.
type ImportOptions struct {
	Namespace string
	NodeId    int64
	FromDir   string
	Selectors Selectors
	// Suffix replaces the certificate date in the secret names (e.g. "v0.75.6").
	Suffix string
	// Force overwrites an existing secret of the same name whose content differs.
	// A name carries the key's fingerprint, so that only happens if a secret was
	// edited by hand; re-importing identical material is idempotent regardless.
	Force bool
	// Labels are extra labels to put on every secret created (for example a
	// reference to an external system). They may not reuse weaver's own label keys.
	Labels map[string]string
}

// ImportResult reports which secrets were created/updated vs left unchanged.
type ImportResult struct {
	Applied   []string // created or overwritten
	Relabeled []string // content already up to date; only labels were added or changed
	Skipped   []string // already up to date
}

// Import reads the selected key material from a folder and creates one secret per
// key type, named by the key's date and fingerprint. Only the admin PUBLIC key
// (admin.pub, created in an external wallet) is imported; there is no admin
// private key in the folder to read or push. Different material always gets a different name, so an import never
// replaces an earlier generation.
//
// Import is idempotent: re-applying identical material is a no-op. A secret that
// already exists under the same name with DIFFERENT content is refused unless
// Force is set.
func Import(ctx context.Context, client SecretClient, opts ImportOptions) (*ImportResult, error) {
	if err := ValidateSuffix(opts.Suffix); err != nil {
		return nil, err
	}
	if err := ValidateLabels(opts.Labels); err != nil {
		return nil, err
	}
	sel := opts.Selectors
	if !sel.Any() {
		// No selector: import the generated keys, plus the admin public key when
		// the operator has placed one in the folder.
		_, err := os.Stat(filepath.Join(opts.FromDir, FileAdminPublic))
		sel = Selectors{Gossip: true, Grpc: true, Admin: err == nil}
	}
	result := &ImportResult{}

	apply := func(gen Generation, data map[string][]byte) error {
		name := gen.SecretName(opts.NodeId, opts.Suffix)
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
		labels := gen.Labels(opts.NodeId)
		for k, v := range opts.Labels {
			labels[k] = v
		}
		switch {
		case exists && equalSecretData(existing, data):
			// Same content: never re-send it. Only add or change labels that differ.
			info, _, err := client.GetSecretInfo(ctx, opts.Namespace, name)
			if err != nil {
				return err
			}
			changed := map[string]string{}
			for k, v := range labels {
				if info == nil || info.Labels[k] != v {
					changed[k] = v
				}
			}
			if len(changed) == 0 {
				log.Info().Msg("Skipping import; secret already up to date")
				result.Skipped = append(result.Skipped, name)
				return nil
			}
			log.Info().Msg("Secret content is up to date; updating labels only")
			if err := client.PatchSecretLabels(ctx, opts.Namespace, name, changed); err != nil {
				return err
			}
			result.Relabeled = append(result.Relabeled, name)
			return nil
		case exists && !opts.Force:
			return errorx.IllegalState.New("secret %q already exists with different content; it does not match the key material being imported. Pass --force to overwrite", name)
		case exists:
			log.Warn().Msg("Overwriting existing secret with different content (--force)")
		default:
			log.Info().Msg("Creating secret")
		}
		if err := client.ApplySecret(ctx, opts.Namespace, name, data, labels, gen.Annotations()); err != nil {
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
		gen, err := certGeneration(KeyTypeGossip, data[FileGossipCert])
		if err != nil {
			return nil, err
		}
		if err := apply(gen, data); err != nil {
			return nil, err
		}
	}
	if sel.Grpc {
		data, err := readFiles(opts.FromDir, FileGrpcKey, FileGrpcCert)
		if err != nil {
			return nil, err
		}
		gen, err := certGeneration(KeyTypeGrpc, data[FileGrpcCert])
		if err != nil {
			return nil, err
		}
		if err := apply(gen, data); err != nil {
			return nil, err
		}
	}
	if sel.Admin {
		// Only the public key. The operator reads this secret's
		// value as raw key bytes, so decode the hex file before storing it.
		files, err := readFiles(opts.FromDir, FileAdminPublic)
		if err != nil {
			return nil, err
		}
		raw, err := AdminPublicToRaw(files[FileAdminPublic])
		if err != nil {
			return nil, err
		}
		if err := apply(adminGeneration(raw), map[string][]byte{FileAdminPublic: raw}); err != nil {
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
	// Resolve narrows which generation to export when a node has more than one.
	Resolve ResolveOptions
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
	result := &ExportResult{}

	if err := os.MkdirAll(opts.OutDir, dirPerm); err != nil {
		return nil, errorx.ExternalError.Wrap(err, "create export directory %q", opts.OutDir)
	}

	targets := []struct {
		enabled bool
		typ     KeyType
	}{
		{sel.Gossip, KeyTypeGossip},
		{sel.Grpc, KeyTypeGrpc},
		{sel.Admin, KeyTypeAdmin},
	}

	for _, t := range targets {
		if !t.enabled {
			continue
		}
		secret, err := ResolveSecret(ctx, client, opts.Namespace, opts.NodeId, t.typ, opts.Resolve)
		if err != nil {
			return nil, err
		}
		log := logx.As().With().
			Str("namespace", opts.Namespace).
			Int64("nodeId", opts.NodeId).
			Str("secret", secret.Name).
			Str("dir", opts.OutDir).
			Bool("force", opts.Force).
			Logger()
		files := exportFiles[t.typ]
		expected := make(map[string]bool, len(files))
		for _, f := range files {
			expected[f.name] = true
			if _, ok := secret.Data[f.name]; !ok {
				return nil, errorx.IllegalState.New("secret %q has no %q entry; re-import the key material", secret.Name, f.name)
			}
		}
		for key := range secret.Data {
			if !expected[key] {
				log.Warn().Str("entry", key).Msg("Ignoring unexpected secret entry; it is not exported")
			}
		}
		wrote := false
		for _, f := range files {
			value := secret.Data[f.name]
			if t.typ == KeyTypeAdmin {
				// The secret holds raw key bytes; the folder file is hex text.
				if value, err = AdminPublicToFile(value); err != nil {
					return nil, err
				}
			}
			changed, err := writeFileGuarded(opts.OutDir, f.name, value, f.perm, opts.Force)
			if err != nil {
				return nil, err
			}
			wrote = wrote || changed
		}
		if wrote {
			log.Info().Msg("Exported secret to folder")
			result.Exported = append(result.Exported, secret.Name)
		} else {
			log.Info().Msg("Skipping export; folder already up to date")
			result.Skipped = append(result.Skipped, secret.Name)
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

// AdminPublicToRaw converts the contents of an admin.pub file (hex text, with or
// without a trailing newline) to the raw key bytes stored in the admin secret:
// 32 bytes for Ed25519 or a 33-byte compressed point for secp256k1.
func AdminPublicToRaw(fileContent []byte) ([]byte, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(string(fileContent)))
	if err != nil {
		return nil, errorx.IllegalArgument.Wrap(err, "admin public key is not valid hex")
	}
	if err := checkAdminPublicLen(len(raw)); err != nil {
		return nil, err
	}
	return raw, nil
}

// AdminPublicToFile converts raw admin public key bytes (as stored in the admin
// secret) to the admin.pub file format: lowercase hex plus a trailing newline.
func AdminPublicToFile(raw []byte) ([]byte, error) {
	if err := checkAdminPublicLen(len(raw)); err != nil {
		return nil, errorx.Decorate(err, "admin public key secret is not raw key bytes")
	}
	return []byte(hex.EncodeToString(raw) + "\n"), nil
}

func checkAdminPublicLen(n int) error {
	if n != 32 && n != 33 {
		return errorx.IllegalArgument.New("unexpected admin public key length %d (want 32 for ed25519 or 33 for secp256k1)", n)
	}
	return nil
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
