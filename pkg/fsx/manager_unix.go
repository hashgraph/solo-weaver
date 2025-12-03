// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package fsx

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/pkg/security"
	"golang.hedera.com/solo-weaver/pkg/security/principal"
)

const (
	// DefaultFileMode is the default file mode used when creating files.
	defaultFileMode = 0644
	// DefaultDirectoryMode is the default directory mode used when creating directories.
	defaultDirectoryMode = 0755
	// DefaultROPermissions is the default permissions used when creating read-only files and directories.
	// TODO - set to 0500 for testing, bash version sets it to 0400 which can only be done with sudo as-is
	DefaultROPermissions = 0500
)

type Option func(*unixManager) error

type unixManager struct {
	pm principal.Manager
}

func NewManager(opts ...Option) (Manager, error) {
	manager := &unixManager{}

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

	return manager, nil
}

func WithPrincipalManager(pm principal.Manager) Option {
	return func(manager *unixManager) error {
		manager.pm = pm
		return nil
	}
}

func (m *unixManager) PathExists(path string) (os.FileInfo, bool, error) {
	pi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}

		return nil, false, err
	}

	return pi, true, nil
}

func (m *unixManager) IsRegularFile(path string) bool {
	pi, exists, err := m.PathExists(path)
	if err != nil || !exists {
		return false
	}

	return m.IsRegularFileByFileInfo(pi)
}

func (m *unixManager) IsRegularFileByFileInfo(fi os.FileInfo) bool {
	return fi.Mode().IsRegular()
}

func (m *unixManager) IsDirectory(path string) bool {
	pi, exists, err := m.PathExists(path)
	if err != nil || !exists {
		return false
	}

	return m.IsDirectoryByFileInfo(pi)
}

func (m *unixManager) IsDirectoryByFileInfo(fi os.FileInfo) bool {
	return fi.Mode().IsDir()
}

func (m *unixManager) IsHardLink(path string) bool {
	pi, exists, err := m.PathExists(path)
	if err != nil || !exists {
		return false
	}

	return m.IsHardLinkByFileInfo(pi)
}

func (m *unixManager) IsHardLinkByFileInfo(fi os.FileInfo) bool {
	if s, ok := fi.Sys().(*syscall.Stat_t); m.IsRegularFileByFileInfo(fi) && ok {
		return s.Nlink > 1
	}

	return false
}

func (m *unixManager) IsSymbolicLink(path string) bool {
	pi, exists, err := m.PathExists(path)
	if err != nil || !exists {
		return false
	}

	return m.IsSymbolicLinkByFileInfo(pi)
}

func (m *unixManager) IsSymbolicLinkByFileInfo(fi os.FileInfo) bool {
	return fi.Mode()&os.ModeSymlink != 0
}

func (m *unixManager) CreateDirectory(path string, recursive bool) error {
	var err error

	_, exists, err := m.PathExists(path)
	if err != nil {
		return FileSystemError.New("invalid path %q", path).WithUnderlyingErrors(err)
	}

	if exists {
		return nil
	}

	parentDir := filepath.Dir(path)
	pfi, exists, err := m.PathExists(parentDir)
	if err != nil {
		return FileSystemError.
			New("parent directory is not a valid path %q", parentDir).
			WithUnderlyingErrors(err)
	}

	if exists && !pfi.Mode().IsDir() {
		retError := false

		// for a symbolic link read info about the target and check if that's a directory
		if m.IsSymbolicLink(parentDir) {
			pfi2, err := os.Stat(parentDir)
			if err != nil || !pfi2.Mode().IsDir() {
				retError = true
			}
		}

		if retError {
			return FileTypeError.New("parent path %q is not a directory", parentDir)
		}
	} else if !exists && !recursive {
		return FileNotFound.New("parent path %q not found", parentDir)
	}

	if recursive {
		err = os.MkdirAll(path, defaultDirectoryMode)
	} else {
		err = os.Mkdir(path, defaultDirectoryMode)
	}

	if err != nil {
		return FileSystemError.New("failed to create a directory %q", path).WithUnderlyingErrors(err)
	}

	return nil
}

