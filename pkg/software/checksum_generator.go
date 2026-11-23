// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"path"

	"github.com/joomcode/errorx"
)

// Catalog sections a computed checksum belongs to.
const (
	SectionArchives = "archives"
	SectionBinaries = "binaries"
	SectionConfigs  = "configs"
)

// ComputedChecksum is the digest of one file of a host version, as found by
// downloading and unpacking it. Name is the entry's name as written in the
// catalog, before templating.
type ComputedChecksum struct {
	Section   string
	Name      string
	Algorithm string
	Value     string
}

// ComputeChecksums downloads and unpacks one version of a host artifact for the
// given platform under workDir, and hashes the archives, every binary and every
// config with the algorithm the catalog names for it. It reads the embedded
// catalog, so run it from source to pick up an edited catalog file.
func ComputeChecksums(name, version, goos, goarch, workDir string) ([]ComputedChecksum, error) {
	b, err := newBaseInstaller(name, WithVersion(version))
	if err != nil {
		return nil, err
	}
	b.software = b.software.withPlatform(goos, goarch)
	b.workDir = workDir
	b.downloader = NewDownloader(WithBasePath(workDir))
	return b.computeChecksums()
}

// computeChecksums walks the version like Download and Extract do, but hashes
// each file instead of verifying it.
func (b *baseInstaller) computeChecksums() ([]ComputedChecksum, error) {
	versionInfo, exists := b.software.Versions[Version(b.versionToBeInstalled)]
	if !exists {
		return nil, NewVersionNotFoundError(b.software.Name, b.versionToBeInstalled)
	}

	for _, dir := range []string{b.downloadsDir(), b.extractFolder()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, errorx.ExternalError.Wrap(err, "failed to create %s", dir)
		}
	}

	var sums []ComputedChecksum

	for _, archive := range versionInfo.GetArchives() {
		sum, err := b.fetchAndHash(SectionArchives, archive.Name, archive.URL, archive.PlatformChecksum)
		if err != nil {
			return nil, err
		}
		sums = append(sums, sum)
	}

	for _, binary := range versionInfo.BinariesByURL() {
		sum, err := b.fetchAndHash(SectionBinaries, binary.Name, binary.URL, binary.PlatformChecksum)
		if err != nil {
			return nil, err
		}
		sums = append(sums, sum)
	}

	for _, cfg := range versionInfo.ConfigsByURL() {
		dest := path.Join(b.downloadsDir(), cfg.Name)
		if err := b.download(cfg.URL, dest); err != nil {
			return nil, err
		}
		sum, err := b.hash(SectionConfigs, cfg.Name, dest, cfg.Algorithm)
		if err != nil {
			return nil, err
		}
		sums = append(sums, sum)
	}

	for _, archive := range versionInfo.GetArchives() {
		if err := b.extractArchive(archive, b.extractFolder()); err != nil {
			return nil, err
		}
	}

	for _, binary := range versionInfo.BinariesByArchive() {
		checksum, err := b.platformChecksum(binary.PlatformChecksum)
		if err != nil {
			return nil, err
		}
		binaryName, err := executeTemplate(binary.Name, b.templateData())
		if err != nil {
			return nil, NewTemplateError(err, b.software.Name)
		}
		sum, err := b.hash(SectionBinaries, binary.Name, path.Join(b.extractFolder(), binaryName), checksum.Algorithm)
		if err != nil {
			return nil, err
		}
		sums = append(sums, sum)
	}

	for _, cfg := range versionInfo.ConfigsByArchive() {
		configName, err := executeTemplate(cfg.Name, b.templateData())
		if err != nil {
			return nil, NewTemplateError(err, b.software.Name)
		}
		sum, err := b.hash(SectionConfigs, cfg.Name, path.Join(b.extractFolder(), configName), cfg.Algorithm)
		if err != nil {
			return nil, err
		}
		sums = append(sums, sum)
	}

	return sums, nil
}

// fetchAndHash downloads one archive or URL binary for this platform and hashes it.
func (b *baseInstaller) fetchAndHash(section, nameTmpl, urlTmpl string, pc PlatformChecksum) (ComputedChecksum, error) {
	downloadURL, dest, err := b.resolveDownload(nameTmpl, urlTmpl)
	if err != nil {
		return ComputedChecksum{}, err
	}
	checksum, err := b.platformChecksum(pc)
	if err != nil {
		return ComputedChecksum{}, err
	}
	if err := b.download(downloadURL, dest); err != nil {
		return ComputedChecksum{}, err
	}
	return b.hash(section, nameTmpl, dest, checksum.Algorithm)
}

// download fetches url to dest, retrying like DownloadAndVerify does.
func (b *baseInstaller) download(url, dest string) error {
	attempts := max(b.downloader.maxAttempts, 1)
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = b.downloader.Download(url, dest); err == nil {
			return nil
		}
		if attempt < attempts {
			b.downloader.sleepBackoff(attempt)
		}
	}
	return err
}

func (b *baseInstaller) hash(section, name, file, algorithm string) (ComputedChecksum, error) {
	if !IsSupportedAlgorithm(algorithm) {
		return ComputedChecksum{}, errorx.IllegalFormat.New(
			"%s %s %s: algorithm %q is not supported; set it in the catalog", b.software.Name, b.versionToBeInstalled, name, algorithm)
	}
	if _, err := os.Stat(file); err != nil {
		return ComputedChecksum{}, NewFileNotFoundError(file)
	}
	value, err := fileDigest(file, algorithm)
	if err != nil {
		return ComputedChecksum{}, err
	}
	return ComputedChecksum{Section: section, Name: name, Algorithm: algorithm, Value: value}, nil
}
