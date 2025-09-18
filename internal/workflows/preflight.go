package workflows

import (
	"context"
	"encoding/json"
	"github.com/automa-saga/automa"
	"github.com/zcalusic/sysinfo"
	"golang.hedera.com/solo-provisioner/pkg/logx"
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
			return automa.StepFailureReport("check-os", automa.WithDetail("requires superuser privilege")), nil
		}

		var si sysinfo.SysInfo
		si.GetSysInfo()
		data, err := json.MarshalIndent(&si, "", "  ")
		if err != nil {
			log.Fatal(err)
		}

		logx.As().Info().Str("system_info", string(data)).Msg("Retrieved system information")

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