func (m *unixManager) CopyFile(src string, dst string, overwrite bool) error {
	// Ensure src exists and is a file
	sfi, exists, err := m.PathExists(src)
	if err != nil || !exists {
		return FileNotFound.New("source file %q not found", src).WithUnderlyingErrors(err)
	}

	if !sfi.Mode().IsRegular() {
		return errorx.IllegalArgument.New("source path is not a file: %s", src)
	}

	// Check to see if dst exists
	dfi, exists, err := m.PathExists(dst)
	if err != nil {
		return FileSystemError.New("destination path is not a valid path: %s", dst).WithUnderlyingErrors(err)
	}

	// If dst exists and is the same file as src, return
	if os.SameFile(sfi, dfi) {
		return nil
	}

	var dstParent, dstFileName string

	if exists {
		// If dst exists as a file and overwrite is not enabled, return an error
		if dfi.Mode().IsRegular() && !overwrite {
			return FileAlreadyExists.New("destination file %q already exists, overwrite is disabled.", dst)
		}

		if dfi.Mode().IsRegular() {
			// if dst exists as a file and overwrite is enabled, overwrite the file.
			dstParent = filepath.Dir(dst)
			dstFileName = filepath.Base(dst)
		} else if dfi.Mode().IsDir() {
			// if dst exists as a directory, copy the file into the directory.
			dstParent = dst
			dstFileName = filepath.Base(src)
		} else if dfi.Mode()&os.ModeSymlink != 0 {
			// if dst exists as a symlink, remove the symlink and copy the file.
			if err := os.Remove(dst); err != nil {
				return FileSystemError.New("failed to remove symlink %q", dst)
			}
		} else {
			// if dst exists as something else, return an error.
			return FileAlreadyExists.New("destination path %q already exists and is not a file or directory", dst)
		}
	} else {
		// If dst does not exist, create the file.
		dstParent = filepath.Dir(dst)
		dstFileName = filepath.Base(dst)
	}

	// Ensure dstParent exists and is a directory
	info, exists, err := m.PathExists(dstParent)
	if err != nil {
		return FileSystemError.New("destination parent path is not a valid path: %s", dstParent).WithUnderlyingErrors(err)
	} else if !exists {
		return FileNotFound.New("destination parent path %q not found", dstParent)
	} else if !info.Mode().IsDir() {
		return FileSystemError.New("destination parent path %q is not a directory", dstParent)
	}

	return copyFileContents(src, filepath.Join(dstParent, dstFileName))
}

func (m *unixManager) CreateSymbolicLink(src string, dst string, overwrite bool) error {
	sfi, exists, err := m.PathExists(src)
	if err != nil {
		return FileNotFound.New("source file %q not found", src)
	}

	brokenLink := false
	if !exists {
		// This could be a relative symlink, so we need to check the relative to the parent directory of the dst
		parentDir := filepath.Dir(dst)
		sfi, exists, err = m.PathExists(filepath.Join(parentDir, src))

		if err != nil || !exists {
			// This is possibly a request to create a broken symlink, so we'll just create it and return
			brokenLink = true
		}
	}

	if !brokenLink {
		if !sfi.Mode().IsRegular() && !sfi.Mode().IsDir() {
			return FileTypeError.New("source file %q is not a directory", src)
		}
	}

	if err = m.checkAndOverwritePath(dst, overwrite); err != nil {
		return err
	}

	if err = os.Symlink(src, dst); err != nil {
		return FileSystemError.New("failed to create symlink: %s", dst).WithUnderlyingErrors(err)
	}

	return nil
}

func (m *unixManager) CreateHardLink(src string, dst string, overwrite bool) error {
	sfi, exists, err := m.PathExists(src)
	if err != nil || !exists {
		return FileNotFound.New("source file %q not found", src)
	}

	if !sfi.Mode().IsRegular() {
		return FileTypeError.New("source path %q is not a regular file", src)
	}

	if err = m.checkAndOverwritePath(dst, overwrite); err != nil {
		return err
	}

	if err = os.Link(src, dst); err != nil {
		return FileSystemError.New("failed to create hard link: %s", dst).WithUnderlyingErrors(err)
	}

	return nil
}

func (m *unixManager) ReadOwner(path string) (principal.User, principal.Group, error) {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, FileSystemError.New("failed to stat path: %s", path).WithUnderlyingErrors(err)
	}

	var uid string
	var gid string
	if stat, ok := fileInfo.Sys().(*syscall.Stat_t); ok {
		uid = strconv.FormatUint(uint64(stat.Uid), 10)
		gid = strconv.FormatUint(uint64(stat.Gid), 10)
	} else {
		return nil, nil, FileSystemError.New("error getting file owner and group: %s", path)
	}

	user, err := m.pm.LookupUserById(uid)
	if err != nil {
		return nil, nil, err
	}

	group, err := m.pm.LookupGroupById(gid)
	if err != nil {
		return nil, nil, err
	}

	return user, group, nil
}

