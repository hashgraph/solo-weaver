// SPDX-License-Identifier: Apache-2.0

package keys

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/hashgraph/solo-weaver/internal/kube"
	corekeys "github.com/hashgraph/solo-weaver/pkg/consensus/keys"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"k8s.io/apimachinery/pkg/util/validation"
)

// KeyType is one of the three kinds of consensus node key secrets.
type KeyType string

const (
	KeyTypeGossip KeyType = "gossip"
	KeyTypeGrpc   KeyType = "grpc"
	KeyTypeAdmin  KeyType = "admin"
)

// Labels and annotations set on every key secret. The labels let weaver and the
// operator find a node's secrets and match one to a roster entry by fingerprint.
const (
	LabelNode        = "solo.hiero.org/node"
	LabelKeyType     = "solo.hiero.org/key-type"
	LabelFingerprint = "solo.hiero.org/cert-fingerprint"

	AnnotationSHA384   = "solo.hiero.org/sha384"
	AnnotationNotAfter = "solo.hiero.org/not-after"

	// fingerprintLabelLen is how much of the SHA-384 fits in a label value (max 63).
	fingerprintLabelLen = 32
	// fingerprintNameLen is how much of it is added to the secret name.
	fingerprintNameLen = 8
	// minFingerprintQueryLen is the shortest --fingerprint prefix accepted.
	minFingerprintQueryLen = 4
	// maxSuffixLen keeps the full secret name well inside Kubernetes limits.
	maxSuffixLen = 40
)

var suffixPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// ValidateSuffix checks a user-supplied --secret-suffix.
func ValidateSuffix(s string) error {
	if s == "" {
		return nil
	}
	if len(s) > maxSuffixLen || !suffixPattern.MatchString(s) {
		return errorx.IllegalArgument.New(
			"invalid secret suffix %q: use lowercase letters, digits, '-' and '.', starting and ending with a letter or digit, at most %d characters",
			s, maxSuffixLen)
	}
	return nil
}

// ValidateLabels checks user-supplied --label values: Kubernetes label syntax, and
// none of weaver's own labels, which the install lookup and fingerprint matching
// depend on.
func ValidateLabels(labels map[string]string) error {
	reserved := map[string]bool{LabelNode: true, LabelKeyType: true, LabelFingerprint: true}
	for k, v := range labels {
		if reserved[k] {
			return errorx.IllegalArgument.New("label %q is set by weaver and cannot be overridden", k)
		}
		if errs := validation.IsQualifiedName(k); len(errs) > 0 {
			return errorx.IllegalArgument.New("invalid label key %q: %s", k, strings.Join(errs, "; "))
		}
		if errs := validation.IsValidLabelValue(v); len(errs) > 0 {
			return errorx.IllegalArgument.New("invalid value %q for label %q: %s", v, k, strings.Join(errs, "; "))
		}
	}
	return nil
}

// Generation describes one set of key material by what is public about it.
type Generation struct {
	Type KeyType
	// SHA384 is the lowercase-hex SHA-384 of the PEM-encoded certificate (gossip, gRPC)
	// or of the raw public key bytes (admin).
	SHA384 string
	// Date is the certificate's "Not before" date (YYYYMMDD, UTC); empty for admin.
	Date string
	// NotAfter is the certificate expiry; zero for admin.
	NotAfter time.Time
}

func baseSecretName(nodeId int64, t KeyType) string {
	scope := models.ConsensusNodeScope(nodeId)
	switch t {
	case KeyTypeGossip:
		return scope + gossipSecretSuffix
	case KeyTypeGrpc:
		return scope + grpcSecretSuffix
	default:
		return scope + adminSecretSuffix
	}
}

// SecretName returns the secret name: <base>-<date>-<fp8>, with suffix replacing
// the date when given. The admin key has no date, so it is <base>-<fp8> unless a
// suffix is given.
func (g Generation) SecretName(nodeId int64, suffix string) string {
	parts := []string{baseSecretName(nodeId, g.Type)}
	switch {
	case suffix != "":
		parts = append(parts, suffix)
	case g.Date != "":
		parts = append(parts, g.Date)
	}
	parts = append(parts, g.SHA384[:fingerprintNameLen])
	return strings.Join(parts, "-")
}

// Labels returns the labels for the generation's secret.
func (g Generation) Labels(nodeId int64) map[string]string {
	return map[string]string{
		LabelNode:        models.ConsensusNodeScope(nodeId),
		LabelKeyType:     string(g.Type),
		LabelFingerprint: g.SHA384[:fingerprintLabelLen],
	}
}

// Annotations returns the annotations for the generation's secret.
func (g Generation) Annotations() map[string]string {
	a := map[string]string{AnnotationSHA384: g.SHA384}
	if !g.NotAfter.IsZero() {
		a[AnnotationNotAfter] = g.NotAfter.UTC().Format("2006-01-02")
	}
	return a
}

