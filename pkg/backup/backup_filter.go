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

package backup

import (
	"fmt"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	fs2 "io/fs"
	"path/filepath"
	"strings"
)

// backupFilter implements Filter interface
type backupFilter struct {
	ruleSet SnapshotRuleSet
}

func NewFilter(ruleSet SnapshotRuleSet) *backupFilter {
	return &backupFilter{ruleSet: ruleSet}
}

// ShouldBackup returns true if src path should be included in the backup
//
// The patterns in the rule-set are evaluated as a POSIX glob expression.
//
// Inclusion rule is as followed:
//   - Include patters are processed first and the first match results in inclusion.
//   - Exclude patterns should be processed subsequently and the first match results in exclusion.
//   - If no include or exclude pattern matches, the file is included.
func (b *backupFilter) ShouldBackup(src string, dst string) bool {
	if len(b.ruleSet.Include) <= 0 || len(b.ruleSet.Exclude) <= 0 {
		return true // ignore filtering
	}

	for _, pattern := range b.ruleSet.Include {
		matched, err := filepath.Match(pattern, src)
		if err != nil {
			// TODO log warning for not being able to parse the pattern
			continue
		}

		if matched {
			return true
		}
	}

	for _, pattern := range b.ruleSet.Exclude {
		matched, err := filepath.Match(pattern, src)
		if err != nil {
			// TODO log warning for not being able to parse the pattern
			continue
		}

		if matched {
			return false
		}
	}

	return true
}

func (b *backupFilter) Apply(src string, dst string, op CloneOp) error {
	if b.ShouldBackup(src, dst) {
		return op(src, dst)
	}

	return nil
}

type noFilter struct{}

func (p *noFilter) Apply(src string, dst string, op CloneOp) error {
	return op(src, dst)
}

type dirAndPFXFilter struct {
	filesystemManager fsx.Manager
	logger            *zerolog.Logger
	srcRootPath       string
}

func (p *dirAndPFXFilter) Apply(src string, dst string, op CloneOp) error {
	// skip directories and private key files
	if p.filesystemManager.IsDirectory(src) && src != p.srcRootPath {
		p.logger.Warn().Msgf("skipping copy of directory %s", src)
		return fs2.SkipDir
	}

	if strings.HasSuffix(strings.ToLower(src), ".pfx") {
		p.logger.Warn().Msgf("skipping copy of private key file %s", src)
		return nil
	}

	return op(src, dst)
}

func NewDirAndPFXFilter(filesystemManager fsx.Manager, srcRootPath string, logger *zerolog.Logger) (Filter, error) {
	if logger == nil {
		nl := zerolog.Nop()
		logger = &nl
	}

	if filesystemManager == nil {
		return nil, fmt.Errorf("filesystemManager is nil")
	}

	return &dirAndPFXFilter{
		filesystemManager: filesystemManager,
		logger:            logger,
		srcRootPath:       srcRootPath,
	}, nil
}
