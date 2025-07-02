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
 *
 *
 *
 */

package common

import (
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/internal/models"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"path/filepath"

	_ "embed"

	"github.com/cockroachdb/errors"
	"golang.hedera.com/solo-provisioner/pkg/paths"
	"gopkg.in/yaml.v3"
)

//go:embed tool-state-template.yaml
var toolStateTemplateStr []byte

type commonManager struct {
	logger        *zerolog.Logger
	stateFilePath string
	fm            fsx.Manager
	nmtPaths      paths.Paths
}

func (c *commonManager) LoadToolState() (*models.ToolState, error) {
	var state models.ToolState
	buffer, err := c.fm.ReadFile(c.stateFilePath, models.MaxStateFileSize)
	if err != nil {
		buffer = toolStateTemplateStr
		c.logger.Warn().
			Str(logFields.path, c.stateFilePath).
			Str(logFields.errMsg, err.Error()).
			Msg("failed to load state file; using default from state template.")
	}

	err = yaml.Unmarshal(buffer, &state)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse state YAML from file %q", c.stateFilePath)
	}

	return &state, nil
}

func (c *commonManager) WriteToolState(state *models.ToolState) error {
	if state == nil {
		return errors.New("state cannot be nil")
	}

	b, err := yaml.Marshal(state)
	if err != nil {
		return errors.Wrap(err, "failed to marshal state as YAML")
	}

	err = c.fm.WriteFile(c.stateFilePath, b)
	if err != nil {
		return errors.Wrapf(err, "failed to write state file at %q", c.stateFilePath)
	}

	return nil
}

func (c *commonManager) GetStateFilePath() string {
	return c.stateFilePath
}

type Option = func(c *commonManager)

func WithLogger(logger *zerolog.Logger) Option {
	return func(c *commonManager) {
		if logger != nil {
			c.logger = logger
		}
	}
}

func WithStateFilePath(path string) Option {
	return func(c *commonManager) {
		if path != "" {
			c.stateFilePath = path
		}
	}
}

func WithFsManager(fm fsx.Manager) Option {
	return func(c *commonManager) {
		if fm != nil {
			c.fm = fm
		}
	}
}

func NewManager(nmtPaths paths.Paths, opts ...Option) (Manager, error) {
	var err error

	cm := &commonManager{
		logger:        logx.Nop(),
		nmtPaths:      nmtPaths,
		stateFilePath: filepath.Join(nmtPaths.HederaAppDir.NodeMgmtTools.State, models.StateFileName),
	}

	for _, opt := range opts {
		opt(cm)
	}

	if cm.fm == nil {
		cm.fm, err = fsx.NewManager()
		if err != nil {
			return nil, err
		}
	}

	return cm, nil
}
