// SPDX-License-Identifier: Apache-2.0

// Package schema provides a generic versioned-YAML loader for any file that
// embeds a schema version field. It handles version probing, unknown-field
// rejection, and in-memory migration to the latest struct shape.
//
// The pattern, extracted once so every versioned file behaves identically:
//
//  1. Probe the version field (Versioned.VersionKey, default "schemaVersion")
//     from raw YAML before decoding the body.
//  2. Normalise an absent/zero version to 1 (files predating versioning).
//  3. Reject any version greater than the running build supports (a file
//     written by a newer binary) with a human-readable "newer binary" error
//     rather than a surprising decode failure against the current shape.
//  4. Strict-decode (KnownFields, single document) into the sealed per-version
//     struct registered for that version.
//  5. Walk the migration chain via Migratable.MigrateToLatest() to produce the
//     current in-memory type T.
//
// The genuinely domain-specific parts — the sealed vN structs and their
// MigrateToLatest field transforms — stay in each consuming package. Only the
// cross-cutting orchestration lives here.
package schema

import (
	"bytes"
	"errors"
	"io"
	"math"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// DefaultVersionKey is the YAML field solo-weaver owned state files use to carry
// their schema version. It is the default when Versioned.VersionKey is empty;
// external schemas may override it with any key they prefer.
const DefaultVersionKey = "schemaVersion"

var (
	// ErrNamespace groups all schema-loading errors.
	ErrNamespace = errorx.NewNamespace("schema")

	// ErrMalformed is returned when the document cannot be probed or strict-decoded.
	ErrMalformed = ErrNamespace.NewType("malformed")

	// ErrUnsupportedVersion is returned when the document declares a schemaVersion
	// the running build does not support (typically a file written by a newer binary).
	ErrUnsupportedVersion = ErrNamespace.NewType("unsupported_version")
)

// Migratable is a sealed, versioned on-disk struct that knows how to migrate
// itself up to the latest in-memory shape T. Each owned state file defines one
// implementation per historical version; the terminal version's MigrateToLatest
// is the identity-style transform into T, and earlier versions delegate down the
// chain (vN.migrate().MigrateToLatest()).
type Migratable[T any] interface {
	MigrateToLatest() T
}

// Versioned describes one owned-state-file schema: the YAML key that carries the
// version, the highest version this build writes, and a factory per supported
// version that returns a fresh pointer to strict-decode into.
type Versioned[T any] struct {
	// VersionKey is the YAML field carrying the schema version. Empty means
	// DefaultVersionKey ("schemaVersion"); external schemas may set any key.
	VersionKey string

	// CurrentVersion is the highest schema version this build understands and writes.
	CurrentVersion int

	// Factories maps a supported version to a constructor returning a fresh
	// sealed struct (as a Migratable[T]) to decode that version's document into.
	Factories map[int]func() Migratable[T]
}

// Decode runs the full owned-state-file load pattern on raw YAML bytes and
// returns the migrated current type T. See the package doc for the steps.
func (s Versioned[T]) Decode(data []byte) (T, error) {
	var zero T

	key := s.VersionKey
	if key == "" {
		key = DefaultVersionKey
	}

	// Phase 1: probe the version key only, via a top-level map so the key is
	// dynamic. An absent key means a pre-versioning file (treated as v1); an
	// explicit zero is likewise normalised to v1.
	var top map[string]any
	if err := yaml.Unmarshal(data, &top); err != nil {
		return zero, ErrMalformed.Wrap(err, "cannot read %s", key)
	}

	version := 1
	if raw, ok := top[key]; ok {
		v, err := toInt(raw)
		if err != nil {
			return zero, ErrMalformed.Wrap(err, "%s must be an integer", key)
		}
		if v != 0 {
			version = v
		}
	}

	if version > s.CurrentVersion {
		return zero, ErrUnsupportedVersion.New(
			"document was written by a newer binary (%s %d > supported %d); upgrade to a compatible version",
			key, version, s.CurrentVersion)
	}

	factory, ok := s.Factories[version]
	if !ok {
		return zero, ErrUnsupportedVersion.New("unsupported %s %d", key, version)
	}

	// Phase 2: strict-decode into the sealed per-version struct, then migrate.
	obj := factory()
	if err := decodeStrictSingleDoc(data, obj); err != nil {
		return zero, ErrMalformed.Wrap(err, "invalid v%d document", version)
	}
	return obj.MigrateToLatest(), nil
}

// decodeStrictSingleDoc decodes data into out with KnownFields(true) (unknown
// fields are an error) and rejects inputs containing more than one YAML
// document. Owned state files are written by us as exactly one document; a
// trailing document or unknown field signals corruption or a format drift we
// want to surface loudly rather than silently ignore.
func decodeStrictSingleDoc(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return err
	}
	var discard any
	err := dec.Decode(&discard)
	switch {
	case err == nil:
		return ErrMalformed.New("must contain exactly one YAML document; additional documents are not permitted")
	case errors.Is(err, io.EOF):
		return nil
	default:
		return err
	}
}

// toInt coerces a YAML-decoded scalar to an int. yaml.v3 decodes integers as
// int (falling back to int64/uint64 for values that overflow int, and float64
// for non-integral numbers), so accept the wider numeric types defensively and
// reject anything out of int range or fractional rather than silently wrapping.
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		if n < math.MinInt || n > math.MaxInt {
			return 0, errorx.IllegalFormat.New("version %d is out of range", n)
		}
		return int(n), nil
	case uint64:
		if n > math.MaxInt {
			return 0, errorx.IllegalFormat.New("version %d is out of range", n)
		}
		return int(n), nil
	case float64:
		if n != math.Trunc(n) {
			return 0, errorx.IllegalFormat.New("expected an integer, got fractional %v", n)
		}
		if n < math.MinInt || n > math.MaxInt {
			return 0, errorx.IllegalFormat.New("version %v is out of range", n)
		}
		return int(n), nil
	default:
		return 0, errorx.IllegalFormat.New("expected an integer, got %T", v)
	}
}
