// SPDX-License-Identifier: Apache-2.0

// host-checksums computes the checksums of host artifacts listed under host:
// in the catalog files under pkg/software/catalog/, by downloading and
// unpacking each version for every platform it lists.
//
// To add a version, write its block with URLs, algorithms and empty
// checksums (checksum: ''), then run:
//
//	task host-checksums -- -write
//
// By default only version blocks with an empty checksum are processed; -all
// processes every block. Without -write nothing is changed. A non-empty value
// that differs from the computed one is reported and never overwritten.
//
// The tool downloads with the embedded catalog, so it must run from source
// (go run) to see an edited catalog file.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/software"
)

const catalogDir = "pkg/software/catalog"

type options struct {
	dir     string
	write   bool
	all     bool
	name    string
	version string
	workDir string
	out     io.Writer
	compute func(name, version, goos, goarch, workDir string) ([]software.ComputedChecksum, error)
	fetch   func(url, workDir string) (string, error)
}

func main() {
	opts := options{dir: catalogDir, out: os.Stdout, compute: software.ComputeChecksums, fetch: fetchText}
	flag.BoolVar(&opts.write, "write", false, "fill empty checksums in place (default: check only)")
	flag.BoolVar(&opts.all, "all", false, "process every version block, not only those with an empty checksum")
	flag.StringVar(&opts.name, "name", "", "only this host component, e.g. cri-o")
	flag.StringVar(&opts.version, "version", "", "only this version, e.g. 1.36.7")
	flag.Parse()

	if err := runInTempDir(opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runInTempDir(opts options) error {
	workDir, err := os.MkdirTemp("", "host-checksums-")
	if err != nil {
		return errorx.ExternalError.Wrap(err, "create a work dir")
	}
	defer os.RemoveAll(workDir)
	opts.workDir = workDir
	return run(opts)
}

type block struct {
	component, version string
	entries            []entry
}

type key struct{ section, name, os, arch string }

func run(opts options) error {
	files, err := catalogFiles(opts.dir)
	if err != nil {
		return err
	}

	var blocks []*block
	for _, file := range files {
		entries, err := readEntries(file)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if n := len(blocks); n > 0 && blocks[n-1].component == e.Component && blocks[n-1].version == e.Version {
				blocks[n-1].entries = append(blocks[n-1].entries, e)
				continue
			}
			blocks = append(blocks, &block{component: e.Component, version: e.Version, entries: []entry{e}})
		}
	}

	fills := map[string]map[int]string{} // file -> line -> value
	var mismatches []string
	processed := 0
	for _, b := range blocks {
		if !opts.selects(b) {
			continue
		}
		processed++
		computed, err := computeBlock(opts, b)
		if err != nil {
			return err
		}
		for _, e := range b.entries {
			got := computed[key{e.Section, e.Name, e.OS, e.Arch}]
			status := "ok"
			switch {
			case e.Value == "":
				status = "filled"
				if !opts.write {
					status = "empty"
				}
				if fills[e.File] == nil {
					fills[e.File] = map[int]string{}
				}
				fills[e.File][e.Line] = got
			case !strings.EqualFold(e.Value, got):
				status = "MISMATCH"
				mismatches = append(mismatches, fmt.Sprintf("%s %s %s %s %s: catalog has %s, computed %s",
					e.Component, e.Version, e.Section, e.Name, e.platform(), e.Value, got))
			}
			if e.Published != "" {
				if err := checkPublished(opts, e, got); err != nil {
					status = "MISMATCH"
					mismatches = append(mismatches, err.Error())
				}
			}
			fmt.Fprintf(opts.out, "%-8s %s %s %s %s %s %s\n", status, e.Component, e.Version, e.Section, e.Name, e.platform(), got)
		}
	}

	if processed == 0 {
		fmt.Fprintln(opts.out, "Nothing to do: no version block has an empty checksum (use -all to verify every block).")
		return nil
	}
	if len(mismatches) > 0 {
		return errorx.IllegalState.New("checksums do not match; nothing was written. "+
			"If upstream really changed the artifact, clear the value and re-run with -write:\n  %s",
			strings.Join(mismatches, "\n  "))
	}
	if opts.write && len(fills) > 0 {
		return writeFills(opts.dir, files, fills, opts.out)
	}
	return nil
}

func (o options) selects(b *block) bool {
	if o.name != "" && b.component != o.name {
		return false
	}
	if o.version != "" && b.version != o.version {
		return false
	}
	return o.all || slices.ContainsFunc(b.entries, func(e entry) bool { return e.Value == "" })
}

// computeBlock computes every checksum of a version block, once per platform
// the block lists. A config has one value, so every platform must agree on it.
func computeBlock(opts options, b *block) (map[key]string, error) {
	var platforms [][2]string
	for _, e := range b.entries {
		if e.OS != "" && !slices.Contains(platforms, [2]string{e.OS, e.Arch}) {
			platforms = append(platforms, [2]string{e.OS, e.Arch})
		}
	}
	if len(platforms) == 0 {
		return nil, errorx.IllegalFormat.New("%s %s lists no platforms", b.component, b.version)
	}

	computed := map[key]string{}
	for _, p := range platforms {
		dir := filepath.Join(opts.workDir, b.component+"-"+b.version+"-"+p[0]+"-"+p[1])
		sums, err := opts.compute(b.component, b.version, p[0], p[1], dir)
		if err != nil {
			return nil, errorx.ExternalError.Wrap(err, "%s %s %s/%s", b.component, b.version, p[0], p[1])
		}
		for _, s := range sums {
			k := key{s.Section, s.Name, p[0], p[1]}
			if s.Section == software.SectionConfigs {
				k = key{s.Section, s.Name, "", ""}
				if prev, ok := computed[k]; ok && prev != s.Value {
					return nil, errorx.IllegalState.New("%s %s config %s differs between platforms", b.component, b.version, s.Name)
				}
			}
			computed[k] = s.Value
		}
	}

	for _, e := range b.entries {
		if _, ok := computed[key{e.Section, e.Name, e.OS, e.Arch}]; !ok {
			return nil, errorx.IllegalState.New("%s %s %s %s %s was not computed; run the tool with go run so it sees the edited catalog",
				e.Component, e.Version, e.Section, e.Name, e.platform())
		}
	}
	return computed, nil
}

// checkPublished requires the checksum file named in the entry's comment to
// contain the computed value.
func checkPublished(opts options, e entry, got string) error {
	text, err := opts.fetch(e.Published, opts.workDir)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "%s %s %s %s: fetch %s", e.Component, e.Version, e.Name, e.platform(), e.Published)
	}
	if !strings.Contains(strings.ToLower(text), strings.ToLower(got)) {
		return errorx.IllegalState.New("%s %s %s %s: computed %s is not in %s", e.Component, e.Version, e.Name, e.platform(), got, e.Published)
	}
	return nil
}

