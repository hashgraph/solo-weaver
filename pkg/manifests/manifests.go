// SPDX-License-Identifier: Apache-2.0

// Package manifests parses and validates the YAML manifest files shipped
// under manifests/ inside a consensus-node deployment package
// (consensus-node-components.yaml, infrastructure-versions.yaml,
// external-files.yaml, state-sources.yaml).
//
// Each per-manifest parser (ParseConsensusNodeComponents,
// ParseInfrastructureVersions, ParseExternalFiles, ParseStateSources) uses
// pkg/schema.Versioned to probe the schemaVersion, decode with lenient
// parsing (unknown fields silently ignored per HIP-1494), enforce single-
// document YAML, and migrate across schema versions. Decode-level errors
// (malformed YAML, unsupported version, multi-document) surface as
// schema.ErrMalformed or schema.ErrUnsupportedVersion. After decode,
// each parser runs semantic validation; those failures surface as
// ValidationError.
package manifests

import (
	"sort"
)

// SchemaVersion is the value of the schemaVersion field on a manifest. The
// HIP defines the field as an integer ("schemaVersion: 1") so the parser
// does not need to round-trip strings into version numbers.
type SchemaVersion int

// SchemaV1 is the only schemaVersion currently accepted on any manifest.
const SchemaV1 SchemaVersion = 1

// Kind identifies which of the four manifest files is being parsed. Its string
// value matches the basename (without ".yaml") of the file inside the
// deployment package's manifests/ directory.
type Kind string

const (
	KindConsensusNodeComponents Kind = "consensus-node-components"
	KindInfrastructureVersions  Kind = "infrastructure-versions"
	KindExternalFiles           Kind = "external-files"
	KindStateSources            Kind = "state-sources"
)

// supportedVersions records the schemaVersion values this build accepts for
// each manifest kind. Bumping a manifest to v2 requires adding that version
// here and shipping a corresponding parser that knows how to read it.
var supportedVersions = map[Kind]map[SchemaVersion]struct{}{
	KindConsensusNodeComponents: {SchemaV1: {}},
	KindInfrastructureVersions:  {SchemaV1: {}},
	KindExternalFiles:           {SchemaV1: {}},
	KindStateSources:            {SchemaV1: {}},
}

// Header captures the common schemaVersion field present on every manifest.
// Concrete parsers embed it in their root struct so a single decode
// pass yields both the version and the rest of the document.
type Header struct {
	SchemaVersion SchemaVersion `yaml:"schemaVersion"`
}

// SupportedVersions returns the sorted list of schemaVersion values this build
// accepts for kind, or nil if kind is not a recognised manifest. It exists for
// callers that need to render help text or diagnostics.
func SupportedVersions(kind Kind) []SchemaVersion {
	if _, ok := supportedVersions[kind]; !ok {
		return nil
	}
	return sortedSupported(kind)
}

func sortedSupported(kind Kind) []SchemaVersion {
	versions := make([]SchemaVersion, 0, len(supportedVersions[kind]))
	for v := range supportedVersions[kind] {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions
}
