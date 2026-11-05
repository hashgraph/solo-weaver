// SPDX-License-Identifier: Apache-2.0

package software

import (
	"bytes"
	"io/fs"
	"reflect"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"

	"github.com/hashgraph/solo-weaver/pkg/deps"
)

// defaultsFile holds every component's default version. Every other *.yaml file
// in the catalog directory is a component file.
const defaultsFile = "defaults.yaml"

// catalogDefaults is the content of defaults.yaml.
type catalogDefaults struct {
	Host    map[string]Version `yaml:"host"`
	Cluster map[string]Version `yaml:"cluster"`
}

// catalogMerger builds one InfrastructureCatalog out of several component files,
// remembering which file each component and version came from so a conflict
// can name both files.
type catalogMerger struct {
	catalog InfrastructureCatalog
	origin  map[string]string // "host/<name>" or "host/<name>/<version>" -> file
}

// loadCatalog reads every *.yaml file in fsys, merges the component files and
// applies defaults.yaml. Files are read in lexical order and each component
// keeps the position where it first appears, so the merged order is stable.
func loadCatalog(fsys fs.FS) (*InfrastructureCatalog, error) {
	files, err := fs.Glob(fsys, "*.yaml")
	if err != nil {
		return nil, errorx.InternalError.Wrap(err, "list catalog files")
	}

	m := catalogMerger{origin: map[string]string{}}
	var defaults *catalogDefaults
	for _, file := range files {
		data, err := fs.ReadFile(fsys, file)
		if err != nil {
			return nil, errorx.InternalError.Wrap(err, "read %s", file)
		}

		if file == defaultsFile {
			var d catalogDefaults
			if err := decodeStrict(data, &d); err != nil {
				return nil, errorx.IllegalFormat.Wrap(err, "%s", file)
			}
			defaults = &d
			continue
		}

		var part InfrastructureCatalog
		if err := decodeStrict(data, &part); err != nil {
			return nil, errorx.IllegalFormat.Wrap(err, "%s", file)
		}
		if err := m.merge(file, part); err != nil {
			return nil, err
		}
	}
	if defaults == nil {
		return nil, errorx.IllegalFormat.New("catalog has no %s", defaultsFile)
	}

	catalog := m.catalog
	if err := catalog.applyDefaults(*defaults); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "%s", defaultsFile)
	}

	// The solo-operator chart version is owned by pkg/deps.SOLO_OPERATOR_VERSION
	// — the single source of truth it shares with the UC sidecar tag — so inject
	// it as the catalog default instead of duplicating a version string in the
	// YAML. Injecting before validate() preserves the "default must exist in
	// versions" invariant, which guarantees a checksum entry exists for this
	// exact version (otherwise the catalog fails to load, loud and early).
	for i := range catalog.Cluster {
		if catalog.Cluster[i].Name == SoloOperatorComponentName {
			catalog.Cluster[i].Default = Version(deps.SOLO_OPERATOR_VERSION)
		}
	}

	if err := catalog.validate(); err != nil {
		return nil, err
	}
	return &catalog, nil
}

// decodeStrict decodes data into out, rejecting keys out has no field for.
func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	return dec.Decode(out)
}

// merge adds file's components to the catalog. A default declared in a
// component file is rejected: defaults live only in defaults.yaml.
func (m *catalogMerger) merge(file string, part InfrastructureCatalog) error {
	for _, host := range part.Host {
		if host.Default != "" {
			return errorx.IllegalFormat.New("%s: host[%s] declares a default; defaults belong in %s", file, host.Name, defaultsFile)
		}
		if err := m.mergeHost(file, host); err != nil {
			return err
		}
	}
	for _, chart := range part.Cluster {
		if chart.Default != "" {
			return errorx.IllegalFormat.New("%s: cluster[%s] declares a default; defaults belong in %s", file, chart.Name, defaultsFile)
		}
		if err := m.mergeChart(file, chart); err != nil {
			return err
		}
	}
	return nil
}

func (m *catalogMerger) mergeHost(file string, host ArtifactMetadata) error {
	key := "host/" + host.Name
	idx := -1
	for i := range m.catalog.Host {
		if m.catalog.Host[i].Name == host.Name {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.catalog.Host = append(m.catalog.Host, ArtifactMetadata{
			Name:        host.Name,
			SelfRelease: host.SelfRelease,
		})
		idx = len(m.catalog.Host) - 1
		m.origin[key] = file
	} else if !reflect.DeepEqual(m.catalog.Host[idx].SelfRelease, host.SelfRelease) {
		return errorx.IllegalFormat.New("host[%s]: selfRelease differs between %s and %s", host.Name, m.origin[key], file)
	}

	merged := &m.catalog.Host[idx]
	for v, details := range host.Versions {
		vkey := key + "/" + string(v)
		if prev, ok := m.origin[vkey]; ok {
			return errorx.IllegalFormat.New("host[%s]: version %s is listed in both %s and %s", host.Name, v, prev, file)
		}
		if merged.Versions == nil {
			merged.Versions = map[Version]VersionDetails{}
		}
		merged.Versions[v] = details
		m.origin[vkey] = file
	}
	return nil
}

func (m *catalogMerger) mergeChart(file string, chart ChartMetadata) error {
	key := "cluster/" + chart.Name
	idx := -1
	for i := range m.catalog.Cluster {
		if m.catalog.Cluster[i].Name == chart.Name {
			idx = i
			break
		}
	}
	if idx < 0 {
		placed := chart
		placed.Versions = nil
		m.catalog.Cluster = append(m.catalog.Cluster, placed)
		idx = len(m.catalog.Cluster) - 1
		m.origin[key] = file
	} else {
		have := m.catalog.Cluster[idx]
		if have.Type != chart.Type || have.Repo != chart.Repo || have.Chart != chart.Chart ||
			have.Namespace != chart.Namespace || have.Release != chart.Release {
			return errorx.IllegalFormat.New("cluster[%s]: type, repo, chart, namespace or release differs between %s and %s",
				chart.Name, m.origin[key], file)
		}
	}

	merged := &m.catalog.Cluster[idx]
	for v, checksum := range chart.Versions {
		vkey := key + "/" + string(v)
		if prev, ok := m.origin[vkey]; ok {
			return errorx.IllegalFormat.New("cluster[%s]: version %s is listed in both %s and %s", chart.Name, v, prev, file)
		}
		if merged.Versions == nil {
			merged.Versions = map[Version]Checksum{}
		}
		merged.Versions[v] = checksum
		m.origin[vkey] = file
	}
	return nil
}

// applyDefaults sets each component's default from defaults.yaml. A component
// left without one fails validate(). solo-operator is rejected here because its
// default is injected from deps.SOLO_OPERATOR_VERSION.
func (c *InfrastructureCatalog) applyDefaults(d catalogDefaults) error {
	for name, v := range d.Host {
		artifact, err := c.GetHostArtifact(name)
		if err != nil {
			return errorx.IllegalFormat.New("host default for %q, which no catalog file lists", name)
		}
		artifact.Default = v
	}
	for name, v := range d.Cluster {
		if name == SoloOperatorComponentName {
			return errorx.IllegalFormat.New("cluster default for %q must not be set; it comes from deps.SOLO_OPERATOR_VERSION", name)
		}
		chart, err := c.GetClusterComponent(name)
		if err != nil {
			return errorx.IllegalFormat.New("cluster default for %q, which no catalog file lists", name)
		}
		chart.Default = v
	}
	return nil
}
