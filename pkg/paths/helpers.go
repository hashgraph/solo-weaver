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
	"github.com/cockroachdb/errors"
	"github.com/cockroachdb/redact"
	"os"
	"path/filepath"
)

func makeDir(folder string) error {
	err := os.MkdirAll(folder, DefaultDirMode)
	if err != nil {
		return errors.Wrapf(err, "attempt to create the path '%s' failed", redact.Safe(folder))
	}

	return nil
}

// MockNmtPaths creates mock NMP paths for the given tmpDir for tests
// Note it doesn't check for those directories to exist
func MockNmtPaths(tmpDir string) Paths {
	nmtDir := filepath.Join(tmpDir, HederaAppDirName, NodeMgmtDirName)
	svcDir := filepath.Join(tmpDir, HederaAppDirName, HederaServicesDirName)
	hapiDir := filepath.Join(svcDir, HederaApiDirName)

	nmtPaths := Paths{
		AppDir:    nmtDir,
		WorkDir:   filepath.Join(nmtDir, "/bin"),
		LogDir:    filepath.Join(nmtDir, "/logs"),
		ConfigDir: filepath.Join(nmtDir, "/config"),
		HederaAppDir: &HederaAppDir{
			Root:           filepath.Join(tmpDir, HederaAppDirName),
			UploaderMirror: filepath.Join(tmpDir, UploaderMirrorDirName),
			HederaBackups:  filepath.Join(tmpDir, HederaBackupsDirName),
			NodeMgmtTools: &NodeMgmtToolsDir{
				Root:   nmtDir,
				Bin:    filepath.Join(nmtDir, "/bin"),
				Common: filepath.Join(nmtDir, "/common"),
				Compose: &ComposeDir{
					Root:        filepath.Join(nmtDir, "/compose"),
					NetworkNode: filepath.Join(nmtDir, "/compose/network-node"),
				},
				Config: filepath.Join(nmtDir, "/config"),
				Image:  filepath.Join(nmtDir, "/image"),
				Logs:   filepath.Join(nmtDir, "/logs"),
				State:  filepath.Join(nmtDir, "/state"),
				Upgrade: &UpgradeDir{
					Root:     filepath.Join(nmtDir, "/upgrade"),
					Current:  filepath.Join(nmtDir, "/upgrade/current"),
					Pending:  filepath.Join(nmtDir, "/upgrade/pending"),
					Previous: filepath.Join(nmtDir, "/upgrade/previous"),
				},
			},
			HederaServices: &HederaServicesDir{
				Root: svcDir,
				HapiApp: &HapiAppDir{
					Root: hapiDir,
					Data: &HapiAppDataDir{
						Root:    filepath.Join(hapiDir, "/data"),
						Config:  filepath.Join(hapiDir, "/config"),
						DiskFs:  filepath.Join(hapiDir, "/diskFs"),
						Keys:    filepath.Join(hapiDir, "/keys"),
						OnBoard: filepath.Join(hapiDir, "/onboard"),
						Saved:   filepath.Join(hapiDir, "/saved"),
						Stats:   filepath.Join(hapiDir, "/stats"),
						Upgrade: &UpgradeDir{
							Root:     filepath.Join(hapiDir, "/data/upgrade"),
							Current:  filepath.Join(hapiDir, "/data/upgrade/current"),
							Pending:  filepath.Join(hapiDir, "/data/upgrade/pending"),
							Previous: filepath.Join(hapiDir, "/data/upgrade/previous"),
						},
					},
				},
			},
		},
	}

	return nmtPaths

}
