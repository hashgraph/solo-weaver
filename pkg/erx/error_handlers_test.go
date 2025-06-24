/*
 * Copyright 2016-2022 Hedera Hashgraph, LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package erx

import (
	"context"
	"errors"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/pkg/exit"
	"os"
	"os/exec"
	"testing"
)

// This needs to check for correct exit code
// Ref: https://stackoverflow.com/questions/26225513/how-to-test-os-exit-scenarios-in-go
// Note that code coverage will not include this test unfortunately
func TestCheckErr(t *testing.T) {
	if os.Getenv("ALLOW_OS_EXIT") == "1" {
		err := NewCommandError(errors.New("error in TestCheckErr"), exit.DataFormatError, "Error in TestCheckErr")
		TerminateIfError(context.Background(), err, zerolog.Nop())

		return
	}

	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(os.Environ(), "ALLOW_OS_EXIT=1")
	err := cmd.Run()
	var e *exec.ExitError
	if errors.As(err, &e) && exit.DataFormatError.Is(e.ExitCode()) {
		return
	}
	t.Fatalf("process ran with err %v, want exit code %d", err, exit.DataFormatError)

}
