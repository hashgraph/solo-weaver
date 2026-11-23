// SPDX-License-Identifier: Apache-2.0

package state

import (
	"os"
	"path/filepath"
	"time"

	"github.com/automa-saga/logx"
	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
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

// provisionerStampLockWait is how long a stamp waits for another writer of
// provisioner.yaml. A stamp holds the lock only around one write, so a
// concurrent one is waited out and its version found current; kube cluster
// uninstall holds it for its whole run, and a stamp then fails.
const provisionerStampLockWait = 3 * time.Second

// PersistProvisionerVersion records the running CLI in provisioner.yaml so
// version-boundary startup migrations are not re-evaluated — and
// non-idempotent ones (e.g. the Cilium agent restart) not re-run — on the next
// invocation. Call only after a successful startup-migration pass and only on
// a provisioned host, so a genuinely fresh machine keeps having no state file.
// changed is false when provisioner.yaml already describes the running binary,
// which is then left as it is. An fsx.FileSyncError means the new record is on
// disk but not synced, so changed is true.
func PersistProvisionerVersion() (changed bool, err error) {
	return persistProvisionerVersionIn(models.Paths().StateDir)
}

func persistProvisionerVersionIn(dir string) (changed bool, err error) {
	release, err := AcquireProvisionerLock(dir, provisionerStampLockWait)
	if err != nil {
		return false, err
	}
	defer release()
	rec := ProvisionerRecord{CLI: currentProvisionerInfo()}
	if prev, ok, readErr := readProvisionerRecord(dir); readErr == nil && ok && prev == rec {
		return false, nil
	}
	if err := writeProvisionerRecord(dir, rec); err != nil {
		// A FileSyncError leaves the new record on disk, so the file changed.
		return errorx.IsOfType(err, fsx.FileSyncError), err
	}
	return true, nil
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
// the provisioner lock, or is a startup migration. An fsx.FileSyncError means
// the new record is on disk but not synced.
func writeProvisionerRecord(dir string, rec ProvisionerRecord) error {
	b, err := yaml.Marshal(rec)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal %s", ProvisionerFileName)
	}
	path := provisionerFilePath(dir)
	if err := atomicWriteFile(path, b); err != nil {
		if errorx.IsOfType(err, fsx.FileSyncError) {
			return err
		}
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
