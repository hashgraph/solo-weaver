/*
 * Copyright (C) 2021-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package fsx

import (
	"io/fs"
	"os"

	"golang.hedera.com/solo-provisioner/pkg/security/principal"
)

// Manager provides an operating system independent interface for managing files and directories.
type Manager interface {
	// PathExists determines if the source path exists. This method does not follow symlinks.
	PathExists(path string) (os.FileInfo, bool, error)
	// IsRegularFile returns true if the path is a regular file; otherwise, false is returned.
	IsRegularFile(path string) bool
	// IsRegularFileByFileInfo returns true if the file info is a regular file; otherwise, false is returned.
	IsRegularFileByFileInfo(fi os.FileInfo) bool
	// IsDirectory returns true if the path is a regular file; otherwise, false is returned.
	IsDirectory(path string) bool
	// IsDirectoryByFileInfo returns true if the file info is a directory; otherwise, false is returned.
	IsDirectoryByFileInfo(fi os.FileInfo) bool
	// IsHardLink returns true if the path is a hard link; otherwise, false is returned.
	IsHardLink(path string) bool
	// IsHardLinkByFileInfo returns true if the file info is a hard link; otherwise, false is returned.
	IsHardLinkByFileInfo(fi os.FileInfo) bool
	// IsSymbolicLink returns true if the path is a symbolic link; otherwise, false is returned.
	IsSymbolicLink(path string) bool
	// IsSymbolicLinkByFileInfo returns true if the file info is a symbolic link; otherwise, false is returned.
	IsSymbolicLinkByFileInfo(fi os.FileInfo) bool
	// CreateDirectory creates a directory at the path specified by the path argument.
	// If the path argument refers to an existing directory, then no action is taken and no error is returned.
	// If the path argument refers to an existing file, then an error is returned.
	// If the path argument refers to a non-existent parent path, then an error is returned unless
	// the recursive argument is true.
	CreateDirectory(path string, recursive bool) error
	// CopyFile copies a single file.
	// The src argument must reference an existing file. The dst argument may reference either a file or directory.
	//
	// If the dst argument refers to an existing directory, then the file will be copied into the directory with same
	// name as the original file.
	//
	// If the dst argument refers to an existing file, then the existing file will be replaced if the overwrite
	// argument is true; otherwise, an error will be returned.
	//
	// If the dst argument refers to a non-existent path (one which is not a file, directory, or link), then the
	// last element of the path will be used as the file name. The file will be rooted in the parent directory of the
	// last path element. If the parent directory does not exist, then an error will be returned.
	CopyFile(src string, dst string, overwrite bool) error
	// CreateSymbolicLink creates a symbolic link at the path specified by the dst argument which points to the file
	// or directory referenced by the src argument.
	//
	// If the src argument refers to a path which does not exist, then an error is returned. If the dst argument refers
	// to an existing file, directory, or link, then it will be deleted if the overwrite argument is true; otherwise, an
	// error will be returned.
	//
	// An error will be returned if the operating system or the destination file system does not support symbolic links.
	CreateSymbolicLink(src string, dst string, overwrite bool) error
	// CreateHardLink creates a hard link at the path specified by the dst argument which points to the file
	// or directory referenced by the src argument.
	//
	// If the src argument refers to a path which does not exist, then an error is returned. If the dst argument refers
	// to an existing file, directory, or link, then it will be deleted if the overwrite argument is true; otherwise, an
	// error will be returned.
	//
	// An error will be returned if the operating system or the destination file system does not support hard links.
	CreateHardLink(src string, dst string, overwrite bool) error
	// ReadOwner returns the owner and group of the file at the given path.
	ReadOwner(path string) (principal.User, principal.Group, error)
	// ReadPermissions returns the permissions of the file at the given path.
	ReadPermissions(path string) (fs.FileMode, error)
	// WriteOwner sets the owner and group of the file at the given path.
	WriteOwner(path string, user principal.User, group principal.Group, recursive bool) error
	// WritePermissions sets the permissions of the file at the given path.
	WritePermissions(path string, perms fs.FileMode, recursive bool) error
	// ReadFile reads whole file as long as it's size is less than the maxFileSize argument.
	// This helper method ensures we avoid reading a very large file accidentally.
	// A negative maxFileSize will disable the file size check.
	ReadFile(path string, maxFileSize int64) ([]byte, error)
	// WriteFile writes payload to a file.
	// After writing the file it also updates the permissions as required.
	// If a file exists at the path, it overwrites it with new contents.
	//
	// Caller should ensure payload is not too big such that ReadFile cannot read it because of the file size limit.
	WriteFile(path string, payload []byte) error

	// RemoveAll removes the path and its contents
	// It is a wrapper of os.RemoveAll. This interface exists to help us mock the functionality during tests.
	RemoveAll(path string) error
}
