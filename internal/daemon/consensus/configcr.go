// SPDX-License-Identifier: Apache-2.0

package consensus

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// This file ports the UC provisioner-proxy's config-CR deployment
// (solo-operator internal/uc/config_cr.go) so the daemon presents the same
// behaviour: per-operation ConsensusConfig CRs created from the upgrade package
// and waited to Valid=True. It is deliberately SELF-CONTAINED — the operator's
// GVK group/version and label keys are mirrored as local constants (they must
// match the operator byte-for-byte), so the daemon carries no solo-operator or
// weaver import here and can be extracted later.
//
// Per-node BlockNodesConfig (block-nodes/config/block-nodes-<id>.json) is NOT
// handled here yet — its naming is being reworked operator-side (solo-operator
// #1321); a follow-up adds it once that settles.

const (
	// configSubdir is the upgrade-package subdirectory holding ConsensusConfig files.
	configSubdir = "data/config"

	// maxConfigFileSize caps a config file — CRs are stored in etcd (~1.5MiB/object).
	maxConfigFileSize = 1 << 20 // 1 MiB

	// configCRGroup / configCRVersion are the ConsensusConfig CR API coordinates.
	// Mirror the operator's api/v1alpha1 GroupVersion (matches networkUpgradeExecuteGVR).
	configCRGroup   = "operator.solo.hedera.com"
	configCRVersion = "v1alpha1"

	// labelOperationID / annotationCreatedBy mirror the operator's label + annotation
	// keys exactly (api/v1alpha1 LabelOperationID; internal/uc created-by annotation).
	labelOperationID    = "operator.solo.hiero.org/operation-id"
	annotationCreatedBy = "operator.solo.hiero.org/created-by"
	createdByDaemon     = "provisioner-daemon"

	// configCRPollInterval is the cadence for polling config CRs toward Valid=True.
	configCRPollInterval = 1 * time.Second
)

// configGVR builds a ConsensusConfig GVR from its (lowercase plural) resource name.
func configGVR(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: configCRGroup, Version: configCRVersion, Resource: resource}
}

// kindEntry binds a package filename to its CR kind, GVR, and metadata.name suffix.
type kindEntry struct {
	gvr        schema.GroupVersionResource
	kind       string
	nameSuffix string
}

// configFileKinds is the filename→CR-kind mapping for every recognised
// ConsensusConfig file (mirrors the operator). Package-root files (log4j2.xml,
// settings.txt) share this map with the data/config/ files.
var configFileKinds = map[string]kindEntry{
	// Package-root files.
	"log4j2.xml":   {gvr: configGVR("log4j2configs"), kind: "Log4j2Config", nameSuffix: "log4j2"},
	"settings.txt": {gvr: configGVR("nodesettings"), kind: "NodeSettings", nameSuffix: "settings"},

	// data/config/ files.
	"application.properties":          {gvr: configGVR("applicationproperties"), kind: "ApplicationProperties", nameSuffix: "application-properties"},
	"application-override.properties": {gvr: configGVR("applicationoverrideproperties"), kind: "ApplicationOverrideProperties", nameSuffix: "application-override-properties"},
	"bootstrap.properties":            {gvr: configGVR("bootstrapproperties"), kind: "BootstrapProperties", nameSuffix: "bootstrap-properties"},
	"node.properties":                 {gvr: configGVR("nodepropertiesconfigs"), kind: "NodePropertiesConfig", nameSuffix: "node-properties"},
	"throttles.json":                  {gvr: configGVR("throttlesconfigs"), kind: "ThrottlesConfig", nameSuffix: "throttles"},
	"feeSchedules.json":               {gvr: configGVR("feeschedules"), kind: "FeeSchedules", nameSuffix: "fee-schedules"},
	"simpleFeesSchedules.json":        {gvr: configGVR("simplefeesschedules"), kind: "SimpleFeesSchedules", nameSuffix: "simple-fee-schedules"},
	"api-permission.properties":       {gvr: configGVR("apipermissionproperties"), kind: "ApiPermissionProperties", nameSuffix: "api-permission-properties"},
}

// packageRootFiles live at the package root, not under data/config/.
var packageRootFiles = []string{"log4j2.xml", "settings.txt"}

// blockNodesSubdir holds per-node block-node config, selected by node id and kept
// out of data/config/ (HIP-1494): block-nodes/config/block-nodes-<nodeID>.json.
const blockNodesSubdir = "block-nodes/config"

