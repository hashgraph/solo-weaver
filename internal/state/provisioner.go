// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"

	"github.com/automa-saga/logx"
	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// ProvisionerFileName is the file in the state directory that describes the
// solo-provisioner binaries themselves, rather than a host or a cluster.
const ProvisionerFileName = "provisioner.yaml"

// ProvisionerRecord is the content of provisioner.yaml. Startup migrations
// read the CLI's recorded version from it to decide which ones apply.
type ProvisionerRecord struct {
	CLI ProvisionerInfo `yaml:"cli" json:"cli"`
}

func provisionerFilePath(dir string) string {
	return filepath.Join(dir, ProvisionerFileName)
}

// PersistProvisionerVersion records the running CLI in provisioner.yaml so
// version-boundary startup migrations are not re-evaluated — and
// non-idempotent ones (e.g. the Cilium agent restart) not re-run — on the next
// invocation. Call only after a successful startup-migration pass and only on
// a provisioned host, so a genuinely fresh machine keeps having no state file.
func PersistProvisionerVersion() error {
	return persistProvisionerVersionIn(models.Paths().StateDir)
}

func persistProvisionerVersionIn(dir string) error {
	release, err := AcquireProvisionerLock(dir, 0)
	if err != nil {
		return err
	}
	defer release()
	return writeProvisionerRecord(dir, ProvisionerRecord{CLI: currentProvisionerInfo()})
}

// currentProvisionerInfo describes the running binary.
func currentProvisionerInfo() ProvisionerInfo {
	exePath, err := os.Executable()
	if err != nil {
		logx.As().Warn().Err(err).Msg("Failed to get executable path for provisioner state, using empty string")
	}

	v := version.Get()
	return ProvisionerInfo{
		Version:      v.Version,
		Commit:       v.Commit,
		GoVersion:    v.GoVersion,
		Executable:   exePath,
		StateVersion: ModelVersion,
	}
}

// writeProvisionerRecord replaces provisioner.yaml with rec. The caller holds
// the provisioner lock, or is a startup migration.
func writeProvisionerRecord(dir string, rec ProvisionerRecord) error {
	b, err := yaml.Marshal(rec)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal %s", ProvisionerFileName)
	}
	path := provisionerFilePath(dir)
	if err := atomicWriteFile(path, b); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write %s", path)
	}
	return nil
}

// readProvisionerRecord reads provisioner.yaml from dir; ok is false when it
// does not exist.
func readProvisionerRecord(dir string) (rec ProvisionerRecord, ok bool, err error) {
	path := provisionerFilePath(dir)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ProvisionerRecord{}, false, nil
		}
		return ProvisionerRecord{}, false, errorx.ExternalError.Wrap(err, "failed to read %s", path)
	}
	if err := yaml.Unmarshal(b, &rec); err != nil {
		return ProvisionerRecord{}, false, errorx.IllegalFormat.Wrap(err, "failed to parse %s", path)
	}
	return rec, true, nil
}
