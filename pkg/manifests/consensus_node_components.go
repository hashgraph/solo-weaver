// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"fmt"
	"sort"
)

// ConsensusNodeComponents is the parsed root of a consensus-node-components.yaml
// manifest. It declares the container images for the consensus node and its
// named sidecars, plus the layer-hash integrity records used to verify those
// images before they run.
//
// Per the manifest contract ("absent = no change"), every entry under Images is
// optional. A nil entry signals "the deployment package does not change this
// component", not "disable this component" — that decision is encoded in the
// Image.Enabled pointer field instead.
type ConsensusNodeComponents struct {
	Header `yaml:",inline"`
	Images Images `yaml:"images"`
}

// Images groups the consensus node and its five named sidecars. Each field is
// a pointer so a parser can distinguish absent from explicitly-zero. Unknown
// component names in the YAML are rejected by strict decoding; if the set of
// supported sidecars grows, this struct grows with it.
type Images struct {
	ConsensusNode        *Image `yaml:"consensusNode,omitempty"`
	RecordStreamUploader *Image `yaml:"recordStreamUploader,omitempty"`
	EventStreamUploader  *Image `yaml:"eventStreamUploader,omitempty"`
	BlockStreamUploader  *Image `yaml:"blockStreamUploader,omitempty"`
	BackupUploader       *Image `yaml:"backupUploader,omitempty"`
	UC                   *Image `yaml:"uc,omitempty"`
}

// Image is the per-component spec. Enabled is a tri-state pointer so the
// manifest can carry an explicit on/off intent (true / false) distinct from
// "no opinion" (nil). Note that when an Image entry is present at all, the
// other required fields (Version, Registries) must still be set — validation
// enforces this. A nil entry under Images is the "absent = no change"
// signal at the section level.
type Image struct {
	Enabled       *bool          `yaml:"enabled,omitempty"`
	Version       string         `yaml:"version"`
	Deterministic *Deterministic `yaml:"deterministic,omitempty"`
	Registries    []Registry     `yaml:"registries"`
}

// Deterministic describes whether a component's container images produce
// identical layer hashes across all registries for the same version. When
// Supported is true, LayerHashes is the single shared record used to verify
// every registry; when false, each Registry carries its own layerHashes
// override and LayerHashes here must be empty.
type Deterministic struct {
	Supported   bool        `yaml:"supported"`
	LayerHashes LayerHashes `yaml:"layerHashes,omitempty"`
}

// Registry is one publication site for a component image. For non-deterministic
// components, LayerHashes is a per-registry override (because the same logical
// image produces different layer digests at each registry).
type Registry struct {
	Image       string      `yaml:"image"`
	LayerHashes LayerHashes `yaml:"layerHashes,omitempty"`
}

// LayerHashes maps a container platform identifier (e.g. "linux/arm64") to an
// ordered list of layer digests for that platform.
type LayerHashes map[string][]string

// SupportedImagePlatforms is the set of platform identifiers accepted in any
// layerHashes map. The slice is kept sorted so it can appear directly in
// error messages without surprising readers.
var SupportedImagePlatforms = []string{"linux/amd64", "linux/arm64"}

