// SPDX-License-Identifier: Apache-2.0

package models

import (
	"sort"
	"strings"

	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// Consensus data-volume names. These MUST match the solo-operator's VolumeType*
// identifiers (its internal/resources/consensus/common.go) — a PodProperties volume
// override or PVC entry only binds to a built-in volume when the name matches
// exactly. Duplicated here because the operator's constants live in an internal/
// package weaver cannot import.
const (
	ConsensusVolumeUpgrade = "upgrade"
	ConsensusVolumeLogs    = "logs"
	ConsensusVolumeStats   = "stats"
	ConsensusVolumeSaved   = "saved"
	ConsensusVolumeState   = "state"
	ConsensusVolumeBlocks  = "blocks"
	ConsensusVolumeRecords = "records"
	ConsensusVolumeEvents  = "events"
)

// Backing types a consensus data volume can use.
const (
	VolumeBackingEmptyDir = "emptydir"
	VolumeBackingHostPath = "hostpath"
	VolumeBackingPVC      = "pvc"
)

// PVC access modes valid for a consensus node. A CN pod is a single writer, so
// only the single-writer modes are sound:
//   - ReadWriteOnce: universal; supported by every provisioner.
//   - ReadWriteOncePod: strictly stronger (single pod, not just single node), but
//     requires k8s >=1.29 and a CSI driver that advertises it.
//
// ReadWriteMany (many writers) is pointless for a single pod, and ReadOnlyMany
// makes the node crash on its first write to state/saved/streams — both are
// rejected up front so a mistyped or unsound access mode fails at the CLI rather
// than leaving a Pending PVC or a crash-looping pod. Whether the chosen mode is
// actually satisfiable against the target StorageClass is cluster-specific and
// surfaces at bind time; weaver cannot pre-validate that.
const (
	AccessModeReadWriteOnce    = "ReadWriteOnce"
	AccessModeReadWriteOncePod = "ReadWriteOncePod"
)

// consensusPVCAccessModes is the allowed set, in a stable order for error messages.
var consensusPVCAccessModes = []string{AccessModeReadWriteOnce, AccessModeReadWriteOncePod}

// IsValidCNAccessMode reports whether mode is a PVC access mode a consensus node
// may use (the single-writer subset).
func IsValidCNAccessMode(mode string) bool {
	switch mode {
	case AccessModeReadWriteOnce, AccessModeReadWriteOncePod:
		return true
	}
	return false
}

// Consensus volume defaults.
const (
	// ConsensusDefaultVolumeBacking is the fallback backing when neither a per-volume
	// setting nor --default-volume-type is given. emptyDir mirrors the operator's own
	// default (ephemeral); persistence is opt-in.
	ConsensusDefaultVolumeBacking = VolumeBackingEmptyDir

	// ConsensusDefaultPVCAccessMode is the fallback PVC access mode.
	ConsensusDefaultPVCAccessMode = AccessModeReadWriteOnce
)

// The uid/gid owning the consensus node's hostPath directories is the canonical
// hedera user/group (config.HederaUserId/GroupId, "2000") — kubelet applies no
// fsGroup ownership to hostPath volumes, so the host dirs must be chowned to it.
// It lives in pkg/config (identity, not domain data); the CLI flag default and the
// chown step read it there (pkg/config imports pkg/models, so models can't import
// config — the value is consumed at those edges, not duplicated here).

// consensusVolumeNames is the fixed set of data volumes, in a stable order.
var consensusVolumeNames = []string{
	ConsensusVolumeUpgrade, ConsensusVolumeLogs, ConsensusVolumeStats, ConsensusVolumeSaved,
	ConsensusVolumeState, ConsensusVolumeBlocks, ConsensusVolumeRecords, ConsensusVolumeEvents,
}

// ConsensusVolumeNames returns the data-volume names in a stable order.
func ConsensusVolumeNames() []string {
	out := make([]string, len(consensusVolumeNames))
	copy(out, consensusVolumeNames)
	return out
}

// consensusDefaultPVCSize is the per-volume default PVC size, from the solo-charts
// solo-deployment defaults (dataSaved 500Gi, streams 100Gi, dataStats 50Gi,
// dataUpgrade/output 5Gi). solo has no separate state claim; 100Gi is a conservative
// default that can be tuned.
var consensusDefaultPVCSize = map[string]string{
	ConsensusVolumeSaved:   "500Gi",
	ConsensusVolumeState:   "100Gi",
	ConsensusVolumeBlocks:  "100Gi",
	ConsensusVolumeRecords: "100Gi",
	ConsensusVolumeEvents:  "100Gi",
	ConsensusVolumeStats:   "50Gi",
	ConsensusVolumeUpgrade: "5Gi",
	ConsensusVolumeLogs:    "5Gi",
}

// consensusDefaultHostPath is the per-volume default hostPath, mirroring the
// in-container HapiApp2.0 layout. `upgrade` is the parent of the daemon's upgrade_dir
// (the node extracts into <upgrade>/current, which the daemon reads).
var consensusDefaultHostPath = map[string]string{
	ConsensusVolumeUpgrade: "/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade",
	ConsensusVolumeLogs:    "/opt/hgcapp/services-hedera/HapiApp2.0/output",
	ConsensusVolumeStats:   "/opt/hgcapp/services-hedera/HapiApp2.0/data/stats",
	ConsensusVolumeSaved:   "/opt/hgcapp/services-hedera/HapiApp2.0/data/saved",
	ConsensusVolumeState:   "/opt/hgcapp/services-hedera/HapiApp2.0/state",
	ConsensusVolumeBlocks:  "/opt/hgcapp/blockStreams",
	ConsensusVolumeRecords: "/opt/hgcapp/recordStreams",
	ConsensusVolumeEvents:  "/opt/hgcapp/eventsStreams",
}

// ConsensusVolumeSpec is a per-volume backing override. Empty fields inherit from the
// config defaults, then the built-in per-volume defaults. Type "" inherits the
// config's default backing.
type ConsensusVolumeSpec struct {
	Type         string `json:"type,omitempty" yaml:"type,omitempty"`
	Path         string `json:"path,omitempty" yaml:"path,omitempty"`                 // hostpath
	Size         string `json:"size,omitempty" yaml:"size,omitempty"`                 // pvc
	StorageClass string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"` // pvc
	AccessMode   string `json:"accessMode,omitempty" yaml:"accessMode,omitempty"`     // pvc
}

// ConsensusVolumeDefaults are the global fallbacks (the --default-* flags / the
// file's `defaults:` block).
type ConsensusVolumeDefaults struct {
	Type         string `json:"type,omitempty" yaml:"type,omitempty"`
	PVCSize      string `json:"pvcSize,omitempty" yaml:"-"`
	StorageClass string `json:"storageClass,omitempty" yaml:"-"`
	AccessMode   string `json:"accessMode,omitempty" yaml:"-"`
}

// ConsensusVolumeConfig is the fully-merged volume configuration: global defaults
// plus per-volume overrides. Resolve a volume's effective backing with EffectiveSpec.
type ConsensusVolumeConfig struct {
	Defaults ConsensusVolumeDefaults        `json:"defaults,omitempty"`
	Volumes  map[string]ConsensusVolumeSpec `json:"volumes,omitempty"`
}

// IsValidVolumeName reports whether name is one of the consensus data volumes.
func IsValidVolumeName(name string) bool {
	for _, n := range consensusVolumeNames {
		if n == name {
			return true
		}
	}
	return false
}

// IsValidVolumeBacking reports whether t is a supported backing type.
func IsValidVolumeBacking(t string) bool {
	switch t {
	case VolumeBackingEmptyDir, VolumeBackingHostPath, VolumeBackingPVC:
		return true
	}
	return false
}

// ParseVolumeArg parses one --volume value: a comma-separated, order-independent
// key=value list (docker --mount style). `name=` is required; every other key is
// optional. Returns the volume name and its spec. Validation of keys-vs-type is
// deferred to EffectiveSpec so an omitted type can still inherit the default.
func ParseVolumeArg(arg string) (string, ConsensusVolumeSpec, error) {
	var spec ConsensusVolumeSpec
	name := ""
	for _, part := range strings.Split(arg, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return "", spec, errorx.IllegalArgument.New(
				"invalid --volume %q: expected key=value, got %q", arg, part)
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "name":
			name = v
		case "type":
			spec.Type = v
		case "path":
			spec.Path = v
		case "size":
			spec.Size = v
		case "storageClass":
			spec.StorageClass = v
		case "accessMode":
			spec.AccessMode = v
		default:
			return "", spec, errorx.IllegalArgument.New(
				"invalid --volume %q: unknown key %q (allowed: name, type, path, size, storageClass, accessMode)", arg, k)
		}
	}
	if name == "" {
		return "", spec, errorx.IllegalArgument.New("invalid --volume %q: name= is required", arg)
	}
	if !IsValidVolumeName(name) {
		return "", spec, errorx.IllegalArgument.New(
			"invalid --volume %q: unknown volume %q (allowed: %s)", arg, name, strings.Join(consensusVolumeNames, ", "))
	}
	if spec.Type != "" && !IsValidVolumeBacking(spec.Type) {
		return "", spec, errorx.IllegalArgument.New(
			"invalid --volume %q: unknown type %q (allowed: emptydir, hostpath, pvc)", arg, spec.Type)
	}
	if spec.AccessMode != "" && !IsValidCNAccessMode(spec.AccessMode) {
		return "", spec, errorx.IllegalArgument.New(
			"invalid --volume %q: unsupported accessMode %q — a consensus node is a single-writer pod (allowed: %s)",
			arg, spec.AccessMode, strings.Join(consensusPVCAccessModes, ", "))
	}
	return name, spec, nil
}

