// SPDX-License-Identifier: Apache-2.0

package core

import (
	"github.com/automa-saga/automa"
	"github.com/hashgraph/solo-weaver/internal/config"
)

// WorkflowExecutionOptions defines options for setting up various components of the cluster
type WorkflowExecutionOptions struct {
	ExecutionMode automa.TypeMode
	RollbackMode  automa.TypeMode
}

type CommonInputs struct {
	Force            bool
	NodeType         string
	ExecutionOptions WorkflowExecutionOptions
}

type UserInputs[T any] struct {
	Common CommonInputs
	Custom T
}

type BlocknodeInputs struct {
	Profile      string
	Version      string
	Namespace    string
	Release      string
	Chart        string
	ChartVersion string
	Storage      config.BlockNodeStorage
	ValuesFile   string
	ReuseValues  bool
}
