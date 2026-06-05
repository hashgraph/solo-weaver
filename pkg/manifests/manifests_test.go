// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"testing"

	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/require"
)

func TestValidateSchemaVersion_AcceptsV1ForEveryKnownKind(t *testing.T) {
	for _, kind := range []Kind{
		KindConsensusNodeComponents,
		KindInfrastructureVersions,
		KindExternalFiles,
		KindStateSources,
	} {
		t.Run(string(kind), func(t *testing.T) {
			header, err := ValidateSchemaVersion(kind, []byte("schemaVersion: 1\n"))
			require.NoError(t, err)
			require.Equal(t, SchemaV1, header.SchemaVersion)
		})
	}
}

func TestValidateSchemaVersion_MissingField(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty document", data: []byte("")},
		{name: "only other fields", data: []byte("foo: bar\nbaz: 42\n")},
		{name: "explicit empty value", data: []byte("schemaVersion:\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateSchemaVersion(KindExternalFiles, tt.data)
			require.Error(t, err)
			require.True(t, errorx.IsOfType(err, MissingSchemaVersionError),
				"expected MissingSchemaVersionError, got %v", err)
			require.Contains(t, err.Error(), string(KindExternalFiles))
		})
	}
}

func TestValidateSchemaVersion_UnsupportedValue(t *testing.T) {
	data := []byte("schemaVersion: 2\n")
	_, err := ValidateSchemaVersion(KindConsensusNodeComponents, data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, UnsupportedSchemaVersionError),
		"expected UnsupportedSchemaVersionError, got %v", err)
	require.Contains(t, err.Error(), string(KindConsensusNodeComponents))
	require.Contains(t, err.Error(), "2")
	require.Contains(t, err.Error(), "1")
}

func TestValidateSchemaVersion_MalformedYAML(t *testing.T) {
	// Unterminated flow sequence — yaml.v3 rejects this at parse time.
	data := []byte("schemaVersion: [1\n")
	_, err := ValidateSchemaVersion(KindStateSources, data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError),
		"expected ParseError, got %v", err)
	require.Contains(t, err.Error(), string(KindStateSources))
}

func TestValidateSchemaVersion_NonIntegerValue(t *testing.T) {
	// The HIP made schemaVersion an integer; a string value must fail
	// decoding, not silently coerce.
	data := []byte(`schemaVersion: "v1"` + "\n")
	_, err := ValidateSchemaVersion(KindExternalFiles, data)
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, ParseError),
		"expected ParseError, got %v", err)
}

func TestValidateSchemaVersion_UnknownKind(t *testing.T) {
	_, err := ValidateSchemaVersion(Kind("not-a-real-manifest"), []byte("schemaVersion: 1\n"))
	require.Error(t, err)
	require.True(t, errorx.IsOfType(err, UnknownKindError),
		"expected UnknownKindError, got %v", err)
	require.Contains(t, err.Error(), "not-a-real-manifest")
}

// Pins the contract that unknown top-level fields are tolerated at the
// schemaVersion-validation stage. Per-kind parsers must not assume this
// validator has already rejected unknown fields.
func TestValidateSchemaVersion_ToleratesUnknownFields(t *testing.T) {
	data := []byte("schemaVersion: 1\nrogueField: ignore-me\nnested:\n  also: tolerated\n")
	header, err := ValidateSchemaVersion(KindInfrastructureVersions, data)
	require.NoError(t, err)
	require.Equal(t, SchemaV1, header.SchemaVersion)
}

func TestSupportedVersions(t *testing.T) {
	t.Run("returns v1 for every known kind", func(t *testing.T) {
		for _, kind := range []Kind{
			KindConsensusNodeComponents,
			KindInfrastructureVersions,
			KindExternalFiles,
			KindStateSources,
		} {
			got := SupportedVersions(kind)
			require.Equal(t, []SchemaVersion{SchemaV1}, got, "kind=%s", kind)
		}
	})

	t.Run("returns nil for unknown kind", func(t *testing.T) {
		require.Nil(t, SupportedVersions(Kind("nope")))
	})
}
