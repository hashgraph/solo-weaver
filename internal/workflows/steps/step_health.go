package steps

import (
	"github.com/automa-saga/automa"
)

func CheckClusterHealth() automa.Builder {
	return bashSteps.CheckClusterHealth()
}
