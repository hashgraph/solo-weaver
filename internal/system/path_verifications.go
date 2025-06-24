/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package common

import (
	_ "embed"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	sysfs "io/fs"
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
)

// PathAction defines path action type
type PathAction string

// PathActionTypes encapsulates a list of action types for easier access and code readability
var PathActionTypes = struct {
	// CreateIfMissingAction defines a PathAction to create path if it is missing
	CreateIfMissing PathAction

	// IgnoreIfMissingAction defines a PathAction to ignore creation if the path is missing
	IgnoreIfMissing PathAction
}{
	PathAction("createIfMissing"),
	PathAction("ignoreIfMissing"),
}

// String returns the string value of PathAction
func (pa *PathAction) String() string {
	return string(*pa)
}

// PathVerification holds the paths and verification actions to be performed on them
type PathVerification struct {
	Path    string       `yaml:"path"`
	Actions []PathAction `yaml:"actions"`
	logger  *zerolog.Logger

	Owner string      `yaml:"owner"`
	Group string      `yaml:"group"`
	Mode  os.FileMode `yaml:"mode"`
	Type  string      `yaml:"type"`
}

func (p *PathVerification) IsDirectory() bool {
	return p.Type == "directory"
}

func (p *PathVerification) IsFile() bool {
	return p.Type == "file"
}

// HasAction returns true if the specified action exists for PathVerification
func (p *PathVerification) HasAction(action PathAction) bool {
	for _, a := range p.Actions {
		if a == action {
			return true
		}
	}

	return false
}

// ResolveTargetPath checks for symlink and returns the actual target path
func (p *PathVerification) ResolveTargetPath(fm fsx.Manager) (string, error) {
	var err error
	targetPath := p.Path

	if fm.IsSymbolicLink(targetPath) {
		p.logger.Debug().
			Str(logFields.path, targetPath).
			Msg("Path Check: Detected Symlink Path.")

		targetPath, err = p.ResolveSymlinkTarget()
		if err != nil {
			return "", errors.Wrapf(err, "failed to resolve symlink's target path")
		}

		p.logger.Debug().
			Str(logFields.path, p.Path).
			Str(logFields.targetPath, targetPath).
			Msg("Path Check: Attempting To Check Permission For Symlink's Target Path.")
	}

	return targetPath, nil
}

// ResolveSymlinkTarget returns target path of the symlink path
func (p *PathVerification) ResolveSymlinkTarget() (string, error) {
	targetPath, err := os.Readlink(p.Path)
	if err != nil {
		return "", err
	}

	// for a relative path, create absolute path
	targetPathDir, targetPathName := filepath.Split(targetPath)
	if targetPathDir == "" {
		targetPath = filepath.Join(filepath.Dir(p.Path), targetPathName)
	}

	return targetPath, nil
}

// GetPathInfo checks for path existences and returns its info if available
//
// It returns nil and an error if path does not exist or there is an error accessing it
func (p *PathVerification) GetPathInfo(targetPath string, fm fsx.Manager) (os.FileInfo, error) {
	fileInfo, exists, err := fm.PathExists(targetPath)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, fsx.NewFileNotFoundError(errors.Newf("path does not exist: %s", targetPath), targetPath) // bubble up error
	}

	return fileInfo, nil
}

