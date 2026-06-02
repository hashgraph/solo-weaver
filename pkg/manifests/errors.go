// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"github.com/joomcode/errorx"
)

var (
	ErrorsNamespace = errorx.NewNamespace("manifests")

	ParseError                    = ErrorsNamespace.NewType("parse_error")
	MissingSchemaVersionError     = ErrorsNamespace.NewType("missing_schema_version")
	UnsupportedSchemaVersionError = ErrorsNamespace.NewType("unsupported_schema_version")
	UnknownKindError              = ErrorsNamespace.NewType("unknown_kind")

	kindProperty              = errorx.RegisterPrintableProperty("kind")
	schemaVersionProperty     = errorx.RegisterPrintableProperty("schema_version")
	supportedVersionsProperty = errorx.RegisterPrintableProperty("supported_versions")
)

const (
	parseErrorMsg                    = "failed to parse manifest %q"
	missingSchemaVersionErrorMsg     = "manifest %q is missing required field \"schemaVersion\""
	unsupportedSchemaVersionErrorMsg = "manifest %q declares schemaVersion %q (supported: %v)"
	unknownKindErrorMsg              = "unknown manifest kind %q"
)

func NewParseError(cause error, kind Kind) *errorx.Error {
	err := ParseError.New(parseErrorMsg, string(kind)).
		WithProperty(kindProperty, string(kind))
	if cause != nil {
		err = err.WithUnderlyingErrors(cause)
	}
	return err
}

func NewMissingSchemaVersionError(kind Kind) *errorx.Error {
	return MissingSchemaVersionError.New(missingSchemaVersionErrorMsg, string(kind)).
		WithProperty(kindProperty, string(kind))
}

func NewUnsupportedSchemaVersionError(kind Kind, declared SchemaVersion, supported []SchemaVersion) *errorx.Error {
	supportedStrs := make([]string, len(supported))
	for i, v := range supported {
		supportedStrs[i] = string(v)
	}
	return UnsupportedSchemaVersionError.New(unsupportedSchemaVersionErrorMsg, string(kind), string(declared), supportedStrs).
		WithProperty(kindProperty, string(kind)).
		WithProperty(schemaVersionProperty, string(declared)).
		WithProperty(supportedVersionsProperty, supportedStrs)
}

func NewUnknownKindError(kind Kind) *errorx.Error {
	return UnknownKindError.New(unknownKindErrorMsg, string(kind)).
		WithProperty(kindProperty, string(kind))
}
