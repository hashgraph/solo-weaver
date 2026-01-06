// SPDX-License-Identifier: Apache-2.0

package node

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestPrepareUserInputs(t *testing.T) {
	// prepare a command with flags that common.Flag* helpers will read
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "", "profile")
	cmd.Flags().Bool("force", false, "force")
	if err := cmd.Flags().Set("profile", "test-profile"); err != nil {
		t.Fatalf("failed to set profile flag: %v", err)
	}
	if err := cmd.Flags().Set("force", "true"); err != nil {
		t.Fatalf("failed to set force flag: %v", err)
	}

	// set package-level flag variables that prepareUserInputs reads directly
	flagValuesFile = ""
	flagChartVersion = "1.2.3"
	flagChartRepo = "https://charts.example"
	flagNamespace = "myns"
	flagReleaseName = "myrel"
	flagBasePath = "/base"
	flagArchivePath = "/archive"
	flagLivePath = "/live"
	flagLogPath = "/log"
	flagLiveSize = "10Gi"
	flagArchiveSize = "20Gi"
	flagLogSize = "5Gi"
	flagNoReuseValues = false

	// set execution-related booleans (only one true to avoid ambiguity)
	flagContinueOnError = true
	flagStopOnError = false
	flagRollbackOnError = false

	// call the function under test
	ui, err := prepareUserInputs(cmd, []string{})
	if err != nil {
		t.Fatalf("prepareUserInputs returned error: %v", err)
	}

	// assertions
	if ui.Custom.Profile != "test-profile" {
		t.Fatalf("unexpected Profile: got %q want %q", ui.Custom.Profile, "test-profile")
	}
	if ui.Custom.Namespace != flagNamespace {
		t.Fatalf("unexpected Namespace: got %q want %q", ui.Custom.Namespace, flagNamespace)
	}
	if ui.Custom.ReleaseName != flagReleaseName {
		t.Fatalf("unexpected ReleaseName: got %q want %q", ui.Custom.ReleaseName, flagReleaseName)
	}
	if ui.Custom.ChartRepo != flagChartRepo {
		t.Fatalf("unexpected ChartRepo: got %q want %q", ui.Custom.ChartRepo, flagChartRepo)
	}
	if ui.Custom.ChartVersion != flagChartVersion {
		t.Fatalf("unexpected ChartVersion: got %q want %q", ui.Custom.ChartVersion, flagChartVersion)
	}
	if ui.Common.Force != true {
		t.Fatalf("unexpected Common.Force: got %v want %v", ui.Common.Force, true)
	}
	if ui.Custom.Storage.BasePath != flagBasePath {
		t.Fatalf("unexpected Storage.BasePath: got %q want %q", ui.Custom.Storage.BasePath, flagBasePath)
	}
	// ReuseValues should be the inverse of flagNoReuseValues
	if ui.Custom.ReuseValues != !flagNoReuseValues {
		t.Fatalf("unexpected ReuseValues: got %v want %v", ui.Custom.ReuseValues, !flagNoReuseValues)
	}
}