// blockNodesEntry is the kind mapping for the per-node BlockNodesConfig CR. It uses
// per-operation naming like every other kind (solo-operator#1321 removed the old
// stable-name exception), so it is created and waited on identically.
var blockNodesEntry = kindEntry{gvr: configGVR("blocknodesconfigs"), kind: "BlockNodesConfig", nameSuffix: "block-nodes"}

// ignoredConfigFilenames legitimately ship in data/config/ but are NOT turned into
// ConsensusConfig CRs (the operator manages them another way). Skipped, not rejected,
// so genuinely unknown files still hard-abort. genesis-network.json is the genesis
// roster, produced by the NetworkGenesis flow.
var ignoredConfigFilenames = map[string]struct{}{
	"genesis-network.json": {},
}

// knownConfigFilenames returns the recognised filenames, sorted, for error hints.
func knownConfigFilenames() []string {
	names := make([]string, 0, len(configFileKinds))
	for name := range configFileKinds {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// rejectSymlink rejects a symlink (Lstat mode): the upgrade package is untrusted and
// following a symlink could copy an arbitrary host file into a CR. Fatal.
func rejectSymlink(path string, mode fs.FileMode) error {
	if mode&fs.ModeSymlink == 0 {
		return nil
	}
	return errx.WithHints(
		errx.WithReason(ErrConfigFileInvalid.New("config file %q is a symlink — refusing to read", path), ReasonConfigFileSymlink),
		"Remove symlinks from the upgrade package; only regular files are accepted under data/config/",
	)
}

// rejectOversized rejects a file above maxConfigFileSize. Fatal.
func rejectOversized(path string, size int64) error {
	if size <= maxConfigFileSize {
		return nil
	}
	return errx.WithHints(
		errx.WithReason(ErrConfigFileInvalid.New("config file %q is %d bytes, exceeds the %d byte limit", path, size, maxConfigFileSize), ReasonConfigFileTooLarge),
		"Reduce the file size or split it before including it in the upgrade package",
	)
}

// readFileNoFollow is os.ReadFile with O_NOFOLLOW, so a symlink is rejected rather
// than silently followed.
func readFileNoFollow(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, errx.WithHints(
				errx.WithReason(ErrConfigFileInvalid.New("%q is a symlink — refusing to read", path), ReasonConfigFileSymlink),
				"Remove symlinks from the upgrade package; only regular files are accepted",
			)
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if err := rejectOversized(path, info.Size()); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

// isDecorated reports whether err already carries an errx reason.
func isDecorated(err error) bool {
	_, ok := errx.ReasonOf(err)
	return ok
}

// scannedFile is one recognised config file. Content is read on demand, not held.
type scannedFile struct {
	absPath string
	entry   kindEntry
}

// scanConfigFiles walks the upgrade package for recognised config files under
// data/config/ and the package root (log4j2.xml, settings.txt). An unrecognised
// filename in data/config/ is a fatal error (it would be silently unapplied); an
// absent data/config/ directory is not (some upgrades change no config).
func scanConfigFiles(upgradePath string) ([]scannedFile, error) {
	var files []scannedFile

	configDir := filepath.Join(upgradePath, configSubdir)
	dirEntries, err := os.ReadDir(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			logx.As().Info().Str("reason", ReasonConfigDirAbsent.String()).
				Str("dir", configDir).Msg("data/config/ absent — no config CRs to deploy")
		} else {
			return nil, errx.WithHints(
				errx.WithReason(ErrUpgradePath.Wrap(err, "read config dir %s", configDir), ReasonUpgradePathUnreadable),
				"Confirm the upgrade package was extracted correctly and data/config/ is readable",
			)
		}
	}
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		absPath := filepath.Join(configDir, name)
		info, err := de.Info() // Lstat semantics: never follows a symlink
		if err != nil {
			return nil, errx.WithHints(
				errx.WithReason(ErrUpgradePath.Wrap(err, "stat %s", absPath), ReasonUpgradePathUnreadable),
				"Confirm the upgrade package was extracted correctly and data/config/ is readable",
			)
		}
		if err := rejectSymlink(absPath, info.Mode()); err != nil {
			return nil, err
		}
		if err := rejectOversized(absPath, info.Size()); err != nil {
			return nil, err
		}
		if _, skip := ignoredConfigFilenames[name]; skip {
			logx.As().Info().Str("reason", ReasonConfigFileIgnored.String()).
				Str("filename", name).Str("dir", configDir).Msg("config file is skipped (no config CR)")
			continue
		}
		entry, ok := configFileKinds[name]
		if !ok {
			logx.As().Error().Str("reason", ReasonConfigFileUnrecognised.String()).
				Str("filename", name).Str("dir", configDir).
				Strs("recognised", knownConfigFilenames()).
				Msg("unrecognised config filename — aborting execute phase")
			return nil, errx.WithHints(
				errx.WithReason(ErrConfigFileInvalid.New("unrecognised config filename %q in %s", name, configDir), ReasonConfigFileUnrecognised),
				"Remove or rename the unrecognised file from data/config/ in the upgrade package",
				"Recognised filenames: "+strings.Join(knownConfigFilenames(), ", "),
			)
		}
		files = append(files, scannedFile{absPath: absPath, entry: entry})
	}

	for _, name := range packageRootFiles {
		path := filepath.Join(upgradePath, name)
		info, err := os.Lstat(path) // not Stat: don't follow a symlink
		if err != nil {
			if os.IsNotExist(err) {
				continue // optional — not all packages include these files
			}
			return nil, errx.WithHints(
				errx.WithReason(ErrUpgradePath.Wrap(err, "stat %s", path), ReasonUpgradePathUnreadable),
				"Confirm the upgrade package was extracted correctly",
			)
		}
		if err := rejectSymlink(path, info.Mode()); err != nil {
			return nil, err
		}
		if err := rejectOversized(path, info.Size()); err != nil {
			return nil, err
		}
		files = append(files, scannedFile{absPath: path, entry: configFileKinds[name]})
	}

	return files, nil
}

// configCRName derives metadata.name: "<operationId-lowercased>-<nameSuffix>".
// Per-operation so sequential upgrades never collide (CRs are never deleted — audit).
func configCRName(operationID string, entry kindEntry) string {
	return strings.ToLower(operationID) + "-" + entry.nameSuffix
}

// buildConfigCR reads sf and constructs the unstructured CR (one file at a time, so
// content is never buffered across files). spec.orbit links it to the Orbit for
// cascade deletion; spec.scope (node<N>) is how the operator matches it to a node.
func buildConfigCR(namespace, scope, orbit, operationID string, sf scannedFile) (*unstructured.Unstructured, error) {
	data, err := readFileNoFollow(sf.absPath)
	if err != nil {
		if isDecorated(err) {
			return nil, err
		}
		return nil, errx.WithHints(
			errx.WithReason(ErrUpgradePath.Wrap(err, "read config file %s", sf.absPath), ReasonUpgradePathUnreadable),
			"Confirm the upgrade package was extracted correctly and the file is readable",
		)
	}

	cr := &unstructured.Unstructured{}
	cr.SetAPIVersion(configCRGroup + "/" + configCRVersion)
	cr.SetKind(sf.entry.kind)
	cr.SetName(configCRName(operationID, sf.entry))
	cr.SetNamespace(namespace)
	cr.SetLabels(map[string]string{labelOperationID: operationID})
	cr.SetAnnotations(map[string]string{annotationCreatedBy: createdByDaemon})
	_ = unstructured.SetNestedField(cr.Object, string(data), "spec", "content")
	_ = unstructured.SetNestedField(cr.Object, scope, "spec", "scope")
	if orbit != "" {
		_ = unstructured.SetNestedField(cr.Object, orbit, "spec", "orbit")
	}
	return cr, nil
}

// configCRRef binds a created CR's name to its GVR so it can be waited on.
type configCRRef struct {
	name string
	gvr  schema.GroupVersionResource
	kind string
}

// configCRDeployer creates and waits the per-operation ConsensusConfig CRs for one
// execute operation. create() populates refs; waitValid() consumes them, so one
// deployer instance is shared by the create and wait steps of a single run.
type configCRDeployer struct {
	client       dynamic.Interface
	namespace    string
	upgradePath  string
	scope        string
	orbit        string
	nodeID       string
	operationID  string
	pollInterval time.Duration

	refs []configCRRef
}

// create scans the upgrade package and creates each recognised config CR (the
// data/config/ + package-root files, plus this node's block-node config). Idempotent:
// AlreadyExists is success (a re-run mints no duplicate — the per-op name is stable in
// the operationId). Populates d.refs for waitValid.
func (d *configCRDeployer) create(ctx context.Context) error {
	if d.upgradePath == "" {
		return errx.WithHints(
			errx.WithReason(ErrUpgradePath.New("upgrade path is empty — cannot deploy config CRs"), ReasonUpgradePathUnreadable),
			"Confirm the daemon's upgrade_dir is configured and the upgrade package is staged there",
		)
	}

	scanned, err := scanConfigFiles(d.upgradePath)
	if err != nil {
		return err
	}

	// Per-node block-node config lives outside data/config/ (block-nodes/config/,
	// selected by node id) but is named and delivered like every other config CR
	// (per-operation, Create-only — solo-operator#1321/#1322).
	bn, err := d.scanBlockNodes()
	if err != nil {
		return err
	}
	if bn != nil {
		scanned = append(scanned, *bn)
	}

	d.refs = make([]configCRRef, 0, len(scanned))
	for _, sf := range scanned {
		ref, err := d.createConfigCR(ctx, sf)
		if err != nil {
			return err
		}
		d.refs = append(d.refs, ref)
	}

	if len(d.refs) == 0 {
		logx.As().Info().Str("reason", ReasonConfigDirAbsent.String()).
			Str("operation_id", d.operationID).Msg("no config files in upgrade package — nothing to deploy")
	}
	return nil
}

// createConfigCR builds and creates the CR for a single scanned file (Create-only:
// AlreadyExists is success, so a re-run mints no duplicate), returning its ref.
func (d *configCRDeployer) createConfigCR(ctx context.Context, sf scannedFile) (configCRRef, error) {
	cr, err := buildConfigCR(d.namespace, d.scope, d.orbit, d.operationID, sf)
	if err != nil {
		return configCRRef{}, err
	}

	createCtx, cancel := context.WithTimeout(ctx, patchTimeout)
	_, createErr := d.client.Resource(sf.entry.gvr).Namespace(d.namespace).Create(createCtx, cr, metav1.CreateOptions{})
	cancel()

	switch {
	case createErr == nil:
		logx.As().Info().Str("reason", ReasonConfigCRCreated.String()).
			Str("operation_id", d.operationID).Str("cr_kind", sf.entry.kind).
			Str("cr_name", cr.GetName()).Msg("created config CR")
	case k8serrors.IsAlreadyExists(createErr):
		logx.As().Debug().Str("reason", ReasonConfigCRExists.String()).
			Str("operation_id", d.operationID).Str("cr_kind", sf.entry.kind).
			Str("cr_name", cr.GetName()).Msg("config CR already exists — skipping create")
	default:
		return configCRRef{}, errx.WithHints(
			errx.WithReason(ErrK8sAPI.Wrap(createErr, "create %s %s", sf.entry.kind, cr.GetName()), ReasonConfigCRCreateFailed),
			"Confirm the daemon-cn kubeconfig has create permission on "+sf.entry.gvr.Resource+"."+sf.entry.gvr.Group,
			"Check API server connectivity and that the CRD is installed",
		)
	}

	return configCRRef{name: cr.GetName(), gvr: sf.entry.gvr, kind: sf.entry.kind}, nil
}

// scanBlockNodes locates this node's block-node config file
// (block-nodes/config/block-nodes-<nodeID>.json) with the same symlink/oversize
// rejection as the other files. Returns (nil, nil) when the node id is unset or the
// package carries no block-node config for this node.
func (d *configCRDeployer) scanBlockNodes() (*scannedFile, error) {
	if d.nodeID == "" {
		return nil, nil
	}
	path := filepath.Join(d.upgradePath, blockNodesSubdir, "block-nodes-"+d.nodeID+".json")
	info, err := os.Lstat(path) // not Stat: don't follow a symlink
	if err != nil {
		if os.IsNotExist(err) {
			logx.As().Info().Str("reason", ReasonConfigDirAbsent.String()).
				Str("operation_id", d.operationID).Str("path", path).
				Msg("no per-node block-node config in upgrade package — skipping")
			return nil, nil
		}
		return nil, errx.WithHints(
			errx.WithReason(ErrUpgradePath.Wrap(err, "stat %s", path), ReasonUpgradePathUnreadable),
			"Confirm the upgrade package was extracted correctly and block-nodes/config/ is readable",
		)
	}
	if err := rejectSymlink(path, info.Mode()); err != nil {
		return nil, err
	}
	if err := rejectOversized(path, info.Size()); err != nil {
		return nil, err
	}
	return &scannedFile{absPath: path, entry: blockNodesEntry}, nil
}

// waitValid polls each created CR until its status.conditions[Valid]=True. A
// Valid=False is a terminal (fatal) failure — the operator rejected the content —
// reported as DaemonResult=False with reason ConfigCRInvalid; the step must not
// advance. A get error is transient (logged, retried next tick). The deadline is the
// step context (ctx.Done()).
func (d *configCRDeployer) waitValid(ctx context.Context) error {
	pending := make(map[string]configCRRef, len(d.refs))
	for _, r := range d.refs {
		pending[r.name] = r
	}
	if len(pending) == 0 {
		return nil
	}

	checkPending := func() error {
		for name, ref := range pending {
			getCtx, cancel := context.WithTimeout(ctx, listTimeout)
			obj, err := d.client.Resource(ref.gvr).Namespace(d.namespace).Get(getCtx, name, metav1.GetOptions{})
			cancel()
			if err != nil {
				logx.As().Warn().Err(err).Str("reason", ReasonConfigCRGetFailed.String()).
					Str("operation_id", d.operationID).Str("cr_kind", ref.kind).Str("cr_name", name).
					Msg("could not get config CR — will retry")
				continue
			}

			status, found := crConditionStatus(obj, "Valid")
			if !found {
				continue // no Valid condition yet — keep waiting
			}
			switch status {
			case string(metav1.ConditionTrue):
				logx.As().Info().Str("reason", ReasonConfigCRValid.String()).
					Str("operation_id", d.operationID).Str("cr_kind", ref.kind).Str("cr_name", name).
					Msg("config CR reconciled — Valid=True")
				delete(pending, name)
			case string(metav1.ConditionFalse):
				msg := crConditionMessage(obj, "Valid")
				logx.As().Error().Str("reason", ReasonConfigCRInvalid.String()).
					Str("operation_id", d.operationID).Str("cr_kind", ref.kind).Str("cr_name", name).
					Str("condition_message", msg).Msg("config CR is invalid — operator rejected content")
				// Fatal: no Temporary trait, so the executor writes DaemonResult=False
				// (reason ConfigCRInvalid) and does not retry (HIP-1496).
				return errx.WithHints(
					errx.WithReason(ErrConfigCRInvalid.New("%s %s is Valid=False: %s", ref.kind, name, msg), ReasonConfigCRInvalid),
					"Fix the config file content in the upgrade package and re-run the upgrade",
				)
			}
		}
		return nil
	}

	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for len(pending) > 0 {
		if err := checkPending(); err != nil {
			return err
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			names := make([]string, 0, len(pending))
			for n := range pending {
				names = append(names, n)
			}
			// Transient: a stalled reconcile is retried via watch re-delivery until
			// the handoff deadline turns it into DeadlineExceeded.
			return errx.WithHints(
				errx.WithReason(ErrK8sAPI.New("timed out waiting for config CRs to become Valid: %v", names), ReasonConfigCRWaitTimeout),
				"Check operator logs — config CR reconciliation may have stalled",
			)
		case <-ticker.C:
			if err := checkPending(); err != nil {
				return err
			}
		}
	}

	logx.As().Info().Str("reason", ReasonConfigCRValid.String()).
		Str("operation_id", d.operationID).Int("count", len(d.refs)).
		Msg("all config CRs are Valid — proceeding")
	return nil
}

// crConditionStatus returns the status string of the named condition and whether it
// was found in obj.status.conditions.
func crConditionStatus(obj *unstructured.Unstructured, condType string) (string, bool) {
	conds, ok, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !ok {
		return "", false
	}
	for _, raw := range conds {
		c, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _, _ := unstructured.NestedString(c, "type"); t == condType {
			s, _, _ := unstructured.NestedString(c, "status")
			return s, true
		}
	}
	return "", false
}

// crConditionMessage returns the message of the named condition, or "".
func crConditionMessage(obj *unstructured.Unstructured, condType string) string {
	conds, ok, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if !ok {
		return ""
	}
	for _, raw := range conds {
		c, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _, _ := unstructured.NestedString(c, "type"); t == condType {
			m, _, _ := unstructured.NestedString(c, "message")
			return m
		}
	}
	return ""
}
