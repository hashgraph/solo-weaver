package backup

import (
	"fmt"
	erx "github.com/joomcode/errorx"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/security/principal"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const backupRegexTemplate = "%s-([0-9]+T[0-9]+)"
const backupTimeFormat = "20060102T150405"

type unixManager struct {
	pm     principal.Manager
	fs     fsx.Manager
	cfg    *Backup
	logger *zerolog.Logger
}

type Option func(*unixManager) error

func NewManager(opts ...Option) (Manager, error) {
	l := zerolog.Nop()
	manager := &unixManager{
		logger: &l,
	}

	for _, opt := range opts {
		if err := opt(manager); err != nil {
			return nil, err
		}
	}

	if manager.pm == nil {
		pm, err := principal.NewManager()
		if err != nil {
			return nil, err
		}
		manager.pm = pm
	}

	if manager.fs == nil {
		m, err := fsx.NewManager(fsx.WithPrincipalManager(manager.pm))
		if err != nil {
			return nil, err
		}
		manager.fs = m
	}

	if manager.cfg == nil {
		manager.cfg = &Backup{
			Pruning: Pruning{
				MaxAge:    -1,
				MaxCopies: -1,
			},
			Snapshots: Snapshot{
				Rules: SnapshotRules{
					Creation: SnapshotRuleSet{
						Include: []string{},
						Exclude: []string{},
					},
					Subsequent: SnapshotRuleSet{
						Include: []string{},
						Exclude: []string{},
					},
				},
			},
		}
	}

	return manager, nil
}

func WithPrincipalManager(pm principal.Manager) Option {
	return func(manager *unixManager) error {
		if pm == nil {
			return erx.IllegalArgument.New("principal manager cannot be nil")
		}

		manager.pm = pm
		return nil
	}
}

func WithFileSystemManager(fs fsx.Manager) Option {
	return func(manager *unixManager) error {
		if fs == nil {
			return erx.IllegalArgument.New("fs manager cannot be nil")
		}

		manager.fs = fs
		return nil
	}
}

func WithConfiguration(cfg *Backup) Option {
	return func(manager *unixManager) error {
		if cfg == nil {
			return erx.IllegalArgument.New("config cannot be nil")
		}

		manager.cfg = cfg
		return nil
	}
}

func WithLogger(logger *zerolog.Logger) Option {
	return func(manager *unixManager) error {
		if logger != nil {
			manager.logger = logger
		}
		return nil
	}
}

func (m *unixManager) IsVersioned(targetPath string) (bool, error) {
	v, err := m.CurrentVersion(targetPath)
	return err == nil && v != nil, err
}

func (m *unixManager) CurrentVersion(targetPath string) (*Version, error) {
	vi, err := m.extractCurrentVersionInfo(targetPath)
	if err != nil {
		return nil, err
	}

	err = m.extractVersionDate(vi)
	if err != nil {
		return nil, err
	}

	return vi, nil
}

func (m *unixManager) EnumerateVersions(targetPath string) ([]*Version, error) {
	cvi, err := m.extractCurrentVersionInfo(targetPath)
	if err != nil {
		return nil, err
	}

	err = m.extractVersionDate(cvi)
	if err != nil {
		return nil, err
	}

	dl, err := os.ReadDir(cvi.RootPath)
	if err != nil {
		return nil, err
	}

	versions := make([]*Version, 0)
	versions = append(versions, cvi)

	for _, de := range dl {
		date, err := m.parseFileDate(cvi.Name, de.Name())
		if err != nil {
			continue
		}

		path := filepath.Join(cvi.RootPath, de.Name())
		vi := &Version{
			RootPath: cvi.RootPath,
			Name:     cvi.Name,
			Date:     time.Date(date.Year(), date.Month(), date.Day(), date.Hour(), date.Minute(), date.Second(), 0, time.UTC),
			Path:     path,
			IsActive: false,
		}

		if filepath.Base(vi.Path) != filepath.Base(cvi.Path) {
			versions = append(versions, vi)
		}
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Date.Before(versions[j].Date)
	})

	return versions, nil
}