// HasIncorrectPermission checks if the path and its contents has incorrect ownership or not.
//
// If the path is a directory, it performs the check recursively. If any of the content has wrong permission, it
// flags the directory path to have incorrect permission.
//
// If the path is a symlink, it attempts to check permission of the symlink's target. If the target is a directory,
// it performs recursive check for a directory path as usual.
//
// Note. It does not follow symlink among the directory contents since filepath.WalkDir does not support that.
func (p *PathVerification) HasIncorrectPermission(fm fsx.Manager) (ok bool, err error) {
	targetPath, err := p.ResolveTargetPath(fm)
	if err != nil {
		return true, errors.Wrapf(err, "failed to resolve target path")
	}

	pathInfo, err := p.GetPathInfo(targetPath, fm)
	if err != nil {
		if errors.Is(err, &fsx.FileNotFoundError{}) && p.HasAction(PathActionTypes.IgnoreIfMissing) {
			p.logger.Warn().
				Str(logFields.path, targetPath).
				Msg("Path Check: Unable To Detect Path Permissions. Ignoring As Specified.")
			return false, nil
		}

		return true, err
	}

	if pathInfo.Mode().Perm() != p.Mode {
		p.logger.Warn().
			Str(logFields.path, targetPath).
			Str(logFields.permission, pathInfo.Mode().String()).
			Msg("Path Check: Incorrect Path Permissions")
		return true, nil
	}

	if fm.IsDirectoryByFileInfo(pathInfo) {
		p.logger.Debug().
			Str(logFields.path, targetPath).
			Msg("Path Check: Checking Path Permission Of Directory and Its Contents")

		// check contents recursively
		foundIncorrect := false
		err = filepath.WalkDir(targetPath, func(path string, d sysfs.DirEntry, err error) error {
			if path == targetPath {
				return nil // the root directory is already checked, so skip
			}

			fileMode, err := fm.ReadPermissions(path)
			if err != nil {
				return fsx.NewFileSystemError(err, "failed to read permission", path)
			}

			if fileMode.Perm() != p.Mode {
				p.logger.Warn().
					Str(logFields.path, path).
					Str(logFields.permission, fileMode.String()).
					Msg("Path Check: Directory Content Has Incorrect Path Permissions")

				foundIncorrect = true
				return errors.Newf("permission does not match for %q", path)
			}

			p.logger.Debug().
				Str(logFields.path, path).
				Str(logFields.permission, fileMode.String()).
				Msg("Path Check: Matched Permission Of Directory Content")

			return nil
		})

		if err != nil && errors.Is(err, &fsx.FileSystemError{}) {
			return true, err // bubble up error
		}

		if foundIncorrect {
			p.logger.Warn().
				Str(logFields.path, targetPath).
				Msg("Path Check: Incorrect Permissions Detected For Directory And Its Contents")

			return true, nil
		}
	}

	// permission matched, so return 'false' and no-error
	p.logger.Info().
		Str(logFields.path, targetPath).
		Str(logFields.permission, pathInfo.Mode().String()).
		Msg("Path Check: Matched Path Permissions")

	return false, nil
}

// HasIncorrectOwnership checks if the path and its contents has incorrect ownership or not.
//
// If the path is a directory, it performs the check recursively. If any of the content has wrong ownership, it
// flags the directory to have incorrect ownership.
//
// If the path is a symlink, it attempts to check ownership of the symlink's target. If the target is a directory,
// it performs recursive check for a directory path as usual.
//
// Note. It does not follow symlink among the directory contents since filepath.WalkDir does not support that.
func (p *PathVerification) HasIncorrectOwnership(fm fsx.Manager) (ok bool, err error) {
	targetPath, err := p.ResolveTargetPath(fm)
	if err != nil {
		return true, errors.Wrapf(err, "failed to resolve target path")
	}

	pathInfo, err := p.GetPathInfo(targetPath, fm)
	if err != nil {
		if errors.Is(err, &fsx.FileNotFoundError{}) && p.HasAction(PathActionTypes.IgnoreIfMissing) {
			p.logger.Warn().
				Str(logFields.path, targetPath).
				Msg("Path Check: Unable To Detect Path Ownership. Ignoring As Specified.")

			return false, nil
		}

		return true, err
	}

	user, group, err := fm.ReadOwner(targetPath)
	if err != nil {
		return true, err
	}

	if user.Name() != p.Owner || group.Name() != p.Group {
		p.logger.Warn().
			Str(logFields.path, targetPath).
			Str(logFields.userName, user.Name()).
			Str(logFields.groupName, group.Name()).
			Msg("Path Check: Incorrect Path Ownership")
		return true, nil
	}

	if fm.IsDirectoryByFileInfo(pathInfo) {
		p.logger.Debug().
			Str(logFields.path, targetPath).
			Msg("Path Check: Checking Path Ownership Of Directory and Its Contents")

		// check contents recursively
		foundIncorrect := false
		err = filepath.WalkDir(targetPath, func(path string, d sysfs.DirEntry, err error) error {
			if path == targetPath {
				return nil // the root directory is already checked, so skip
			}

			user2, group2, err := fm.ReadOwner(path)
			if err != nil {
				return fsx.NewFileSystemError(err, "failed to read ownership", path)
			}

			if user2.Name() != p.Owner || group2.Name() != p.Group {
				foundIncorrect = true
				return errors.Newf("ownership does not match for %q", path)
			}

			p.logger.Debug().
				Str(logFields.path, path).
				Str(logFields.userName, user.Name()).
				Str(logFields.groupName, group.Name()).
				Msg("Path Check: Matched Ownership Of Directory Content")

			return nil
		})

		if err != nil && errors.Is(err, &fsx.FileSystemError{}) {
			return true, err // bubble up error
		}

		if foundIncorrect {
			p.logger.Warn().
				Str(logFields.path, targetPath).
				Msg("Path Check: Incorrect Permissions Detected For Directory And Its Contents")

			return true, nil
		}
	}

	// ownership matched, so return 'false' and no-error
	p.logger.Info().
		Str(logFields.path, targetPath).
		Str(logFields.userName, user.Name()).
		Str(logFields.groupName, group.Name()).
		Msg("Path Check: Matched Path Ownership")

	return false, nil
}