func certGeneration(t KeyType, certPEM []byte) (Generation, error) {
	cert, err := corekeys.ParseCertificatePEM(certPEM)
	if err != nil {
		return Generation{}, errorx.Decorate(err, "read the %s certificate", t)
	}
	return Generation{
		Type:     t,
		SHA384:   corekeys.CertPEMHashSHA384(cert),
		Date:     cert.NotBefore.UTC().Format("20060102"),
		NotAfter: cert.NotAfter,
	}, nil
}

func adminGeneration(raw []byte) Generation {
	sum := sha512.Sum384(raw)
	return Generation{Type: KeyTypeAdmin, SHA384: hex.EncodeToString(sum[:])}
}

// generationFromData recomputes a generation from a secret's data.
func generationFromData(t KeyType, data map[string][]byte) (Generation, error) {
	switch t {
	case KeyTypeGossip:
		return certGeneration(t, data[FileGossipCert])
	case KeyTypeGrpc:
		return certGeneration(t, data[FileGrpcCert])
	default:
		raw := data[FileAdminPublic]
		if err := checkAdminPublicLen(len(raw)); err != nil {
			return Generation{}, errorx.Decorate(err, "admin public key secret is not raw key bytes")
		}
		return adminGeneration(raw), nil
	}
}

// ResolveOptions narrows which generation of a node's key secrets to use when
// more than one exists.
type ResolveOptions struct {
	// Fingerprint is a prefix (at least 4 hex characters) of the SHA-384.
	Fingerprint string
	// Suffix is the --secret-suffix the secret was imported with, or its date part.
	Suffix string
}

// ResolvedSecret is a key secret found in the cluster, with its data and the
// generation recomputed from that data.
type ResolvedSecret struct {
	Name       string
	Data       map[string][]byte
	Generation Generation
}

// ResolveSecret finds the secret holding a node's key of the given type. With no
// options it succeeds only when exactly one generation exists; otherwise it lists
// the candidates. The chosen secret's content is checked against its name and
// fingerprint label, so a renamed or hand-edited secret is refused.
func ResolveSecret(ctx context.Context, client SecretClient, namespace string, nodeId int64, t KeyType, opts ResolveOptions) (*ResolvedSecret, error) {
	fp := strings.ToLower(strings.TrimSpace(opts.Fingerprint))
	if fp != "" {
		if _, err := hex.DecodeString(evenLen(fp)); err != nil || len(fp) < minFingerprintQueryLen {
			return nil, errorx.IllegalArgument.New("--fingerprint must be at least %d hex characters, got %q", minFingerprintQueryLen, opts.Fingerprint)
		}
	}
	if err := ValidateSuffix(opts.Suffix); err != nil {
		return nil, err
	}

	selector := fmt.Sprintf("%s=%s,%s=%s", LabelNode, models.ConsensusNodeScope(nodeId), LabelKeyType, t)
	all, err := client.ListSecrets(ctx, namespace, selector)
	if err != nil {
		return nil, err
	}

	base := baseSecretName(nodeId, t)
	var matches []kube.SecretInfo
	for _, s := range all {
		if fp != "" && !strings.HasPrefix(s.Labels[LabelFingerprint], fp) {
			continue
		}
		if opts.Suffix != "" && !strings.HasPrefix(s.Name, base+"-"+opts.Suffix+"-") {
			continue
		}
		matches = append(matches, s)
	}

	switch len(matches) {
	case 0:
		return nil, errorx.IllegalState.New("no %s key secret found for node%d in namespace %q%s; import one with `consensus keys import`",
			t, nodeId, namespace, describeFilter(fp, opts.Suffix))
	case 1:
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return nil, errorx.IllegalState.New("%d %s key secrets found for node%d (%s); choose one with --fingerprint or --secret-suffix",
			len(matches), t, nodeId, strings.Join(names, ", "))
	}

	picked := matches[0]
	data, exists, err := client.GetSecretData(ctx, namespace, picked.Name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errorx.IllegalState.New("secret %q disappeared while reading it", picked.Name)
	}
	gen, err := generationFromData(t, data)
	if err != nil {
		return nil, errorx.Decorate(err, "secret %q", picked.Name)
	}
	if !strings.HasPrefix(gen.SHA384, picked.Labels[LabelFingerprint]) || !strings.HasSuffix(picked.Name, "-"+gen.SHA384[:fingerprintNameLen]) {
		return nil, errorx.IllegalState.New(
			"secret %q does not match its fingerprint: it holds key material with fingerprint %s but its name and label say otherwise; it was probably renamed or edited — re-import the key material",
			picked.Name, gen.SHA384[:fingerprintLabelLen])
	}
	return &ResolvedSecret{Name: picked.Name, Data: data, Generation: gen}, nil
}

func describeFilter(fp, suffix string) string {
	var parts []string
	if fp != "" {
		parts = append(parts, "fingerprint "+fp)
	}
	if suffix != "" {
		parts = append(parts, "suffix "+suffix)
	}
	if len(parts) == 0 {
		return ""
	}
	return " matching " + strings.Join(parts, " and ")
}

func evenLen(s string) string {
	if len(s)%2 == 1 {
		return s + "0"
	}
	return s
}