func (m *unixManager) CreateVersion(targetPath string) (*Version, error) {
	return m.createVersionInternal(targetPath, time.Now(), false)
}

func (m *unixManager) CreateVersionAt(targetPath string, date time.Time) (*Version, error) {
	return m.createVersionInternal(targetPath, date, true)
}

func (m *unixManager) DeleteVersion(version *Version) error {
	if version == nil {
		return erx.IllegalArgument.New("the version argument must not be nil")
	}

	cvi, err := m.extractCurrentVersionInfo(filepath.Join(version.RootPath, version.Name))
	if err != nil {
		return err
	}

	if cvi.Path == version.Path {
		return erx.IllegalArgument.New("the version to delete is the current version")
	}

	_, exists, err := m.fs.PathExists(version.Path)
	if err != nil {
		return err
	}

	if !exists {
		return nil
	}

	if m.fs.IsDirectory(version.Path) {
		return os.RemoveAll(version.Path)
	} else if m.fs.IsRegularFile(version.Path) {
		return os.Remove(version.Path)
	}

	return fsx.NewFileTypeError(nil, fsx.FileOrDirectory, version.Path)
}

func (m *unixManager) extractCurrentVersionInfo(targetPath string) (*Version, error) {
	if !m.fs.IsSymbolicLink(targetPath) {
		return nil, NewNotVersionedError(nil, targetPath)
	}

	parentDir := filepath.Dir(targetPath)
	if !m.fs.IsDirectory(parentDir) {
		return nil, fsx.NewFileTypeError(nil, fsx.Directory, parentDir)
	}

	linkTarget, err := os.Readlink(targetPath)
	if err != nil {
		return nil, fsx.NewFileSystemError(err, "failed to read symbolic link target", targetPath)
	}

	_, ok, err := m.fs.PathExists(linkTarget)
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, fsx.NewFileNotFoundError(nil, linkTarget)
	}

	targetName := filepath.Base(targetPath)
	return &Version{
		RootPath: parentDir,
		Name:     targetName,
		Path:     linkTarget,
		IsActive: true,
	}, nil
}

func (m *unixManager) extractVersionDate(vi *Version) error {
	date, err := m.parseFileDate(vi.Name, vi.Path)
	if err != nil {
		return erx.IllegalArgument.New("the symbolic link target is not a valid backup version: %s", vi.Path)
	}

	vi.Date = *date
	return nil
}

func (m *unixManager) parseFileDate(prefix string, fileName string) (*time.Time, error) {
	exp, err := regexp.Compile(fmt.Sprintf(backupRegexTemplate, regexp.QuoteMeta(prefix)))
	if err != nil {
		return nil, err
	}

	groups := exp.FindStringSubmatch(fileName)
	if groups == nil || len(groups) != 2 {
		return nil, erx.IllegalArgument.New("the file or directory name is not a valid backup version: %s", fileName)
	}

	dateStr := groups[1]
	date, err := time.Parse(backupTimeFormat, dateStr)
	if err != nil {
		return nil, erx.IllegalArgument.New("the file or directory name contains an invalid datetime: %s", fileName)
	}

	return &date, nil
}

func (m *unixManager) createBackupPath(cv *Version, t time.Time, exactTimeRequired bool) (string, *time.Time, error) {
	for i := 0; i < 10; i++ {
		ft := t
		dateStr := ft.Format(backupTimeFormat)
		softwareFolder := filepath.Base(cv.RootPath)
		backupPath := filepath.Join(cv.RootPath, fmt.Sprintf("%s-%s", cv.Name, dateStr))
		backupFolder := filepath.Base(backupPath)

		m.logger.Debug().
			Str(logFields.timestamp, dateStr).
			Str(logFields.softwareFolder, softwareFolder).
			Str(logFields.toolsFolder, backupFolder).
			Msg("Naming: New Timestamp & Folder ConfigNames Registered Successfully")

		_, exists, err := m.fs.PathExists(backupPath)
		if err != nil {
			return "", nil, err
		}

		if !exists {
			return backupPath, &ft, nil
		} else if exactTimeRequired {
			return "", nil, erx.IllegalArgument.New("the requested time is already taken: %s", t)
		}

		// If the originally requested time is already taken, we try to find a free time slot by adding a second to the time
		ft = ft.Add(time.Duration(i) * time.Second)
		time.Sleep(time.Duration(25) * time.Millisecond)
	}

	return "", nil, erx.IllegalArgument.New("unable to create a unique backup path: %s", t)
}

