// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"regexp"
	"strings"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"

	"github.com/hashgraph/solo-weaver/pkg/software"
)

// entry is one checksum value in a host catalog file. OS and Arch are empty
// for configs, which have one checksum for every platform.
type entry struct {
	File      string
	Component string
	Version   string
	Section   string
	Name      string
	OS        string
	Arch      string
	Algorithm string
	Value     string
	Line      int    // 1-based line holding the checksum value
	Published string // URL from a trailing "# from <url>" comment
}

func (e entry) platform() string {
	if e.OS == "" {
		return "all"
	}
	return e.OS + "/" + e.Arch
}

// readEntries returns every checksum entry under host: in a catalog file, in
// file order. A file without host: has none.
func readEntries(file string) ([]entry, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "read %s", file)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, errorx.IllegalFormat.Wrap(err, "parse %s", file)
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}
	host := mapValue(doc.Content[0], "host")
	if host == nil {
		return nil, nil
	}

	var entries []entry
	for _, component := range host.Content {
		name := scalar(mapValue(component, "name"))
		versions := mapValue(component, "versions")
		if versions == nil {
			continue // a self-released artifact
		}
		for i := 0; i+1 < len(versions.Content); i += 2 {
			version, details := versions.Content[i].Value, versions.Content[i+1]
			for _, section := range []string{software.SectionArchives, software.SectionBinaries, software.SectionConfigs} {
				items := mapValue(details, section)
				if items == nil {
					continue
				}
				for _, item := range items.Content {
					base := entry{File: file, Component: name, Version: version, Section: section, Name: scalar(mapValue(item, "name"))}
					found, err := itemEntries(base, item)
					if err != nil {
						return nil, errorx.IllegalFormat.Wrap(err, "%s: %s %s %s %s", file, name, version, section, base.Name)
					}
					entries = append(entries, found...)
				}
			}
		}
	}
	return entries, nil
}

// itemEntries reads the checksums of one archive, binary or config.
func itemEntries(base entry, item *yaml.Node) ([]entry, error) {
	if base.Section == software.SectionConfigs {
		e, err := withChecksum(base, item)
		if err != nil {
			return nil, err
		}
		return []entry{e}, nil
	}

	var entries []entry
	for i := 0; i+1 < len(item.Content); i += 2 {
		goos, arches := item.Content[i].Value, item.Content[i+1]
		if goos == "name" || goos == "url" || goos == "archive" {
			continue
		}
		if arches.Kind != yaml.MappingNode {
			return nil, errorx.IllegalFormat.New("%s: expected a map of architectures", goos)
		}
		for j := 0; j+1 < len(arches.Content); j += 2 {
			e := base
			e.OS, e.Arch = goos, arches.Content[j].Value
			e, err := withChecksum(e, arches.Content[j+1])
			if err != nil {
				return nil, err
			}
			entries = append(entries, e)
		}
	}
	if len(entries) == 0 {
		return nil, errorx.IllegalFormat.New("no platforms listed")
	}
	return entries, nil
}

// withChecksum fills the algorithm, value and position from a node holding
// algorithm: and checksum: keys.
func withChecksum(e entry, n *yaml.Node) (entry, error) {
	key, value := mapEntry(n, "checksum")
	if value == nil {
		return e, errorx.IllegalFormat.New("%s: no checksum key; add checksum: ''", e.platform())
	}
	e.Algorithm = scalar(mapValue(n, "algorithm"))
	e.Value = value.Value
	e.Line = key.Line
	for _, comment := range []string{value.LineComment, key.LineComment} {
		if url, ok := strings.CutPrefix(strings.TrimSpace(strings.TrimPrefix(comment, "#")), "from "); ok {
			e.Published = strings.TrimSpace(url)
		}
	}
	return e, nil
}

func mapEntry(n *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i], n.Content[i+1]
		}
	}
	return nil, nil
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	_, v := mapEntry(n, key)
	return v
}

func scalar(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	return n.Value
}

var checksumLineRE = regexp.MustCompile(`^(\s*checksum:)\s*('[^']*'|"[^"]*"|[^\s#]*)(.*)$`)

// fillChecksums returns data with the checksum value on each given line
// replaced. Everything else on the line, such as a trailing comment, is kept.
func fillChecksums(data []byte, values map[int]string) ([]byte, error) {
	lines := strings.Split(string(data), "\n")
	for line, value := range values {
		if line < 1 || line > len(lines) {
			return nil, errorx.IllegalArgument.New("line %d is out of range", line)
		}
		m := checksumLineRE.FindStringSubmatch(lines[line-1])
		if m == nil {
			return nil, errorx.IllegalFormat.New("line %d is not a checksum line: %q", line, lines[line-1])
		}
		lines[line-1] = m[1] + " '" + value + "'" + m[3]
	}
	return []byte(strings.Join(lines, "\n")), nil
}
