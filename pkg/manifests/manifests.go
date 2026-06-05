// SPDX-License-Identifier: Apache-2.0

// Package manifests parses and validates the YAML manifest files shipped
// under manifests/ inside a consensus-node deployment package
// (consensus-node-components.yaml, infrastructure-versions.yaml,
// external-files.yaml, state-sources.yaml).
//
// Each per-manifest parser (ParseConsensusNodeComponents,
// ParseInfrastructureVersions, ParseExternalFiles, ParseStateSources) runs
// the cross-cutting schemaVersion check (ValidateSchemaVersion) first, then
// strict-decodes the document against its typed root struct, then runs
// semantic validation. Typed errorx error classifications (ParseError,
// MissingSchemaVersionError, UnsupportedSchemaVersionError, UnknownKindError,
// ValidationError) let callers branch on failure mode without string
// matching.
package manifests

import (
	"sort"

	"gopkg.in/yaml.v3"
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
// Concrete parsers embed it in their root struct so a single strict-decode
// pass yields both the version and the rest of the document.
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

	// Probe with a pointer-typed schemaVersion so we can distinguish "field
	// absent or explicit null" (nil) from "explicit value zero" (&0). yaml.v3
	// would otherwise coerce both cases to SchemaVersion(0) and lose the
	// distinction.
	var probe struct {
		SchemaVersion *SchemaVersion `yaml:"schemaVersion"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return Header{}, NewParseError(err, kind)
	}

	if probe.SchemaVersion == nil {
		return Header{}, NewMissingSchemaVersionError(kind)
	}

	h := Header{SchemaVersion: *probe.SchemaVersion}
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