// IsMissing returns true if the path is missing
//
// If the path is a symlink, it tries to detect existence of the target.
func (p *PathVerification) IsMissing(fm fsx.Manager) (bool, error) {
	targetPath, err := p.ResolveTargetPath(fm)
	if err != nil {
		return true, errors.Wrapf(err, "failed to resolve target path")
	}

	_, exists, _ := fm.PathExists(targetPath)
	if !exists {
		if p.HasAction(PathActionTypes.IgnoreIfMissing) {
			p.logger.Warn().
				Str(logFields.path, p.Path).
				Msg("Path Check: Ignoring Missing Path As Specified.")

			return false, nil
		}

		p.logger.Debug().
			Str(logFields.path, p.Path).
			Str(logFields.pathType, p.Type).
			Msg("Path Check: Path Is Missing")

		return true, nil
	}

	p.logger.Debug().
		Str(logFields.path, p.Path).
		Str(logFields.pathType, p.Type).
		Msg("Path Check: Path Exists")

	return true, nil
}

// PathVerifications type allows us to use method on slices of PathVerification
// e.g. items.Filter(predicate)
type PathVerifications []PathVerification

// Filter applies a filter to slice of PathVerification items
func (paths PathVerifications) Filter(fm fsx.Manager,
	predicate func(fsx.Manager, PathVerification) (bool, error),
) (PathVerifications, error) {

	var filteredPaths []PathVerification

	for _, p := range paths {
		ok, err := predicate(fm, p)
		if err != nil {
			return PathVerifications{}, err
		}

		if ok {
			filteredPaths = append(filteredPaths, p)
		}
	}

	return filteredPaths, nil
}

// FilterByAction returns a list of paths matching the action type
func (paths PathVerifications) FilterByAction(action PathAction) PathVerifications {
	candidates := PathVerifications{}
	for _, p := range paths {
		for _, a := range p.Actions {
			if a == action {
				candidates = append(candidates, p)
			}
		}
	}

	return candidates
}

// PathFilters defines a list of filters to be applied on PathVerification item
// It allows us to apply functional pattern like this: paths.Filter(Filter.IncorrectPermissions)
var PathFilters = struct {
	IncorrectPermissions func(fsx.Manager, PathVerification) (bool, error)

	IncorrectOwnership func(fsx.Manager, PathVerification) (bool, error)

	// IsPathMissing checks if the given path exists or not
	IsPathMissing func(fsx.Manager, PathVerification) (bool, error)
}{
	IncorrectPermissions: func(fm fsx.Manager, p PathVerification) (bool, error) {
		return p.HasIncorrectPermission(fm)
	},

	IncorrectOwnership: func(fm fsx.Manager, p PathVerification) (bool, error) {
		return p.HasIncorrectOwnership(fm)
	},

	IsPathMissing: func(fm fsx.Manager, p PathVerification) (bool, error) {
		return p.IsMissing(fm)
	},
}
