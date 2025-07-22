/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
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

package paths

import (
	"fmt"
	"github.com/cockroachdb/redact"
	"github.com/pkg/errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// TrimFromPath removes any of the matching directory in the exclusions list
// For example, for a location "/a/b/c/d/f" and exclusions = ["d", "e", "f"] it returns "/a/b/c"
func TrimFromPath(fullPath string, exclusions []string) (string, error) {
	dir := filepath.Dir(filepath.Clean(fullPath))

	pathParts := strings.Split(dir, string(os.PathSeparator))

	var finalPath []string
	for i := len(pathParts) - 1; i >= 0; i-- {
		if pathParts[i] != "" && !Contains(pathParts[i], exclusions) {
			finalPath = pathParts[0 : i+1]
			break
		}
	}

	if finalPath == nil || len(finalPath) == 0 {
		return "", errors.Errorf("entire path contained excluded folder names: %s", redact.Safe(dir))
	}

	trimmedPath := filepath.Join(finalPath...)

	// this check is necessary since filepath.Split removes the first path separator
	if filepath.IsAbs(dir) {
		return fmt.Sprintf("%s%s", string(os.PathSeparator), trimmedPath), nil
	}

	return trimmedPath, nil
}

// FindParentPath returns full path to the parent directory given the full path of a work(child) directory
// Here parentDirName can be any parent in the path.
//
// For example, for childDir = "/a/b/c/d/e" and parentDirName="c", it will return "/a/b/c"
// If the input is already the parent, it will return as it is
func FindParentPath(workDir string, parentDirName string) (string, error) {
	dir := filepath.Clean(workDir)
	pathParts := strings.Split(dir, string(os.PathSeparator))

	var finalPath []string
	for i := len(pathParts) - 1; i >= 0; i-- {
		if pathParts[i] != "" && pathParts[i] == parentDirName {
			finalPath = pathParts[0 : i+1]
			break
		}
	}

	if len(finalPath) == 0 {
		return "", errors.Errorf("no parent dir '%s' found in the given path '%s'",
			parentDirName, redact.Safe(workDir))
	}

	parentPath := filepath.Join(finalPath...)

	if filepath.IsAbs(workDir) {
		return fmt.Sprintf("%s%s", string(os.PathSeparator), parentPath), nil
	}

	return parentPath, nil
}

// FindChildPath returns a path to the child directory from the given root dir and childDirName
//
// For example, for a child dir named "e" and root path "/a" it would return "/a/b/c/d/e" if there is such a path to "e"
// It returns a map of the given pattern to the actual path: {"e": "/a/b/c/d/e"}
// Currently, pattern can only be a word or sub-path like "/upgrade/current"
// maxDepth = -1 means no limit
func FindChildPath(rootDir string, childDirName string, maxDepth int) (string, error) {
	childrenPaths, err := FindChildrenPath(rootDir, []string{childDirName}, maxDepth)
	if err != nil {
		return "", err
	}

	return childrenPaths[childDirName], nil
}

// FindChildrenPath returns paths to the children directories from the given root dir and childDirPattern
//
// For example, for a child dir pattern "/e" and root path "/a" it would return "/a/b/c/d/e" if there is such a path to "e"
// It returns a map of the given pattern to the actual path: {"e": "/a/b/c/d/e"}
// Currently, pattern can only be a word or sub-path like "/upgrade/current"
// maxDepth = -1 means no limit
func FindChildrenPath(rootDir string, childDirPattern []string, maxDepth int) (map[string]string, error) {
	childDirSuffix := map[string]string{}
	for _, childDirName := range childDirPattern {
		suffix := childDirName
		if strings.HasPrefix(childDirName, string(os.PathSeparator)) == false {
			suffix = fmt.Sprintf("%s%s", string(os.PathSeparator), childDirName)
		}

		childDirSuffix[childDirName] = suffix
	}

	childPathMap := map[string]string{}
	count := 0
	successMessage := fmt.Sprintf("** FOUND CHILD DIRECTORY PATHS ***")
	err := filepath.WalkDir(rootDir, func(path string, dir fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// determine depth by using the path separator; but first we need to do some cleanup for this logic to work.
		// Here the order is relevant since ../ and ./ has similarity
		// first remove path parts like ../ so that ../../a/b becomes a/b
		trimmedPath := strings.TrimLeft(path, fmt.Sprintf("..%s", string(os.PathSeparator)))
		// then remove path parts like ./ so that ./a/b becomes a/b
		trimmedPath = strings.TrimLeft(path, fmt.Sprintf(".%s", string(os.PathSeparator)))
		// also remove leading and trailing / so that /a/b or /a/b/ becomes a/b
		trimmedPath = strings.Trim(trimmedPath, string(os.PathSeparator))
		depth := strings.Count(trimmedPath, string(os.PathSeparator))
		if maxDepth != -1 && depth >= maxDepth {
			return errors.Errorf("Max depth %d is reached before finding all children paths", maxDepth)
		}

		for k, suffix := range childDirSuffix {
			if strings.HasSuffix(path, suffix) {
				childPathMap[k] = path
				count = count + 1

				// found so the return non-nil error to stop the walk
				if count == len(childDirPattern) {
					return errors.New(successMessage)
				}
			}
		}

		return nil
	})

	// this may look silly, but we had to return error to stop the walk.
	// we just need to check that it is not our success message
	if err != nil && err.Error() != successMessage {
		return nil, err
	}

	if count == 0 {
		return nil, errors.Errorf("child directory couldn't be found in root '%s' with patterns: %s",
			rootDir,
			childDirPattern)
	}

	return childPathMap, nil
}

// FolderExists checks if a folder exists at the path location
func FolderExists(path string) bool {
	if path == "" {
		return false
	}

	info, err := os.Stat(path)

	if err != nil && errors.Is(err, os.ErrNotExist) {
		return false
	}

	if info == nil {
		return false
	}

	return info.IsDir()
}

// Contains checks if a string exists in the array of strings
func Contains(val string, slice []string) bool {
	for _, itm := range slice {
		if itm == val {
			return true
		}
	}

	return false
}

// Parent returns the path to the immediate parent directory
// If it is empty or top level path like "/a", it returns "/"
func Parent(path string) string {
	root := string(os.PathSeparator)
	if path == "" || path == string(os.PathSeparator) {
		return root
	}

	path = filepath.Clean(path)
	pathParts := strings.Split(path, string(os.PathSeparator))

	if len(pathParts) > 1 && pathParts[0] == "" {
		pathParts[0] = root // replace "" with os.PathSeparator
	}

	parentPath := filepath.Join(pathParts[:len(pathParts)-1]...)
	if parentPath == "" {
		return root
	}

	return parentPath
}
