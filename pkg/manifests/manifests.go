// SPDX-License-Identifier: Apache-2.0

// Package manifests parses and validates the YAML manifest files shipped under
// manifests/ inside a consensus-node deployment package
// (consensus-node-components.yaml, infrastructure-versions.yaml,
// external-files.yaml, state-sources.yaml).
//
// This initial revision exposes the cross-cutting schemaVersion validator
// (ValidateSchemaVersion plus its supporting Kind, SchemaVersion, Header
// types and typed errorx error classifications) used by every per-manifest
// parser as its first step. Concrete parsers and root structs for the four
// manifest kinds are added in follow-on stories (see issues #531–#534).
package manifests

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the value of the schemaVersion field on a manifest.
type SchemaVersion string

// SchemaV1 is the only schemaVersion currently accepted on any manifest.
const SchemaV1 SchemaVersion = "v1"

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
// Concrete parsers in #531–#534 embed it in their root struct so a single
// strict-decode pass yields both the version and the rest of the document.
type Header struct {
	SchemaVersion SchemaVersion `yaml:"schemaVersion"`
}

// ValidateSchemaVersion decodes only the schemaVersion field from data and
// confirms the value is in the supported set for kind. It returns the parsed
// Header. Callers run this before full unmarshalling so that a manifest
// declaring an unsupported (e.g. future) schemaVersion is rejected with a
// clear error instead of producing surprising decode failures against the
// current shape.
//
// Unknown fields in data are tolerated at this stage — the function inspects
// only schemaVersion. Per-kind parsers may apply stricter checks downstream.
func ValidateSchemaVersion(kind Kind, data []byte) (Header, error) {
	supported, ok := supportedVersions[kind]
	if !ok {
		return Header{}, NewUnknownKindError(kind)
	}

	var h Header
	if err := yaml.Unmarshal(data, &h); err != nil {
		return Header{}, NewParseError(err, kind)
	}

	if h.SchemaVersion == "" {
		return Header{}, NewMissingSchemaVersionError(kind)
	}

	if _, ok := supported[h.SchemaVersion]; !ok {
		return Header{}, NewUnsupportedSchemaVersionError(kind, h.SchemaVersion, sortedSupported(kind))
	}

	return h, nil
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
