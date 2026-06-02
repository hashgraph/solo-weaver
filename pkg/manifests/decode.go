// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"bytes"
	"errors"
	"io"

	"gopkg.in/yaml.v3"
)

// decodeStrictSingleYAMLDoc decodes data into out using strict YAML semantics
// (yaml.v3 KnownFields(true), so unknown fields are an error) and rejects
// inputs containing more than one YAML document. Multi-document streams would
// otherwise yield the first document silently while dropping the rest — a
// failure mode that's easy to ship and hard to detect, especially for a
// council-signed manifest where exactly-one document is the contract.
//
// Returns a typed *errorx.Error: ParseError on decode failure, ValidationError
// when a trailing document is found.
func decodeStrictSingleYAMLDoc(kind Kind, data []byte, out interface{}) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil {
		return NewParseError(err, kind)
	}
	var discard interface{}
	err := dec.Decode(&discard)
	switch {
	case err == nil:
		return NewValidationError(kind, "<document>",
			"manifest must contain exactly one YAML document; additional documents are not permitted")
	case errors.Is(err, io.EOF):
		// Single document — the expected case.
		return nil
	default:
		// Trailing bytes that look like a malformed second document.
		return NewParseError(err, kind)
	}
}
