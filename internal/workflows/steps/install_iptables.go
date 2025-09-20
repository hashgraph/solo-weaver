package steps

import (
	"context"
	"github.com/automa-saga/automa"
)

func InstallIpTables() automa.Builder {
	return automa.NewStepBuilder("install-iptables", automa.WithOnExecute(func(ctx context.Context) (*automa.Report, error) {
		// TODO implement iptables installation logic here

		return automa.StepSuccessReport("install-iptables"), nil
	}))
}
