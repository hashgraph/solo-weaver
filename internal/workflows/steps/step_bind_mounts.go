package steps

import "github.com/automa-saga/automa"

func SetupBindMounts() automa.Builder {
	return bashSteps.SetupBindMounts()
}
