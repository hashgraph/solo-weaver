package software

import "github.com/bluet/syspkg"

type Package interface {
	Name() string

	Install() (*syspkg.PackageInfo, error)

	Uninstall() (*syspkg.PackageInfo, error)

	Upgrade() (*syspkg.PackageInfo, error)

	Info() (*syspkg.PackageInfo, error)

	IsInstalled() bool
}

type Software interface {
	// Download fetches the software artifacts,
	// including the binary, and configuration files
	Download() error

	// Extract unpacks the downloaded files under a temporary subdirectory called 'unpack'
	Extract() error

	// Install places the files in the sandbox destination
	Install() error

	// Uninstall removes the software from the sandbox and cleans up related files
	Uninstall() error

	// IsInstalled checks the directories and high-level contents in sandbox
	IsInstalled() (bool, error)

	// Configure sets up configurations, services, and related symlinks
	// It also fills the configuration files
	Configure() error

	// IsConfigured checks if the configuration has been done
	IsConfigured() (bool, error)

	// Version returns the version of the software to be installed
	Version() string

	// Cleanup removes temporary files created during download and extraction
	Cleanup() error
}
