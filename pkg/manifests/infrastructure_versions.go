// SPDX-License-Identifier: Apache-2.0

package manifests

import (
	"fmt"
)

// InfrastructureVersions is the parsed root of an infrastructure-versions.yaml
// manifest. It declares the solo-provisioner binary versions (CLI + daemon)
// that must be installed at apply time, plus an audit record of every host-
// level binary and Helm chart whose version the provisioner will reconcile.
//
// Per the manifest contract ("absent = no change"), every section is optional.
// Provisioner is a pointer so a partial manifest can omit the section
// entirely. Host and Cluster are slices: a missing section and an empty
// section both decode to len==0 — callers that need to differentiate them
// must inspect the raw YAML, not the slice length, but the parser treats
// both identically since they're equivalent under the "absent = no change"
// contract.
type InfrastructureVersions struct {
	Header      `yaml:",inline"`
	Provisioner *Provisioner    `yaml:"provisioner,omitempty"`
	Host        []HostComponent `yaml:"host,omitempty"`
	Cluster     []ClusterChart  `yaml:"cluster,omitempty"`
}

// Provisioner holds the integrity records for the two solo-provisioner
// binaries. The split reflects the existing two-binary layout in this repo
// (CLI: solo-provisioner; daemon: solo-provisioner-daemon — independently
// released and tagged). Both sub-sections are pointer-typed for the same
// "absent = no change" reason as Image.Enabled in #531.
type Provisioner struct {
	CLI    *Binary `yaml:"cli,omitempty"`
	Daemon *Binary `yaml:"daemon,omitempty"`
}

// Binary is one solo-provisioner binary's release spec — the exact version to
// install and the checksum used to verify the downloaded artifact.
type Binary struct {
	Version   string `yaml:"version"`
	Algorithm string `yaml:"algorithm"`
	Checksum  string `yaml:"checksum"`
}

// HostComponent is one audit-record entry under host: — a host-level binary
// (e.g. cri-o, kubelet, kubeadm, kubectl, helm, cilium) whose installed
// version the provisioner reconciles against its embedded catalog.
type HostComponent struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// ClusterChart is one audit-record entry under cluster: — a Helm chart
// installed into the Kubernetes cluster (e.g. alloy, metallb, metrics-server)
// whose installed version the provisioner reconciles against its embedded
// catalog.
type ClusterChart struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// ParseInfrastructureVersions parses raw YAML bytes of an
// infrastructure-versions.yaml manifest. It runs the cross-cutting
// schemaVersion check first, then strict-decodes the single YAML document
// (unknown top-level fields fail; multi-document inputs are rejected), then
// runs semantic validation.
func ParseInfrastructureVersions(data []byte) (*InfrastructureVersions, error) {
	if _, err := ValidateSchemaVersion(KindInfrastructureVersions, data); err != nil {
		return nil, err
	}

	var doc InfrastructureVersions
	if err := decodeStrictSingleYAMLDoc(KindInfrastructureVersions, data, &doc); err != nil {
		return nil, err
	}

	if err := doc.validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

func (iv *InfrastructureVersions) validate() error {
	if iv.Provisioner != nil {
		if err := iv.Provisioner.CLI.validate("provisioner.cli"); err != nil {
			return err
		}
		if err := iv.Provisioner.Daemon.validate("provisioner.daemon"); err != nil {
			return err
		}
	}
	if err := validateAuditEntries("host", entriesFromHost(iv.Host)); err != nil {
		return err
	}
	if err := validateAuditEntries("cluster", entriesFromCluster(iv.Cluster)); err != nil {
		return err
	}
	return nil
}

// validate checks an individual Binary section. A nil receiver is allowed —
// callers use it to express "this binary's section was absent from the
// manifest", which is valid under the "absent = no change" contract.
func (b *Binary) validate(fieldPath string) error {
	if b == nil {
		return nil
	}
	if b.Version == "" {
		return NewValidationError(KindInfrastructureVersions, fieldPath+".version", "must not be empty")
	}
	if b.Algorithm == "" {
		return NewValidationError(KindInfrastructureVersions, fieldPath+".algorithm", "must not be empty")
	}
	if b.Checksum == "" {
		return NewValidationError(KindInfrastructureVersions, fieldPath+".checksum", "must not be empty")
	}
	return nil
}

type auditEntry struct {
	name    string
	version string
}

func entriesFromHost(in []HostComponent) []auditEntry {
	out := make([]auditEntry, len(in))
	for i, c := range in {
		out[i] = auditEntry{name: c.Name, version: c.Version}
	}
	return out
}

func entriesFromCluster(in []ClusterChart) []auditEntry {
	out := make([]auditEntry, len(in))
	for i, c := range in {
		out[i] = auditEntry{name: c.Name, version: c.Version}
	}
	return out
}

// validateAuditEntries enforces the contract on host[] / cluster[]: every
// entry must declare both name and version, and names must be unique within
// a section. Duplicate names would produce ambiguous audit records.
func validateAuditEntries(section string, entries []auditEntry) error {
	seen := make(map[string]struct{}, len(entries))
	for i, e := range entries {
		prefix := fmt.Sprintf("%s[%d]", section, i)
		if e.name == "" {
			return NewValidationError(KindInfrastructureVersions, prefix+".name", "must not be empty")
		}
		if e.version == "" {
			return NewValidationError(KindInfrastructureVersions, prefix+".version", "must not be empty")
		}
		if _, dup := seen[e.name]; dup {
			return NewValidationError(KindInfrastructureVersions, prefix+".name",
				fmt.Sprintf("duplicate entry %q in %s[] (each name must be unique)", e.name, section))
		}
		seen[e.name] = struct{}{}
	}
	return nil
}
