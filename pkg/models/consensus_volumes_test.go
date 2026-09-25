// SPDX-License-Identifier: Apache-2.0

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseVolumeArg(t *testing.T) {
	t.Run("full spec, order-independent", func(t *testing.T) {
		name, spec, err := ParseVolumeArg("size=500Gi,name=saved,type=pvc,storageClass=fast-ssd")
		require.NoError(t, err)
		assert.Equal(t, ConsensusVolumeSaved, name)
		assert.Equal(t, ConsensusVolumeSpec{Type: VolumeBackingPVC, Size: "500Gi", StorageClass: "fast-ssd"}, spec)
	})
	t.Run("type optional (inherit later)", func(t *testing.T) {
		name, spec, err := ParseVolumeArg("name=saved,storageClass=fast-ssd")
		require.NoError(t, err)
		assert.Equal(t, ConsensusVolumeSaved, name)
		assert.Equal(t, "", spec.Type)
		assert.Equal(t, "fast-ssd", spec.StorageClass)
	})
	t.Run("hostpath with path", func(t *testing.T) {
		name, spec, err := ParseVolumeArg("name=upgrade,type=hostpath,path=/mnt/up")
		require.NoError(t, err)
		assert.Equal(t, ConsensusVolumeUpgrade, name)
		assert.Equal(t, "/mnt/up", spec.Path)
	})
	t.Run("RWOP accessMode accepted", func(t *testing.T) {
		_, spec, err := ParseVolumeArg("name=saved,type=pvc,accessMode=ReadWriteOncePod")
		require.NoError(t, err)
		assert.Equal(t, AccessModeReadWriteOncePod, spec.AccessMode)
	})
	errCases := map[string]string{
		"missing name":         "type=pvc,size=1Gi",
		"unknown key":          "name=saved,sizee=1Gi",
		"unknown volume":       "name=bogus,type=pvc",
		"unknown type":         "name=saved,type=nfs",
		"no equals":            "name=saved,pvc",
		"RWX accessMode":       "name=saved,type=pvc,accessMode=ReadWriteMany",
		"ROX accessMode":       "name=saved,type=pvc,accessMode=ReadOnlyMany",
		"typo accessMode":      "name=saved,type=pvc,accessMode=RWO",
		"lowercase accessMode": "name=saved,type=pvc,accessMode=readwriteonce",
	}
	for label, arg := range errCases {
		t.Run("error: "+label, func(t *testing.T) {
			_, _, err := ParseVolumeArg(arg)
			assert.Error(t, err)
		})
	}
}

func TestEffectiveSpec_Defaults(t *testing.T) {
	t.Run("empty config => emptydir", func(t *testing.T) {
		var cfg ConsensusVolumeConfig
		spec, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		require.NoError(t, err)
		assert.Equal(t, VolumeBackingEmptyDir, spec.Type)
	})

	t.Run("default pvc uses per-volume default size + RWO", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{Defaults: ConsensusVolumeDefaults{Type: VolumeBackingPVC}}
		spec, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		require.NoError(t, err)
		assert.Equal(t, VolumeBackingPVC, spec.Type)
		assert.Equal(t, "500Gi", spec.Size) // solo-charts dataSaved default
		assert.Equal(t, ConsensusDefaultPVCAccessMode, spec.AccessMode)
		assert.Equal(t, "", spec.StorageClass) // cluster default
	})

	t.Run("per-volume overrides win over defaults and builtin", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{
			Defaults: ConsensusVolumeDefaults{Type: VolumeBackingPVC, PVCSize: "10Gi", StorageClass: "standard"},
			Volumes:  map[string]ConsensusVolumeSpec{ConsensusVolumeSaved: {Size: "800Gi", StorageClass: "fast-ssd"}},
		}
		spec, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		require.NoError(t, err)
		assert.Equal(t, "800Gi", spec.Size)
		assert.Equal(t, "fast-ssd", spec.StorageClass)
		// A volume with no override falls to the global default size.
		other, err := cfg.EffectiveSpec(ConsensusVolumeStats)
		require.NoError(t, err)
		assert.Equal(t, "10Gi", other.Size)
	})

	t.Run("hostpath uses builtin default path", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{Volumes: map[string]ConsensusVolumeSpec{
			ConsensusVolumeUpgrade: {Type: VolumeBackingHostPath},
		}}
		spec, err := cfg.EffectiveSpec(ConsensusVolumeUpgrade)
		require.NoError(t, err)
		assert.Equal(t, "/opt/hgcapp/services-hedera/HapiApp2.0/data/upgrade", spec.Path)
	})
}

func TestEffectiveSpec_KeyValidation(t *testing.T) {
	t.Run("pvc key on hostpath is rejected", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{Volumes: map[string]ConsensusVolumeSpec{
			ConsensusVolumeSaved: {Type: VolumeBackingHostPath, Size: "10Gi"},
		}}
		_, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		assert.Error(t, err)
	})
	t.Run("path on pvc is rejected", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{Volumes: map[string]ConsensusVolumeSpec{
			ConsensusVolumeSaved: {Type: VolumeBackingPVC, Path: "/x"},
		}}
		_, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		assert.Error(t, err)
	})
	t.Run("size on emptydir is rejected", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{Volumes: map[string]ConsensusVolumeSpec{
			ConsensusVolumeSaved: {Type: VolumeBackingEmptyDir, Size: "10Gi"},
		}}
		_, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		assert.Error(t, err)
	})
}

