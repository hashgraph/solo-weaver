// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"io"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// ---------------------------------------------------------------------------
// UnmarshalManifest — shared manifest helper used by blockNodeChecker
// ---------------------------------------------------------------------------

// UnmarshalManifest parses a Helm release manifest (possibly multi-doc YAML)
// and returns a slice of Unstructured objects (one per non-empty document).
func UnmarshalManifest(manifest string) ([]*unstructured.Unstructured, error) {
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []*unstructured.Unstructured

	for {
		var doc map[string]interface{}
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		// skip empty documents (e.g. separators or whitespace-only)
		if len(doc) == 0 {
			continue
		}
		out = append(out, &unstructured.Unstructured{Object: doc})
	}
	return out, nil
}