// volumesFile is the on-disk schema for --volumes-file. `defaults:` mirrors the
// --default-* flags; `volumes:` mirrors the --volume overrides.
type volumesFile struct {
	Defaults struct {
		Type string `yaml:"type"`
		PVC  struct {
			Size         string `yaml:"size"`
			StorageClass string `yaml:"storageClass"`
			AccessMode   string `yaml:"accessMode"`
		} `yaml:"pvc"`
	} `yaml:"defaults"`
	Volumes map[string]ConsensusVolumeSpec `yaml:"volumes"`
}

// LoadVolumeConfigYAML parses a --volumes-file body into a ConsensusVolumeConfig and
// validates volume names and backing types.
func LoadVolumeConfigYAML(data []byte) (ConsensusVolumeConfig, error) {
	var f volumesFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return ConsensusVolumeConfig{}, errorx.IllegalFormat.Wrap(err, "parse volumes file")
	}
	cfg := ConsensusVolumeConfig{
		Defaults: ConsensusVolumeDefaults{
			Type:         f.Defaults.Type,
			PVCSize:      f.Defaults.PVC.Size,
			StorageClass: f.Defaults.PVC.StorageClass,
			AccessMode:   f.Defaults.PVC.AccessMode,
		},
		Volumes: map[string]ConsensusVolumeSpec{},
	}
	if cfg.Defaults.Type != "" && !IsValidVolumeBacking(cfg.Defaults.Type) {
		return ConsensusVolumeConfig{}, errorx.IllegalFormat.New(
			"volumes file: defaults.type %q is invalid (allowed: emptydir, hostpath, pvc)", cfg.Defaults.Type)
	}
	if cfg.Defaults.AccessMode != "" && !IsValidCNAccessMode(cfg.Defaults.AccessMode) {
		return ConsensusVolumeConfig{}, errorx.IllegalFormat.New(
			"volumes file: defaults.pvc.accessMode %q is unsupported — a consensus node is a single-writer pod (allowed: %s)",
			cfg.Defaults.AccessMode, strings.Join(consensusPVCAccessModes, ", "))
	}
	names := make([]string, 0, len(f.Volumes))
	for name := range f.Volumes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !IsValidVolumeName(name) {
			return ConsensusVolumeConfig{}, errorx.IllegalFormat.New(
				"volumes file: unknown volume %q (allowed: %s)", name, strings.Join(consensusVolumeNames, ", "))
		}
		spec := f.Volumes[name]
		if spec.Type != "" && !IsValidVolumeBacking(spec.Type) {
			return ConsensusVolumeConfig{}, errorx.IllegalFormat.New(
				"volumes file: volume %q has invalid type %q (allowed: emptydir, hostpath, pvc)", name, spec.Type)
		}
		if spec.AccessMode != "" && !IsValidCNAccessMode(spec.AccessMode) {
			return ConsensusVolumeConfig{}, errorx.IllegalFormat.New(
				"volumes file: volume %q has unsupported accessMode %q — a consensus node is a single-writer pod (allowed: %s)",
				name, spec.AccessMode, strings.Join(consensusPVCAccessModes, ", "))
		}
		cfg.Volumes[name] = spec
	}
	return cfg, nil
}