func (m *unixManager) ReadPermissions(path string) (fs.FileMode, error) {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return 0, FileSystemError.New("failed to stat path; %s", path).WithUnderlyingErrors(err)
	}

	return fileInfo.Mode().Perm(), nil
}

func (m *unixManager) WriteOwner(path string, user principal.User, group principal.Group, recursive bool) error {
	uid, err := strconv.Atoi(user.Uid())
	if err != nil {
		return errorx.IllegalArgument.New("UID must be an integer: %s", user.Uid())
	}

	gid, err := strconv.Atoi(group.Gid())
	if err != nil {
		return errorx.IllegalArgument.New("GID must be an integer: %s", group.Gid())
	}

	if m.IsSymbolicLink(path) {
		err = os.Lchown(path, uid, gid)
	} else {
		err = os.Chown(path, uid, gid)
	}

	if err != nil {
		return NewOwnershipChangeError(err, path, user, group, recursive)
	}

	if recursive {
		stat, err := os.Lstat(path)
		if err != nil {
			return FileSystemError.New("failed to stat path: %s", path).WithUnderlyingErrors(err)
		}

		if stat.IsDir() {
			err = filepath.WalkDir(path, func(nameAndPath string, d fs.DirEntry, err error) error {
				if err == nil {
					if m.IsSymbolicLink(nameAndPath) {
						err = os.Lchown(nameAndPath, uid, gid)
					} else {
						err = os.Chown(nameAndPath, uid, gid)
					}
				}

				return err
			})

			if err != nil {
				return NewOwnershipChangeError(err, path, user, group, recursive)
			}
		}
	}

	return nil
}

// WritePermissions updates the permissions of the given path.
// If the path is a symbolic link, it skips updating the permission of the path.
func (m *unixManager) WritePermissions(path string, perms fs.FileMode, recursive bool) error {
	if m.IsSymbolicLink(path) {
		return nil // cannot change permission of a symlink
	}

	if err := os.Chmod(path, perms); err != nil {
		return NewPermissionChangeError(err, path, uint(perms), recursive)
	}

	if recursive {
		stat, err := os.Lstat(path)
		if err != nil {
			return FileSystemError.New("failed to stat path: %s", path).WithUnderlyingErrors(err)
		}

		if stat.IsDir() {
			err = filepath.WalkDir(path, func(nameAndPath string, d fs.DirEntry, err error) error {
				if err == nil && !m.IsSymbolicLink(nameAndPath) { // we cannot change permission of a symlink
					err = os.Chmod(nameAndPath, perms)
				}

				return err
			})

			if err != nil {
				return NewPermissionChangeError(err, path, uint(perms), recursive)
			}
		}
	}

	return nil
}

func (m *unixManager) ReadFile(path string, maxFileSize int64) ([]byte, error) {
	fileInfo, exists, err := m.PathExists(path)
	if err != nil || !exists {
		return nil, FileNotFound.New("path %q not found", path)
	}

	if maxFileSize > 0 && fileInfo.Size() > maxFileSize {
		return nil, errorx.IllegalArgument.New("file size is larger than %d bytes", maxFileSize)
	}

	if fileInfo.Size() <= 0 {
		return []byte{}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to open file at %q", path).WithUnderlyingErrors(err)
	}
	defer Close(file)

	buffer := make([]byte, fileInfo.Size())
	totalRead, err := io.ReadAtLeast(file, buffer, len(buffer))
	if err != nil {
		return nil, errorx.IllegalArgument.New("failed to read from file %q", path).WithUnderlyingErrors(err)
	}

	if totalRead != len(buffer) {
		return nil, errorx.IllegalArgument.
			New("failed to load full contents from file %q", path).
			WithUnderlyingErrors(err)
	}

	return buffer, nil
}

