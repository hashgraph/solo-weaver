package steps

import "github.com/automa-saga/logx"

func init() {
	var err error

	bashSteps = initBashScriptSteps()
	bashStepsRegistry, err = initBashScriptBasedStepRegistry()
	if err != nil {
		logx.As().Fatal().Err(err).Msg("bash step registry creation failed")
	}

	// initialize the core step registry here
}
