// SPDX-License-Identifier: Apache-2.0

package models

import "github.com/automa-saga/automa"

func AllExecutionModes() []automa.TypeMode {
	return []automa.TypeMode{automa.RollbackOnError, automa.StopOnError, automa.ContinueOnError}
}
