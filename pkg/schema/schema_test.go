// SPDX-License-Identifier: Apache-2.0

//go:build !integration

package schema_test

import (
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/schema"
	"github.com/joomcode/errorx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// demo is the current in-memory shape used by the tests.
type demo struct {
	Name string
	Size int
}

// demoV1 is the sealed v1 on-disk struct.
type demoV1 struct {
	SchemaVersion int    `yaml:"schemaVersion"`
	Name          string `yaml:"name"`
	Size          int    `yaml:"size"`
}

func (v *demoV1) MigrateToLatest() demo {
	return demo{Name: v.Name, Size: v.Size}
}

func newDemoSchema() schema.Versioned[demo] {
	return schema.Versioned[demo]{
		CurrentVersion: 1,
		Factories: map[int]func() schema.Migratable[demo]{
			1: func() schema.Migratable[demo] { return &demoV1{} },
		},
	}
}

func TestDecode_V1(t *testing.T) {
	got, err := newDemoSchema().Decode([]byte("schemaVersion: 1\nname: alpha\nsize: 7\n"))
	require.NoError(t, err)
	assert.Equal(t, demo{Name: "alpha", Size: 7}, got)
}

func TestDecode_AbsentVersionTreatedAsV1(t *testing.T) {
	got, err := newDemoSchema().Decode([]byte("name: beta\nsize: 1\n"))
	require.NoError(t, err)
	assert.Equal(t, demo{Name: "beta", Size: 1}, got)
}

func TestDecode_RejectsNewerVersion(t *testing.T) {
	_, err := newDemoSchema().Decode([]byte("schemaVersion: 99\nname: x\n"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, schema.ErrUnsupportedVersion),
		"expected ErrUnsupportedVersion, got %T: %v", err, err)
	assert.Contains(t, err.Error(), "newer binary")
	assert.Contains(t, err.Error(), "99")
}

func TestDecode_RejectsUnknownField(t *testing.T) {
	_, err := newDemoSchema().Decode([]byte("schemaVersion: 1\nname: x\nbogus: true\n"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, schema.ErrMalformed),
		"expected ErrMalformed, got %T: %v", err, err)
}

func TestDecode_RejectsMultiDocument(t *testing.T) {
	_, err := newDemoSchema().Decode([]byte("schemaVersion: 1\nname: x\n---\nname: y\n"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, schema.ErrMalformed),
		"expected ErrMalformed, got %T: %v", err, err)
}

func TestDecode_RejectsInvalidYAML(t *testing.T) {
	_, err := newDemoSchema().Decode([]byte("schemaVersion: [not a number}"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, schema.ErrMalformed),
		"expected ErrMalformed, got %T: %v", err, err)
}

func TestDecode_RejectsNonIntegerVersion(t *testing.T) {
	_, err := newDemoSchema().Decode([]byte("schemaVersion: not-a-number\nname: x\n"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, schema.ErrMalformed),
		"expected ErrMalformed, got %T: %v", err, err)
}

func TestDecode_RejectsFractionalAndOutOfRangeVersion(t *testing.T) {
	cases := map[string]string{
		"fractional":    "schemaVersion: 1.5\nname: x\n",
		"overflows int": "schemaVersion: 99999999999999999999\nname: x\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := newDemoSchema().Decode([]byte(in))
			require.Error(t, err)
			assert.True(t, errorx.IsOfType(err, schema.ErrMalformed),
				"expected ErrMalformed, got %T: %v", err, err)
		})
	}
}

// legacyV1 mirrors demoV1 but reads its version from a custom key, exercising
// the VersionKey override.
type legacyV1 struct {
	SchemaVersion int    `yaml:"schema_version"`
	Name          string `yaml:"name"`
	Size          int    `yaml:"size"`
}

func (v *legacyV1) MigrateToLatest() demo { return demo{Name: v.Name, Size: v.Size} }

func TestDecode_CustomVersionKey(t *testing.T) {
	s := schema.Versioned[demo]{
		VersionKey:     "schema_version",
		CurrentVersion: 1,
		Factories: map[int]func() schema.Migratable[demo]{
			1: func() schema.Migratable[demo] { return &legacyV1{} },
		},
	}

	got, err := s.Decode([]byte("schema_version: 1\nname: gamma\nsize: 3\n"))
	require.NoError(t, err)
	assert.Equal(t, demo{Name: "gamma", Size: 3}, got)

	// A newer version under the custom key is still rejected, and the message
	// names the custom key.
	_, err = s.Decode([]byte("schema_version: 5\nname: x\n"))
	require.Error(t, err)
	assert.True(t, errorx.IsOfType(err, schema.ErrUnsupportedVersion))
	assert.Contains(t, err.Error(), "schema_version")
}