func (m *unixManager) cloneFileWithAccess(src string, dst string) error {
	return m.cloneEntity(src, dst, fsx.File, m.fs.IsRegularFileByFileInfo, func(src string, dst string) error {
		return m.fs.CopyFile(src, dst, true)
	})
}

func (m *unixManager) cloneDirectoryWithAccess(src string, dst string) error {
	return m.cloneEntity(src, dst, fsx.Directory, m.fs.IsDirectoryByFileInfo, func(src string, dst string) error {
		return m.fs.CreateDirectory(dst, true)
	})
}

func (m *unixManager) cloneSymlinkWithAccess(src string, dst string) error {
	return m.cloneEntity(src, dst, fsx.Symlink, m.fs.IsSymbolicLinkByFileInfo, func(src string, dst string) error {

		// Find the immediate target of the symlink instead of expanding multiple levels of symlinks.
		// This is because expanding multiple levels of symlinks can result in a version which is not a copy of the
		// original and may still point to the original. For example this ensures relative symlink like below is
		// handled correctly: `/solo-provisioner/bin/node-mgmt-tool -> node-mgmt-tool`
		linkTarget, err := os.Readlink(src)
		if err != nil {
			return err
		}

		return m.fs.CreateSymbolicLink(linkTarget, dst, true)
	})
}

func (m *unixManager) cloneHardlinkWithAccess(src string, dst string) error {
	return m.cloneEntity(src, dst, fsx.Hardlink, m.fs.IsHardLinkByFileInfo, func(src string, dst string) error {
		return m.fs.CreateHardLink(src, dst, true)
	})
}

func (m *unixManager) cloneEntity(src string, dst string, entityType fsx.ExpectedFileType, typeCheckFn func(fi os.FileInfo) bool, createFn func(src string, dst string) error) error {
	// Ensure the source exists and is of the required type
	sfi, exists, err := m.fs.PathExists(src)
	if err != nil {
		return fsx.NewFileSystemError(err, "invalid source path", src)
	}

	if !exists {
		return fsx.NewFileNotFoundError(nil, src)
	}

	if !typeCheckFn(sfi) {
		return fsx.NewFileTypeError(nil, entityType, src)
	}

	// Check to see if the destination exists.
	dfi, exists, err := m.fs.PathExists(dst)
	if err != nil {
		return fsx.NewFileSystemError(err, "invalid destination path", dst)
	}

	// If the destination exists and is not of the required type, we return an error. If it does not exist, we create it.
	if exists && !typeCheckFn(dfi) {
		return fsx.NewFileTypeError(nil, entityType, dst)
	} else if !exists {
		err = createFn(src, dst)
		if err != nil {
			return err
		}
	}

	// Retrieve the owner and permissions of the source
	user, group, err := m.fs.ReadOwner(src)
	if err != nil {
		return err
	}

	perms, err := m.fs.ReadPermissions(src)
	if err != nil {
		return err
	}

	// Apply the source's owner and permissions to the destination
	if err = m.fs.WriteOwner(dst, user, group, false); err != nil {
		return err
	}

	if err = m.fs.WritePermissions(dst, perms, false); err != nil {
		return err
	}

	return nil
}