func (m *unixManager) setupAccessForServiceUser(path string) error {
	user, err := m.pm.LookupUserByName(security.ServiceAccountUserName())
	if err != nil {
		return errorx.IllegalArgument.
			New("failed to retrieve user %q", security.ServiceAccountUserName()).
			WithUnderlyingErrors(err)
	}

	group, err := m.pm.LookupGroupByName(security.ServiceAccountGroupName())
	if err != nil {
		return errorx.IllegalArgument.
			New("failed to retrieve group %q", security.ServiceAccountGroupName()).
			WithUnderlyingErrors(err)
	}

	err = m.WriteOwner(path, user, group, false)
	if err != nil {
		return errorx.IllegalArgument.
			New("failed to set file owner (%s) and group(%s) to path: %q", user.Name(), group.Name(), path).
			WithUnderlyingErrors(err)
	}

	err = m.WritePermissions(path, security.ACLFilePerms, false)
	if err != nil {
		return errorx.IllegalArgument.
			New("failed to set file permissions to %q", path).
			WithUnderlyingErrors(err)
	}

	return nil
}

func (m *unixManager) WriteFile(path string, payload []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return errorx.IllegalArgument.New("failed to open file at %q", path).WithUnderlyingErrors(err)
	}
	defer Close(file)

	n, err := file.Write(payload)
	if err != nil {
		return errorx.IllegalArgument.New("failed to write to file %q", path).WithUnderlyingErrors(err)
	}

	if n != len(payload) {
		return errorx.IllegalArgument.
			New("failed to write full payload to file %q", path).
			WithUnderlyingErrors(err)
	}

	// set file permission
	return m.setupAccessForServiceUser(path)
}

func (m *unixManager) checkAndOverwritePath(path string, overwrite bool) error {
	_, exists, err := m.PathExists(path)
	if err != nil {
		return FileSystemError.New("destination path is not a valid path: %s", path).WithUnderlyingErrors(err)
	}

	if exists {
		if overwrite {
			if err := os.Remove(path); err != nil {
				if err := os.RemoveAll(path); err != nil {
					return FileSystemError.
						New("failed to remove existing path: %s", path).
						WithUnderlyingErrors(err)
				}
			}
		} else {
			return FileAlreadyExists.New("destination path %q already exists, overwrite is disabled", path)
		}
	}

	return nil
}

func copyFileContents(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return FileSystemError.New("failed to open the source file: %s", src).WithUnderlyingErrors(err)
	}
	defer Close(srcFile)

	dstFile, err := os.Create(dst)
	if err != nil {
		return FileSystemError.New("failed to create the destination file: %s", dst).WithUnderlyingErrors(err)
	}
	defer Close(dstFile)

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return FileSystemError.New("failed to copy the file contents: %s", src).WithUnderlyingErrors(err)
	}

	err = dstFile.Sync()
	if err != nil {
		return FileSystemError.New("failed to sync the destination file: %s", dst).WithUnderlyingErrors(err)
	}

	return nil
}

func (m *unixManager) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func (m *unixManager) ExcludeFromPath(path string, exclusions []string) (string, error) {
	pathParts, finalPath := pathToComponents(path)

	lastFoundIndex := -1

	for i := len(pathParts) - 1; i >= 0; i-- {
		if pathParts[i] != "" && slices.Contains(exclusions, pathParts[i]) {
			// If we found an excluded folder, we will not include it in the final path
			lastFoundIndex = i
		}
	}

	if lastFoundIndex == 0 {
		return "", FileNotFound.New("entire path contained excluded folder names: %s", filepath.Clean(path))
	}

	finalPath = append(finalPath, pathParts[0:lastFoundIndex]...)
	return filepath.Join(finalPath...), nil
}

func (m *unixManager) FindParentPath(childPath string, parentDirName string) (string, error) {
	pathParts, finalPath := pathToComponents(childPath)

	found := false
	for i := len(pathParts) - 1; i >= 0; i-- {
		if pathParts[i] != "" && pathParts[i] == parentDirName {
			finalPath = append(finalPath, pathParts[0:i+1]...)
			found = true
			break
		}
	}

	if !found {
		return "", FileNotFound.New("no parent dir '%s' found in the given path '%s'",
			parentDirName, filepath.Clean(childPath))
	}

	return filepath.Join(finalPath...), nil
}

func pathToComponents(path string) (components []string, finalPathBase []string) {
	dir := filepath.Clean(path)
	components = strings.Split(dir, string(os.PathSeparator))

	if filepath.IsAbs(dir) {
		// if the path is absolute, we need to keep the first part as os.PathSeparator
		finalPathBase = []string{string(os.PathSeparator)}
	} else {
		finalPathBase = []string{}
	}

	return
}