func TestEffectiveSpec_AccessMode(t *testing.T) {
	t.Run("RWOP is accepted", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{Volumes: map[string]ConsensusVolumeSpec{
			ConsensusVolumeSaved: {Type: VolumeBackingPVC, AccessMode: AccessModeReadWriteOncePod},
		}}
		spec, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		require.NoError(t, err)
		assert.Equal(t, AccessModeReadWriteOncePod, spec.AccessMode)
	})
	t.Run("per-volume RWX is rejected", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{Volumes: map[string]ConsensusVolumeSpec{
			ConsensusVolumeSaved: {Type: VolumeBackingPVC, AccessMode: "ReadWriteMany"},
		}}
		_, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		assert.Error(t, err)
	})
	t.Run("global default ROX is rejected", func(t *testing.T) {
		cfg := ConsensusVolumeConfig{Defaults: ConsensusVolumeDefaults{Type: VolumeBackingPVC, AccessMode: "ReadOnlyMany"}}
		_, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		assert.Error(t, err)
	})
	t.Run("accessMode is not validated for non-pvc backings", func(t *testing.T) {
		// A stray global default accessMode is harmless when the volume is emptydir:
		// EffectiveSpec never reads it, so it must not error.
		cfg := ConsensusVolumeConfig{Defaults: ConsensusVolumeDefaults{AccessMode: "ReadWriteMany"}}
		spec, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		require.NoError(t, err)
		assert.Equal(t, VolumeBackingEmptyDir, spec.Type)
	})
}

func TestSetVolume_MergesKeys(t *testing.T) {
	var cfg ConsensusVolumeConfig
	cfg.SetVolume(ConsensusVolumeSaved, ConsensusVolumeSpec{Type: VolumeBackingPVC, Size: "500Gi", StorageClass: "fast-ssd"})
	cfg.SetVolume(ConsensusVolumeSaved, ConsensusVolumeSpec{Size: "600Gi"}) // override only size
	got := cfg.Volumes[ConsensusVolumeSaved]
	assert.Equal(t, "600Gi", got.Size)
	assert.Equal(t, "fast-ssd", got.StorageClass) // preserved
	assert.Equal(t, VolumeBackingPVC, got.Type)   // preserved
}

func TestLoadVolumeConfigYAML(t *testing.T) {
	t.Run("valid defaults + volumes", func(t *testing.T) {
		y := []byte(`
defaults:
  type: pvc
  pvc:
    size: 100Gi
    storageClass: standard
    accessMode: ReadWriteOnce
volumes:
  saved: { size: 500Gi, storageClass: fast-ssd }
  upgrade: { type: hostpath, path: /opt/up }
  logs: { type: emptydir }
`)
		cfg, err := LoadVolumeConfigYAML(y)
		require.NoError(t, err)
		assert.Equal(t, VolumeBackingPVC, cfg.Defaults.Type)
		assert.Equal(t, "100Gi", cfg.Defaults.PVCSize)
		assert.Equal(t, "standard", cfg.Defaults.StorageClass)

		saved, err := cfg.EffectiveSpec(ConsensusVolumeSaved)
		require.NoError(t, err)
		assert.Equal(t, VolumeBackingPVC, saved.Type)
		assert.Equal(t, "500Gi", saved.Size)
		assert.Equal(t, "fast-ssd", saved.StorageClass)

		up, err := cfg.EffectiveSpec(ConsensusVolumeUpgrade)
		require.NoError(t, err)
		assert.Equal(t, VolumeBackingHostPath, up.Type)
		assert.Equal(t, "/opt/up", up.Path)

		logs, err := cfg.EffectiveSpec(ConsensusVolumeLogs)
		require.NoError(t, err)
		assert.Equal(t, VolumeBackingEmptyDir, logs.Type)
	})
	t.Run("unknown volume rejected", func(t *testing.T) {
		_, err := LoadVolumeConfigYAML([]byte("volumes:\n  bogus: { type: pvc }\n"))
		assert.Error(t, err)
	})
	t.Run("invalid default type rejected", func(t *testing.T) {
		_, err := LoadVolumeConfigYAML([]byte("defaults:\n  type: nfs\n"))
		assert.Error(t, err)
	})
	t.Run("invalid default accessMode rejected", func(t *testing.T) {
		_, err := LoadVolumeConfigYAML([]byte("defaults:\n  type: pvc\n  pvc:\n    accessMode: ReadWriteMany\n"))
		assert.Error(t, err)
	})
	t.Run("invalid per-volume accessMode rejected", func(t *testing.T) {
		_, err := LoadVolumeConfigYAML([]byte("volumes:\n  saved: { type: pvc, accessMode: ReadOnlyMany }\n"))
		assert.Error(t, err)
	})
}
