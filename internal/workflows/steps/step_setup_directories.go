package steps

import (
	"github.com/automa-saga/automa"
	"github.com/automa-saga/automa/automa_steps"
	"golang.hedera.com/solo-provisioner/internal/core"
	"path"
)

func SetupHomeDirectoryStructure() automa.Builder {
	return automa_steps.NewMkdirStep("home_directories", []string{
		path.Join(core.ProvisionerHomeDir, "bin"),
		path.Join(core.ProvisionerHomeDir, "logs"),
		path.Join(core.ProvisionerHomeDir, "config"),
	}, core.DefaultFilePerm)
}
