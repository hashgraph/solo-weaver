// SPDX-License-Identifier: Apache-2.0

package reality

import (
	"github.com/hashgraph/solo-weaver/internal/state"
	"github.com/hashgraph/solo-weaver/pkg/models"
)

// liveConsensusShape holds the managed-shape fields the reality checker reads
// back from a live ConsensusCapsule (and the Orbit). It is an intermediate
// form: the checker converts it to state.ConsensusNodeObservedShape before
// storing it, so the drift producer can compare without importing this package.
type liveConsensusShape struct {
	ContainerName string
	CPULimit      string
	CPURequest    string
	MemoryLimit   string
	MemoryRequest string
	JavaHeapMin   string
	JavaHeapMax   string
	JavaOpts      string
	UCImageRepo   string
	UCImageTag    string

	Orbit               *state.ConsensusOrbitState
	Volumes             models.ConsensusVolumeConfig
	VolumesSet          bool
	ImagePullSecrets    models.PullSecretSelector
	ImagePullSecretsSet bool
	HostPathOwners      map[string]state.HostPathOwner
}

// liveShapeToObserved converts the checker's internal readback type into the
// state-package type that the drift producer consumes.
func liveShapeToObserved(l liveConsensusShape) *state.ConsensusNodeObservedShape {
	return &state.ConsensusNodeObservedShape{
		ContainerName:       l.ContainerName,
		CPULimit:            l.CPULimit,
		CPURequest:          l.CPURequest,
		MemoryLimit:         l.MemoryLimit,
		MemoryRequest:       l.MemoryRequest,
		JavaHeapMin:         l.JavaHeapMin,
		JavaHeapMax:         l.JavaHeapMax,
		JavaOpts:            l.JavaOpts,
		UCImageRepo:         l.UCImageRepo,
		UCImageTag:          l.UCImageTag,
		Orbit:               l.Orbit,
		Volumes:             l.Volumes,
		VolumesSet:          l.VolumesSet,
		ImagePullSecrets:    l.ImagePullSecrets,
		ImagePullSecretsSet: l.ImagePullSecretsSet,
		HostPathOwners:      l.HostPathOwners,
	}
}