func (m *unixManager) cloneDirectoryRecursive(src string, dst string, filter Filter) error {
	sfi, exists, err := m.fs.PathExists(src)
	if err != nil {
		return fsx.NewFileSystemError(err, "invalid source path", src)
	}

	if !exists {
		return fsx.NewFileNotFoundError(nil, src)
	}

	if !m.fs.IsDirectoryByFileInfo(sfi) {
		return fsx.NewFileTypeError(nil, fsx.Directory, src)
	}

	dfi, exists, err := m.fs.PathExists(dst)
	if err != nil {
		return fsx.NewFileSystemError(err, "invalid destination path", dst)
	}

	if exists && !m.fs.IsDirectoryByFileInfo(dfi) {
		return fsx.NewFileTypeError(nil, fsx.Directory, dst)
	} else if !exists {
		err = filter.Apply(src, dst, m.cloneDirectoryWithAccess)
		if err != nil {
			return err
		}
	}

	cleanSrc := filepath.Clean(src)
	cleanDst := filepath.Clean(dst)

	m.logger.Debug().
		Str(logFields.srcPath, cleanSrc).
		Str(logFields.destPath, cleanDst).
		Msg("Snapshot: Executing Copy Tree (CTREE)")

	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		return m.handleWalkDir(cleanSrc, cleanDst, path, d, err, filter)
	})

	if err != nil {
		return fsx.NewFileSystemError(err, "unable to walk source directory", src)
	}

	return nil
}

func (m *unixManager) handleWalkDir(srcRoot string, dstRoot string, path string, d fs.DirEntry, err error, filter Filter) error {
	if err != nil {
		return err
	}

	fi, err := d.Info()
	if err != nil {
		return err
	}

	// Get the relative path of the file rooted at the source directory
	relPath, err := filepath.Rel(srcRoot, path)
	if err != nil {
		return fsx.NewFileSystemError(err, "unable to determine relative path", path)
	}

	var operator CloneOp
	var logMessage string
	if m.fs.IsSymbolicLinkByFileInfo(fi) {
		operator = m.cloneSymlinkWithAccess
		logMessage = "CTREE: Copying Symbolic Link"
	} else if m.fs.IsHardLinkByFileInfo(fi) {
		operator = m.cloneHardlinkWithAccess
		logMessage = "CTREE: Copying Hard Link"
	} else if m.fs.IsDirectoryByFileInfo(fi) {
		operator = m.cloneDirectoryWithAccess
		logMessage = "CTREE: Copying Directory"
	} else if m.fs.IsRegularFileByFileInfo(fi) {
		operator = m.cloneFileWithAccess
		logMessage = "CTREE: Copying File"
	} else {
		return fsx.NewFileSystemError(nil, "unsupported file type", path)
	}

	fullPath := filepath.Join(dstRoot, relPath)
	m.logger.Debug().
		Str(logFields.relativePath, relPath).
		Str(logFields.backupPath, fullPath).
		Msg(logMessage)
	return filter.Apply(path, fullPath, operator)
}

func (m *unixManager) createNewVersion(cvi *Version, t time.Time, exactTimeRequired bool, filter Filter, op func(string, string, Filter) error) (*Version, error) {
	m.logger.Debug().
		Str(logFields.srcPath, cvi.Path).
		Str(logFields.backupRootPath, cvi.RootPath).
		Msg("Naming: Updating Current Backup Timestamp & Folder ConfigNames")

	backupPath, ft, err := m.createBackupPath(cvi, t, exactTimeRequired)
	if err != nil {
		m.logger.Error().
			Str(logFields.srcPath, cvi.Path).
			Str(logFields.backupRootPath, cvi.RootPath).
			Err(err).
			Msg("Snapshot: Failure Preparing Backup Path")

		return nil, err
	}

	snapshotName := filepath.Base(backupPath)
	m.logger.Debug().
		Str(logFields.snapshotName, snapshotName).
		Str(logFields.folder, cvi.Path).
		Msg("Snapshot: Enabling Snapshot Support (Folder)")

	fvi := &Version{
		RootPath: cvi.RootPath,
		Name:     cvi.Name,
		Path:     backupPath,
		Date:     time.Date(ft.Year(), ft.Month(), ft.Day(), ft.Hour(), ft.Minute(), ft.Second(), 0, time.UTC),
		IsActive: false,
	}

	err = op(cvi.Path, backupPath, filter)
	if err != nil {
		return fvi, err
	}

	link := filepath.Join(cvi.RootPath, cvi.Name)
	if err = m.fs.CreateSymbolicLink(backupPath, link, true); err != nil {
		m.logger.Error().
			Str(logFields.symlinkPath, filepath.Join(cvi.RootPath, cvi.Name)).
			Str(logFields.backupPath, backupPath).
			Err(err).
			Msg("Snapshot: Failure Creating New Symbolic Link")

		return fvi, err
	}

	fvi.IsActive = true

	m.logger.Debug().
		Str(logFields.link, link).
		Str(logFields.target, fvi.Path).
		Msg("Symlink: Created Symbolic Link Successfully")

	m.logger.Info().
		Str(logFields.snapshotName, snapshotName).
		Str(logFields.folder, cvi.Path).
		Msg("Snapshot: Enabled Snapshot Support Successfully")

	return fvi, nil
}

