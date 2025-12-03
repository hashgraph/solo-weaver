// SPDX-License-Identifier: Apache-2.0

package steps

import "github.com/automa-saga/automa"

const (
	LoadedByThisStep  = automa.Key("loadedByThisStep")
	ConfigurationFile = "configurationFile"

	AlreadyInstalled      = "alreadyInstalled"
	AlreadyConfigured     = "alreadyConfigured"
	ServiceAlreadyEnabled = "serviceAlreadyEnabled"
	ServiceAlreadyRunning = "serviceAlreadyRunning"

	ServiceEnabledByThisStep = "serviceEnabled"
	ServiceStartedByThisStep = "serviceStarted"

	DownloadedByThisStep = "downloaded"
	ExtractedByThisStep  = "extracted"
	InstalledByThisStep  = "installed"
	CleanedUpByThisStep  = "cleanedUp"
	ConfiguredByThisStep = "configured"

	IsReady   = "isReady"
	IsPending = "isPending"
)
