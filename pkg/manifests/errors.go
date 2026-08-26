// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"github.com/joomcode/errorx"
)

var (
	ErrorsNamespace = errorx.NewNamespace("manifests")

	ValidationError = ErrorsNamespace.NewType("validation_error")

	kindProperty  = errorx.RegisterPrintableProperty("kind")
	fieldProperty = errorx.RegisterPrintableProperty("field")
)

// NewValidationError flags a semantic-validation failure on a parsed manifest:
// a field is present and structurally valid, but its value violates a rule
// that yaml.Unmarshal alone cannot enforce (e.g. layerHashes appearing in the
// wrong place for a deterministic build). The field path is the dotted Go
// field name, e.g. `images.backupUploader.registries[0].layerHashes`.
func NewValidationError(kind Kind, field string, reason string) *errorx.Error {
	return ValidationError.New("manifest %q: invalid %s: %s", string(kind), field, reason).
		WithProperty(kindProperty, string(kind)).
		WithProperty(fieldProperty, field)
}
