package steps

import "github.com/automa-saga/automa"

const (
	LoadedByThisStep = automa.Key("loadedByThisStep")

	AlreadyInstalled     = "alreadyInstalled"
	AlreadyConfigured    = "alreadyConfigured"
	DownloadedByThisStep = "downloaded"
	ExtractedByThisStep  = "extracted"
	InstalledByThisStep  = "installed"
	CleanedUpByThisStep  = "cleanedUp"
	ConfiguredByThisStep = "configured"
)