// ParseConsensusNodeComponents parses raw YAML bytes of a
// consensus-node-components.yaml manifest. It runs the cross-cutting
// schemaVersion check first (so a future-versioned manifest is rejected before
// any current-shape decode), then strict-decodes the (single) YAML document
// into ConsensusNodeComponents (unknown top-level fields or unknown component
// names under images: are errors; multi-document inputs are rejected), then
// runs semantic validation on every present component entry.
func ParseConsensusNodeComponents(data []byte) (*ConsensusNodeComponents, error) {
	if _, err := ValidateSchemaVersion(KindConsensusNodeComponents, data); err != nil {
		return nil, err
	}

	var doc ConsensusNodeComponents
	if err := decodeStrictSingleYAMLDoc(KindConsensusNodeComponents, data, &doc); err != nil {
		return nil, err
	}

	if err := doc.validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// validate enforces the semantic invariants of a parsed manifest. It runs on
// every non-nil entry under Images and fails fast on the first violation,
// reporting the dotted field path so the user can find the offending entry.
func (c *ConsensusNodeComponents) validate() error {
	for _, e := range c.Images.entries() {
		if e.image == nil {
			continue
		}
		if err := e.image.validate(e.name); err != nil {
			return err
		}
	}
	return nil
}

type namedImage struct {
	name  string
	image *Image
}

// entries returns the named image fields in a deterministic order so that
// validation errors are reproducible.
func (i Images) entries() []namedImage {
	return []namedImage{
		{name: "consensusNode", image: i.ConsensusNode},
		{name: "recordStreamUploader", image: i.RecordStreamUploader},
		{name: "eventStreamUploader", image: i.EventStreamUploader},
		{name: "blockStreamUploader", image: i.BlockStreamUploader},
		{name: "backupUploader", image: i.BackupUploader},
		{name: "uc", image: i.UC},
	}
}

func (img *Image) validate(componentName string) error {
	prefix := "images." + componentName

	if img.Version == "" {
		return NewValidationError(KindConsensusNodeComponents, prefix+".version", "must not be empty")
	}
	if len(img.Registries) == 0 {
		return NewValidationError(KindConsensusNodeComponents, prefix+".registries", "must declare at least one registry")
	}

	deterministicSupported := img.Deterministic != nil && img.Deterministic.Supported

	if deterministicSupported {
		if len(img.Deterministic.LayerHashes) == 0 {
			return NewValidationError(KindConsensusNodeComponents, prefix+".deterministic.layerHashes",
				"must be declared when deterministic.supported is true")
		}
		if err := validateLayerHashesPlatforms(prefix+".deterministic.layerHashes", img.Deterministic.LayerHashes); err != nil {
			return err
		}
	} else if img.Deterministic != nil && len(img.Deterministic.LayerHashes) > 0 {
		return NewValidationError(KindConsensusNodeComponents, prefix+".deterministic.layerHashes",
			"must not be set when deterministic.supported is false (use per-registry overrides)")
	}

	for idx, reg := range img.Registries {
		regPrefix := fmt.Sprintf("%s.registries[%d]", prefix, idx)
		if reg.Image == "" {
			return NewValidationError(KindConsensusNodeComponents, regPrefix+".image", "must not be empty")
		}
		if deterministicSupported {
			if len(reg.LayerHashes) > 0 {
				return NewValidationError(KindConsensusNodeComponents, regPrefix+".layerHashes",
					"must not be set when deterministic.supported is true (use the shared deterministic.layerHashes)")
			}
		} else {
			if len(reg.LayerHashes) == 0 {
				return NewValidationError(KindConsensusNodeComponents, regPrefix+".layerHashes",
					"must be declared for non-deterministic components")
			}
			if err := validateLayerHashesPlatforms(regPrefix+".layerHashes", reg.LayerHashes); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateLayerHashesPlatforms(fieldPath string, hashes LayerHashes) error {
	allowed := make(map[string]struct{}, len(SupportedImagePlatforms))
	for _, p := range SupportedImagePlatforms {
		allowed[p] = struct{}{}
	}
	platforms := make([]string, 0, len(hashes))
	for p := range hashes {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)
	for _, p := range platforms {
		if _, ok := allowed[p]; !ok {
			return NewValidationError(KindConsensusNodeComponents, fieldPath,
				fmt.Sprintf("unsupported platform %q (supported: %v)", p, SupportedImagePlatforms))
		}
		if len(hashes[p]) == 0 {
			return NewValidationError(KindConsensusNodeComponents, fieldPath+"."+p,
				"must declare at least one layer hash")
		}
	}
	return nil
}
