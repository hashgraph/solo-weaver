package software

import "github.com/bluet/syspkg"

type Package interface {
	Name() string
	Install() (*syspkg.PackageInfo, error)
	Uninstall() (*syspkg.PackageInfo, error)
	Upgrade() (*syspkg.PackageInfo, error)
	Info() (*syspkg.PackageInfo, error)
	Verify() error
	IsInstalled() bool
}

// Downloader interface for downloading and verifying software packages
type Downloader interface {
	Download(url, destination string) error
	VerifyMD5(filePath, expectedMD5 string) error
	VerifySHA256(filePath, expectedSHA256 string) error
	VerifySHA512(filePath, expectedSHA512 string) error
	ExtractTarGz(gzPath, destDir string) error
	ExtractZip(zipPath, destDir string) error
}

type Software interface {
	Download() error

	Extract() error

	Install() error

	Verify() error

	IsInstalled() (bool, error)

	Configure() error

	IsConfigured() (bool, error)
}
