package workflows

import (
	"context"
	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"github.com/zcalusic/sysinfo"
	"log"
	"os/user"
)

func CheckSysInfoStep() automa.Builder {
	return automa.NewStepBuilder("check-os", automa.WithOnExecute(func(context.Context) (*automa.Report, error) {
		current, err := user.Current()
		if err != nil {
			log.Fatal(err)
		}

		if current.Uid != "0" {
			return nil, errorx.IllegalState.New("requires superuser privilege")
		}

		var si sysinfo.SysInfo
		si.GetSysInfo()
		logx.As().Info().Interface("system_info", si).Msg("Retrieved system information")

		// TODO add required OS checks here

		return automa.StepSuccessReport("check-os"), nil
	}))
}
func NewPreflightWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder("preflight").Steps(
		CheckSysInfoStep(),
		//CheckDockerStep(),
	)
}

func SetupWorkflow() automa.Builder {
	return automa.NewWorkFlowBuilder("setup").Steps(
		NewPreflightWorkflow(),
	)
}
