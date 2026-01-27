// SPDX-License-Identifier: Apache-2.0

package version

import (
	_ "embed"
	"strings"
)

//go:embed COMMIT
var commit string

//go:embed VERSION
var number string

// buildMode is set at build time via ldflags for release builds
// -ldflags="-X 'github.com/hashgraph/solo-weaver/internal/version.buildMode=release'"
var buildMode string

func Commit() string {
	return commit
}

func Number() string {
	return number
}

// IsReleaseBuild returns true if this is a production release build.
// Release builds are created by the CI/CD pipeline with buildMode="release".
// Local/dev builds will return false.
func IsReleaseBuild() bool {
	return strings.TrimSpace(buildMode) == "release"
}

// BuildMode returns the current build mode ("release" or "dev")
func BuildMode() string {
	if IsReleaseBuild() {
		return "release"
	}
	return "dev"
}
