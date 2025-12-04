package bll

/*
import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/config"
	"github.com/hashgraph/solo-weaver/internal/core"
)

type Effective struct {
	Intent *core.Intent
	Inputs *config.UserInputs
}


type BlocknodeRuntime struct {
	state *core.BlockNodeState
	inputs *core.BlocknodeInputs

}

func (b *BlocknodeRuntime) ChartVersion() *RValue[string]{
	return &RValue[string]{
		defaultValue: func() string {
			return "latest"
		},
		stateValue: func() string {
			return b.state.ChartVersion
		},
		userInput: func() string {
			if b.inputs.ChartVersion != "" {
				return b.inputs.ChartVersion
			}
			return b.state.ChartVersion
		},
	}
}

type RValue[T comparable] struct {
	defaultValue func() T
	stateValue func() T
	userInput    func() T
}

func(v* RValue[T]) Effective() T {
	return  v.userInput()
}


func checkBlocknodeIntent(intent core.Intent, inputs *config.UserInputs) (EffectiveIntent, error) {
	// intent: block node install --profile <profile> --blocknode-version v0.3 --storage-path /data/blocknode
	// inputs: version: v0.3, storage-path: /data/blocknode
	// current: version: v0.1 (on disk), storage-path: /mnt/fast-storage
	// reality: version: v0.1, storage-path: /mnt/fast-storage
	// allowed: NO
	//	- v.0.1 -> v.0.3: NO (version jump isn't allowed, needs to go through v0.2)
	//	- /mnt/fast-storage -> /data/blocknode: YES
	// effective:
	//    - version: v0.2
	//    - storage-path: /data/blocknode
	return nil, nil
}

func handleBlocknodeIntent(intent core.Intent, inputs *config.BlocknodeInputs) error {
	// intent: block node install --profile <profile> --blocknode-version v0.3 --chart-version 0.4.5 --storage-archive-path /data/blocknode
	// inputs: version: v0.3, storage-path: /data/blocknode
	// current: version: v0.1 (on disk), storage-path: /mnt/fast-storage
	// reality: version: v0.1, storage-path: /mnt/fast-storage
	// allowed: NO
	//	- v.0.1 -> v.0.3: NO (version jump isn't allowed, needs to go through v0.2)
	//	- /mnt/fast-storage -> /data/blocknode: YES
	// effective:

	// common
	    - reload state

	// handleBlocknodeInstall(intent core.Intent, inputs *config.UserInputs, state *core.BlockNodeState) error
	// handleVersion(intent core.Intent, inputs *config.UserInputs)
}

func handleInstall(intent core.Intent, inputs *config.UserInputs, state *core.BlockNodeState) error {
	//
}
*/