// writeFills checks that the filled files still load as a catalog, then writes them.
func writeFills(dir string, files []string, fills map[string]map[int]string, out io.Writer) error {
	staged, err := os.MkdirTemp("", "host-checksums-catalog-")
	if err != nil {
		return errorx.ExternalError.Wrap(err, "create a staging dir")
	}
	defer os.RemoveAll(staged)

	all, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return errorx.ExternalError.Wrap(err, "list %s", dir)
	}
	updated := map[string][]byte{}
	for _, file := range all {
		data, err := os.ReadFile(file)
		if err != nil {
			return errorx.ExternalError.Wrap(err, "read %s", file)
		}
		if lines, ok := fills[file]; ok {
			if data, err = fillChecksums(data, lines); err != nil {
				return errorx.IllegalFormat.Wrap(err, "%s", file)
			}
			updated[file] = data
		}
		if err := os.WriteFile(filepath.Join(staged, filepath.Base(file)), data, 0o644); err != nil {
			return errorx.ExternalError.Wrap(err, "stage %s", file)
		}
	}
	if _, err := software.LoadCatalogDir(staged); err != nil {
		return errorx.IllegalFormat.Wrap(err, "refusing to write: the filled catalog does not load")
	}

	for _, file := range files {
		data, ok := updated[file]
		if !ok {
			continue
		}
		if err := os.WriteFile(file, data, 0o644); err != nil {
			return errorx.ExternalError.Wrap(err, "write %s", file)
		}
		fmt.Fprintf(out, "Wrote %d checksums into %s\n", len(fills[file]), file)
	}
	return nil
}

// catalogFiles lists the catalog files that can hold host entries.
func catalogFiles(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "list %s", dir)
	}
	if len(files) == 0 {
		return nil, errorx.IllegalArgument.New("no catalog files in %s; run from the repo root", dir)
	}
	return slices.DeleteFunc(files, func(f string) bool { return filepath.Base(f) == "defaults.yaml" }), nil
}

// fetchText downloads a small text file, such as a published checksum file.
func fetchText(url, workDir string) (string, error) {
	dest := filepath.Join(workDir, "published.txt")
	if err := software.NewDownloader(software.WithBasePath(workDir)).Download(url, dest); err != nil {
		return "", err
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		return "", errorx.ExternalError.Wrap(err, "read %s", dest)
	}
	return string(data), nil
}