// SetVolume records a per-volume override, merging non-empty fields onto any existing
// entry (so a later flag overrides one field without clearing the others).
func (c *ConsensusVolumeConfig) SetVolume(name string, spec ConsensusVolumeSpec) {
	if c.Volumes == nil {
		c.Volumes = map[string]ConsensusVolumeSpec{}
	}
	cur := c.Volumes[name]
	if spec.Type != "" {
		cur.Type = spec.Type
	}
	if spec.Path != "" {
		cur.Path = spec.Path
	}
	if spec.Size != "" {
		cur.Size = spec.Size
	}
	if spec.StorageClass != "" {
		cur.StorageClass = spec.StorageClass
	}
	if spec.AccessMode != "" {
		cur.AccessMode = spec.AccessMode
	}
	c.Volumes[name] = cur
}

// EffectiveSpec resolves a volume's final backing by applying precedence
// (per-volume > defaults > built-in) and validates that the keys present match the
// resolved backing type. The returned spec has Type always set, and — for pvc/hostpath
// — the backing-specific fields fully populated.
func (c ConsensusVolumeConfig) EffectiveSpec(name string) (ConsensusVolumeSpec, error) {
	if !IsValidVolumeName(name) {
		return ConsensusVolumeSpec{}, errorx.IllegalArgument.New("unknown consensus volume %q", name)
	}
	spec := c.Volumes[name]

	// Resolve backing type: per-volume, else default, else built-in.
	t := firstNonEmpty(spec.Type, c.Defaults.Type, ConsensusDefaultVolumeBacking)
	out := ConsensusVolumeSpec{Type: t}

	switch t {
	case VolumeBackingEmptyDir:
		if err := rejectKeys(name, spec, "emptydir", spec.Path != "", spec.Size != "", spec.StorageClass != "", spec.AccessMode != ""); err != nil {
			return ConsensusVolumeSpec{}, err
		}
	case VolumeBackingHostPath:
		if spec.Size != "" || spec.StorageClass != "" || spec.AccessMode != "" {
			return ConsensusVolumeSpec{}, errorx.IllegalArgument.New(
				"volume %q is hostpath: size/storageClass/accessMode do not apply", name)
		}
		out.Path = firstNonEmpty(spec.Path, consensusDefaultHostPath[name])
	case VolumeBackingPVC:
		if spec.Path != "" {
			return ConsensusVolumeSpec{}, errorx.IllegalArgument.New(
				"volume %q is pvc: path does not apply", name)
		}
		out.Size = firstNonEmpty(spec.Size, c.Defaults.PVCSize, consensusDefaultPVCSize[name])
		out.StorageClass = firstNonEmpty(spec.StorageClass, c.Defaults.StorageClass) // "" = cluster default
		out.AccessMode = firstNonEmpty(spec.AccessMode, c.Defaults.AccessMode, ConsensusDefaultPVCAccessMode)
		if !IsValidCNAccessMode(out.AccessMode) {
			return ConsensusVolumeSpec{}, errorx.IllegalArgument.New(
				"volume %q has unsupported PVC accessMode %q — a consensus node is a single-writer pod (allowed: %s)",
				name, out.AccessMode, strings.Join(consensusPVCAccessModes, ", "))
		}
	default:
		return ConsensusVolumeSpec{}, errorx.IllegalArgument.New(
			"volume %q has invalid backing %q", name, t)
	}
	return out, nil
}

// rejectKeys errors when any pvc/hostpath key is set on an emptydir volume.
func rejectKeys(name string, _ ConsensusVolumeSpec, backing string, anySet ...bool) error {
	for _, set := range anySet {
		if set {
			return errorx.IllegalArgument.New(
				"volume %q is %s: path/size/storageClass/accessMode do not apply", name, backing)
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
