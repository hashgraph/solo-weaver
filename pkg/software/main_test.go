// SPDX-License-Identifier: Apache-2.0

package software

import (
	"os"
	"testing"

	"github.com/hashgraph/solo-weaver/pkg/config"
)

// TestMain is the entry point for all tests in the software package.
// It initialises the service account and logging (via config.Init) before
// any test runs, so that fsx.Manager can look up the "weaver" user/group
// and replaceAllInFile / WriteFile calls succeed without needing an
// import of internal/config from every individual test.
func TestMain(m *testing.M) {
	config.Init()
	os.Exit(m.Run())
}
