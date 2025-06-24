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
	"fmt"
	configs2 "golang.hedera.com/solo-provisioner/internal/models"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/pkg/fsx"
	"golang.hedera.com/solo-provisioner/pkg/paths"
	"gopkg.in/yaml.v3"
)

var tmpDir = "../../tmp"

func TestVerification(t *testing.T) {
	req := require.New(t)
	var c PathVerification
	path := "services-hedera/HapiApp2.0"
	action := PathActionTypes.CreateIfMissing
	user := "hedera"
	group := "hedera"
	permission := os.FileMode(755)
	_type := "file"

	var yamlString = fmt.Sprintf(`
path: %s
actions:
  - %s
owner: %s
group: %s
mode: %d
type: %s
`, path, action, user, group, permission, _type)

	err := yaml.Unmarshal([]byte(yamlString), &c)
	if err != nil {
		t.Errorf("Could not parse yaml, error = %s", err)
	}

	req.Equal(c.Path, path)
	req.Equal(c.Actions[0], action)
	req.Equal(c.Owner, user)
	req.Equal(c.Group, group)
	req.Equal(c.Mode, permission)
	req.Equal(c.Type, _type)
}

func TestCommonManager_LoadToolState(t *testing.T) {
	req := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	pm, _, _ := setupMockPrincipalManagerWithCurrentUser(t, ctrl)
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(pm))
	req.NoError(err)

	cm, mockState, err := writeMockToolState(t, tmpDir, fm)
	req.NoError(err)

	state, err := cm.LoadToolState()
	req.NoError(err)
	req.Equal(mockState, state)
}

func TestCommonManager_LoadToolState_Failures(t *testing.T) {
	req := require.New(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	tmpDir := "../../tmp"
	pm, _, _ := setupMockPrincipalManagerWithCurrentUser(t, ctrl)
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(pm))
	req.NoError(err)

	cm, mockState, err := writeMockToolState(t, tmpDir, fm)
	req.NoError(err)

	stateFile := cm.stateFilePath

	// invalid state file path
	cm.stateFilePath = filepath.Join(t.TempDir(), "invalid-state.yaml")
	defaultState, err := cm.LoadToolState()
	req.NoError(err)
	req.NotNil(defaultState)

	// garbled state file
	req.NoError(os.WriteFile(cm.stateFilePath, []byte("---- INVALID YAML ---"), 0755))
	_, err = cm.LoadToolState()
	req.Error(err)
	req.Contains(err.Error(), "failed to parse state YAML")

	cm.stateFilePath = stateFile
	state, err := cm.LoadToolState()
	req.NoError(err)
	req.Equal(mockState, state)

	// if read access is removed, we should fail to load the state file.
	// In VirtioFS this does not have any effect.
	// so we try to access first and then skip if we are able to access the file.
	req.NoError(os.Chmod(cm.stateFilePath, 0300))
	defer os.Remove(cm.stateFilePath)
	_, err = os.Open(cm.stateFilePath)
	if err != nil { // if we cannot open then run the test
		info, err := os.Stat(cm.stateFilePath)
		req.NoError(err)
		t.Log(info.Mode().Perm().String())

		state2, err2 := cm.LoadToolState()
		req.NoError(err2)
		req.NotNil(state2)
		req.Equal(defaultState, state2)
	}
}

func TestCommonManager_WriteToolState_Failures(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	req := require.New(t)

	defer os.RemoveAll(t.TempDir())

	pm, _, _ := setupMockPrincipalManagerWithCurrentUser(t, ctrl)
	fm, err := fsx.NewManager(fsx.WithPrincipalManager(pm))
	req.NoError(err)

	cm, mockState, err := writeMockToolState(t, tmpDir, fm)
	req.NoError(err)

	stateFile := cm.stateFilePath

	err = cm.WriteToolState(nil)
	req.Error(err)
	req.Contains(err.Error(), "state cannot be nil")

	cm.stateFilePath = stateFile
	state, err := cm.LoadToolState()
	req.NoError(err)
	req.Equal(mockState, state)

	invalidFile := filepath.Join("/`INVALID_CHARACTER_IN_DIR", "invalid-state.yaml")
	cm.stateFilePath = invalidFile
	err = cm.WriteToolState(mockState)
	req.Error(err)
	req.Contains(err.Error(), "failed to write state file")
}

func writeMockToolState(t *testing.T, tmpDir string, fm fsx.Manager) (*commonManager, *configs2.ToolState, error) {
	req := require.New(t)
	mockNmtPaths := paths.MockNmtPaths(tmpDir)
	logger := logx.Nop()
	cm := &commonManager{
		logger:        logger,
		stateFilePath: filepath.Join(mockNmtPaths.HederaAppDir.NodeMgmtTools.State, configs2.StateFileName),
		fm:            fm,
		nmtPaths:      mockNmtPaths,
	}

	mockState := &configs2.ToolState{
		NodeID:      "node0",
		AppVersion:  "0.36.0",
		ImageID:     configs2.DefaultImageId,
		JavaHeapMin: configs2.DefaultJVMMinMem,
		JavaHeapMax: configs2.DefaultJVMMaxMem,
		JavaVersion: configs2.DefaultJVMVersion,
	}

	err := cm.WriteToolState(mockState)
	req.NoError(err)

	return cm, mockState, err
}
