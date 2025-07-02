package backup

import (
	"time"
)

// Manager provides an operating system independent interface for backing up files and directories.
// Backups are performed by copying files and directories to a backup location and then creating a symbolic link.
// The current implementation only supports operating systems which support symbolic links (eg: Linux, Unix, Darwin,
// BSD, etc). This implies that Windows is not currently supported.
type Manager interface {
	// IsVersioned determines if the target path is a symbolic link which points to a versioned backup.
	IsVersioned(targetPath string) (bool, error)
	// CurrentVersion returns the current version of the target path. The target path must be the symbolic link
	// which points to the current version. The symbolic link must be located in the same directory as the versioned
	// backups. If the target path is not a symbolic link, then an error is returned.
	CurrentVersion(targetPath string) (*Version, error)
	// EnumerateVersions returns a list of versions for the target path. The target path must be the symbolic link
	// which points to the current version. The symbolic link must be located in the same directory as the versioned
	// backups. If the target path is not a symbolic link, then an error is returned.
	EnumerateVersions(targetPath string) ([]*Version, error)
	// CreateVersion creates a new version of the target path. The target path may a symbolic link, file, or directory.
	//
	// If the targetPath argument refers to a file or directory, then a new date/time based version will be created and a
	// symbolic link will replace the target path. The symbolic link will be updated to point to the new version. This
	// initial version will be the only version of the target path and will be the writable/active version.
	//
	// If the targetPath argument refers to a symbolic link, then the symbolic link will be followed and the target
	// of the symbolic link will be versioned. The symbolic link will be updated to point to the new version.
	CreateVersion(targetPath string) (*Version, error)
	// CreateVersionAt creates a new version of the target path with the specified date/time. This method behaves
	// identically to CreateVersion except in the case when a backup with the specified date/time already exists, then
	// an error will be returned.
	CreateVersionAt(targetPath string, date time.Time) (*Version, error)
	// DeleteVersion deletes the version of the target path. The target path must be the symbolic link which points
	// to the current version. The symbolic link must be located in the same directory as the versioned backups.
	//
	// If the version argument is the active version, then an error will be returned. If the version argument is the
	// only version, then an error will be returned. If the version argument is not the active version and there are
	// other versions, then the version will be deleted.
	DeleteVersion(version *Version) error
	// CopyTree copies the source directory to the destination directory. If the destination directory does not exist,
	// then it will be created. If the destination directory exists, then it will be overwritten.
	CopyTree(src string, dst string) error
	// CopyTreeByFilter copies the source directory to the destination directory. If the destination directory does not
	// exist, then it will be created. If the destination directory exists, then it will be overwritten.
	// The filter is applied to each file and directory before it is copied.
	CopyTreeByFilter(src string, dst string, filter Filter) error
}

// Version represents a versioned backup copy of a file or directory. Only a single version for a specific target path
// may be active at any given time. The active version is the version which is pointed to by the symbolic link.
// The active version is the only version which may be modified and is always writable. All other versions should be
// considered read-only.
type Version struct {
	// RootPath is the path to the directory which contains the symbolic link and the versioned backups.
	RootPath string
	// Name of the symbolic link. This is also the prefix used by all backup versions.
	Name string
	// Date indicates the date and time the version was created.
	Date time.Time
	// Path to this version.
	Path string
	// IsActive indicates if the version is the writable version.
	IsActive bool
}

// CloneOp defines a backup operation
type CloneOp = func(src string, dst string) error

// Filter exposes filter function to be applied while creating backup
type Filter interface {
	// Apply executes the CloneOp if it is allowed in the ruleset
	Apply(src string, dst string, op CloneOp) error
}
