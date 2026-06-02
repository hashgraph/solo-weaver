// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"fmt"
)

// ExternalFiles is the parsed root of an external-files.yaml manifest. It
// declares large remote files (over the ~1 MB ConfigurationFile-CR limit)
// that the upgrade-controller sidecar or the solo-provisioner-upgrade daemon
// must download and stage on the host before the consensus node restarts.
type ExternalFiles struct {
	Header `yaml:",inline"`
	Files  []ExternalFile `yaml:"files,omitempty"`
}

// ExternalFile is one entry under files:. The destination is expressed using
// a directory-marker prefix (e.g. HAPIAPP_DIR/...) that the downloader
// resolves to a real filesystem path at apply time.
type ExternalFile struct {
	URL         string `yaml:"url"`
	Algorithm   string `yaml:"algorithm"`
	Checksum    string `yaml:"checksum"`
	ContentType string `yaml:"contentType,omitempty"`
	Destination string `yaml:"destination"`
	// Optional defaults to false. When true, a download failure for this entry
	// is logged but does not abort the wider apply. yaml.v3 zeroes the field
	// when absent from the YAML, which is the same as the documented default.
	Optional bool  `yaml:"optional,omitempty"`
	Phase    Phase `yaml:"phase"`
}

// Phase tells the downloader when each file is fetched and when it is moved
// into place. Download can happen either before the freeze window starts
// (prepare — concurrent with normal traffic) or during the freeze itself.
// Install always happens during the freeze, when the CN is stopped and the
// staged files can be moved into place atomically.
type Phase struct {
	Download DownloadPhase `yaml:"download"`
	Install  InstallPhase  `yaml:"install"`
}

// DownloadPhase enumerates the legal values for phase.download. "prepare"
// downloads before the freeze window begins (low-risk, can spread network
// I/O over a long lead time); "freeze" delays the download until the freeze
// window itself (used for large state-bearing files that must reflect the
// exact freeze-time bytes).
type DownloadPhase string

const (
	DownloadPhasePrepare DownloadPhase = "prepare"
	DownloadPhaseFreeze  DownloadPhase = "freeze"
)

// InstallPhase enumerates the legal values for phase.install. Today only
// "freeze" is permitted — installing a large file outside the freeze window
// would require the CN to be live during a non-atomic move and is rejected
// by design.
type InstallPhase string

const (
	InstallPhaseFreeze InstallPhase = "freeze"
)

// ParseExternalFiles parses raw YAML bytes of an external-files.yaml
// manifest. It runs the cross-cutting schemaVersion check first, then
// strict-decodes the single YAML document (unknown top-level fields fail;
// multi-document inputs are rejected), then runs per-entry semantic
// validation.
func ParseExternalFiles(data []byte) (*ExternalFiles, error) {
	if _, err := ValidateSchemaVersion(KindExternalFiles, data); err != nil {
		return nil, err
	}

	var doc ExternalFiles
	if err := decodeStrictSingleYAMLDoc(KindExternalFiles, data, &doc); err != nil {
		return nil, err
	}

	if err := doc.validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// validate enforces semantic invariants on every files[] entry. Two files
// declaring the same destination would silently overwrite each other at
// install time, so destination uniqueness is enforced here.
func (ef *ExternalFiles) validate() error {
	seenDestinations := make(map[string]int, len(ef.Files))
	for i := range ef.Files {
		if err := ef.Files[i].validate(i); err != nil {
			return err
		}
		dest := ef.Files[i].Destination
		if prevIdx, dup := seenDestinations[dest]; dup {
			return NewValidationError(KindExternalFiles,
				fmt.Sprintf("files[%d].destination", i),
				fmt.Sprintf("duplicate destination %q (also declared by files[%d]); two entries cannot install to the same path", dest, prevIdx))
		}
		seenDestinations[dest] = i
	}
	return nil
}

func (f *ExternalFile) validate(idx int) error {
	prefix := fmt.Sprintf("files[%d]", idx)

	if f.URL == "" {
		return NewValidationError(KindExternalFiles, prefix+".url", "must not be empty")
	}
	if f.Algorithm == "" {
		return NewValidationError(KindExternalFiles, prefix+".algorithm", "must not be empty")
	}
	if f.Checksum == "" {
		return NewValidationError(KindExternalFiles, prefix+".checksum", "must not be empty")
	}
	if f.Destination == "" {
		return NewValidationError(KindExternalFiles, prefix+".destination", "must not be empty")
	}

	switch f.Phase.Download {
	case DownloadPhasePrepare, DownloadPhaseFreeze:
	case "":
		return NewValidationError(KindExternalFiles, prefix+".phase.download", "must not be empty")
	default:
		return NewValidationError(KindExternalFiles, prefix+".phase.download",
			fmt.Sprintf("invalid value %q (must be %q or %q)", f.Phase.Download, DownloadPhasePrepare, DownloadPhaseFreeze))
	}

	switch f.Phase.Install {
	case InstallPhaseFreeze:
	case "":
		return NewValidationError(KindExternalFiles, prefix+".phase.install", "must not be empty")
	default:
		return NewValidationError(KindExternalFiles, prefix+".phase.install",
			fmt.Sprintf("invalid value %q (must be %q)", f.Phase.Install, InstallPhaseFreeze))
	}

	return nil
}
