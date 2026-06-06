// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// allowedBucketSchemes is the closed set of cloud-storage URI schemes that
// may prefix the `bucket` field on any state-sources.yaml entry. The scheme
// encodes the provider (per HIP-XXXX0): the downloader picks its SDK client
// purely by string-matching the scheme, so an unknown scheme is unfollowable
// and must be rejected at parse time rather than silently failing later.
//
// Kept unexported so that enforcement (validateBucketScheme) reads from an
// immutable source of truth — external callers see only a cloned copy via
// AllowedBucketSchemes() and cannot weaken the policy by mutating the slice
// in place.
var allowedBucketSchemes = []string{
	"gcs://",
	"s3://",
}

// AllowedBucketSchemes returns a fresh copy of the closed set of cloud-storage
// URI schemes recognised on state-sources.yaml bucket fields. Each call
// returns a new slice so callers cannot affect enforcement by mutating the
// returned value.
func AllowedBucketSchemes() []string {
	return slices.Clone(allowedBucketSchemes)
}

// StateSources is the parsed root of a state-sources.yaml manifest. It
// declares one or more cloud storage buckets from which a new or rejoining
// consensus node can fast-sync the latest saved-state snapshot rather than
// replaying the entire event stream from genesis. Multiple buckets are
// listed for redundancy and geographic locality.
type StateSources struct {
	Header  `yaml:",inline"`
	Sources []StateSource `yaml:"stateSources,omitempty"`
}

// StateSource is one cloud-storage entry. Each source declares its location
// (region), the bucket URI (whose scheme encodes the provider — `gcs://`,
// `s3://`), and two parallel maps keyed by node ID: Index names the per-node
// index file containing the latest available round, and Paths names the
// per-node base directory where that round's state files live.
type StateSource struct {
	Bucket   string            `yaml:"bucket"`
	Location string            `yaml:"location"`
	Index    map[string]string `yaml:"index"`
	Paths    map[string]string `yaml:"paths"`
}

// ParseStateSources parses raw YAML bytes of a state-sources.yaml manifest.
// It runs the cross-cutting schemaVersion check first, then strict-decodes
// the single YAML document (unknown top-level fields fail; multi-document
// inputs are rejected), then runs per-source semantic validation.
func ParseStateSources(data []byte) (*StateSources, error) {
	if _, err := ValidateSchemaVersion(KindStateSources, data); err != nil {
		return nil, err
	}

	var doc StateSources
	if err := decodeStrictSingleYAMLDoc(KindStateSources, data, &doc); err != nil {
		return nil, err
	}

	if err := doc.validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// validate enforces per-source invariants and the cross-source uniqueness
// rule on bucket URIs (two stateSources[] entries pointing at the same
// bucket would be ambiguous and almost certainly a manifest authoring
// mistake).
func (ss *StateSources) validate() error {
	seenBuckets := make(map[string]int, len(ss.Sources))
	for i := range ss.Sources {
		if err := ss.Sources[i].validate(i); err != nil {
			return err
		}
		bucket := ss.Sources[i].Bucket
		if prevIdx, dup := seenBuckets[bucket]; dup {
			return NewValidationError(KindStateSources,
				fmt.Sprintf("stateSources[%d].bucket", i),
				fmt.Sprintf("duplicate bucket %q (also declared by stateSources[%d])", bucket, prevIdx))
		}
		seenBuckets[bucket] = i
	}
	return nil
}

func (s *StateSource) validate(idx int) error {
	prefix := fmt.Sprintf("stateSources[%d]", idx)

	if s.Bucket == "" {
		return NewValidationError(KindStateSources, prefix+".bucket", "must not be empty")
	}
	if err := validateBucketScheme(prefix+".bucket", s.Bucket); err != nil {
		return err
	}
	if s.Location == "" {
		return NewValidationError(KindStateSources, prefix+".location", "must not be empty")
	}

	if len(s.Index) == 0 {
		return NewValidationError(KindStateSources, prefix+".index", "must declare at least one node")
	}
	if len(s.Paths) == 0 {
		return NewValidationError(KindStateSources, prefix+".paths", "must declare at least one node")
	}

	// Every node must appear in both index and paths — an index pointing at a
	// node with no path is unfollowable, and a path with no index has no
	// "latest round" pointer.
	if err := validateNodeKeysMatch(prefix, s.Index, s.Paths); err != nil {
		return err
	}
	if err := validateMapEntriesNonEmpty(prefix+".index", s.Index); err != nil {
		return err
	}
	if err := validateMapEntriesNonEmpty(prefix+".paths", s.Paths); err != nil {
		return err
	}
	return nil
}

// validateBucketScheme enforces that bucket starts with one of the
// allowedBucketSchemes. The provider type is encoded in the scheme (per
// HIP-XXXX0), so an unknown scheme is rejected with a clear error listing
// the supported set.
func validateBucketScheme(fieldPath, bucket string) error {
	for _, scheme := range allowedBucketSchemes {
		if strings.HasPrefix(bucket, scheme) && len(bucket) > len(scheme) {
			return nil
		}
	}
	return NewValidationError(KindStateSources, fieldPath,
		fmt.Sprintf("must start with a recognised cloud-storage scheme (allowed: %v); got %q", allowedBucketSchemes, bucket))
}

// validateNodeKeysMatch enforces that index and paths declare exactly the
// same set of node IDs. Mismatches are surfaced with the offending node ID
// named explicitly so the manifest author knows which row to fix.
func validateNodeKeysMatch(prefix string, index, paths map[string]string) error {
	missingFromPaths := sortedMissingKeys(index, paths)
	for _, node := range missingFromPaths {
		return NewValidationError(KindStateSources, prefix+".paths",
			fmt.Sprintf("node %q is listed in index but missing from paths", node))
	}
	missingFromIndex := sortedMissingKeys(paths, index)
	for _, node := range missingFromIndex {
		return NewValidationError(KindStateSources, prefix+".index",
			fmt.Sprintf("node %q is listed in paths but missing from index", node))
	}
	return nil
}

// sortedMissingKeys returns the keys present in have but absent from want,
// sorted alphabetically so error messages name the same node on repeat runs.
func sortedMissingKeys(have, want map[string]string) []string {
	missing := make([]string, 0)
	for k := range have {
		if _, ok := want[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}

func validateMapEntriesNonEmpty(fieldPath string, m map[string]string) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "" {
			return NewValidationError(KindStateSources, fieldPath, "node ID key must not be empty")
		}
		if m[k] == "" {
			return NewValidationError(KindStateSources, fieldPath+"."+k, "must not be empty")
		}
	}
	return nil
}