// enableSnapshotSupports is a helper method to create a mock Version entry from the targetPath without actually
// creating a versioned path.
//
// It sets Version.IsActive field false to denote that version is not active yet.
//
// This is meant to be a helper function to be used in createVersionInternal method where actual versioned path will be
// created.
func (m *unixManager) enableSnapshotSupport(targetPath string, t time.Time) (*Version, error) {
	_, ok, err := m.fs.PathExists(targetPath)
	if err != nil || !ok {
		return nil, erx.IllegalArgument.New("the target path does not exist: %s", targetPath)
	}

	parentDir := filepath.Dir(targetPath)
	if !m.fs.IsDirectory(parentDir) {
		return nil, erx.IllegalArgument.New("the parent directory does not exist: %s", targetPath)
	}

	targetName := filepath.Base(targetPath)

	cvi := &Version{
		RootPath: parentDir,
		Name:     targetName,
		Path:     targetPath,
		Date:     t,
		IsActive: false, // set false to denote it is not active yet
	}

	return cvi, nil
}

func (m *unixManager) createVersionInternal(targetPath string, t time.Time, exactTimeRequired bool) (*Version, error) {
	var cvi *Version
	var err error
	var filter Filter

	if !m.fs.IsSymbolicLink(targetPath) {
		m.logger.Debug().
			Str(logFields.srcPath, targetPath).
			Msg("Snapshot: Source Path Is Not A Symbolic Link")

		cvi, err = m.enableSnapshotSupport(targetPath, t)
		if err != nil {
			m.logger.Error().
				Str(logFields.srcPath, targetPath).
				Err(err).
				Msg("Snapshot: Failure Enabling Snapshot Support For Source Path")

			return nil, erx.IllegalArgument.
				New("failed to enable snapshot support to the target path: %s", targetPath).
				WithUnderlyingErrors(err)
		}

		filter = NewFilter(m.cfg.Snapshots.Rules.Creation)

	} else {
		cvi, err = m.CurrentVersion(targetPath)

		if err != nil {
			m.logger.Error().
				Str(logFields.srcPath, targetPath).
				Err(err).
				Msg("Snapshot: Failure Resolving Source Path Version")
			return nil, err
		}

		filter = NewFilter(m.cfg.Snapshots.Rules.Subsequent)
	}

	var operator func(src string, dst string, filter Filter) error
	if m.fs.IsRegularFile(cvi.Path) {
		m.logger.Debug().
			Str(logFields.srcPath, cvi.Path).
			Msg("Snapshot: Source Path Is A Regular File")

		operator = func(src string, dst string, filter Filter) error {
			return filter.Apply(src, dst, m.cloneFileWithAccess)
		}
	} else if m.fs.IsDirectory(cvi.Path) {
		operator = m.cloneDirectoryRecursive
	}

	if operator == nil {
		return nil, fsx.NewFileTypeError(nil, fsx.FileOrDirectory, targetPath)
	}

	return m.createNewVersion(cvi, t, exactTimeRequired, filter, operator)
}

func (m *unixManager) CopyTree(src string, dst string) error {
	return m.cloneDirectoryRecursive(src, dst, &noFilter{})
}

func (m *unixManager) CopyTreeByFilter(src string, dst string, filter Filter) error {
	return m.cloneDirectoryRecursive(src, dst, filter)
}
