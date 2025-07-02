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

package exc

import (
	"bufio"
	"context"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"golang.hedera.com/solo-provisioner/internal/nio"
	"os/exec"
	"strings"
	"testing"
)

func TestScriptExecution_Start(t *testing.T) {
	req := require.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdStreams, _, out, _ := nio.NewTestIOStreams()
	cmd := exec.Command("ps")
	cmd.Stdin = stdStreams.In
	cmd.Stdout = stdStreams.Out
	cmd.Stderr = stdStreams.ErrOut

	sc := NewCmdExecution(cmd, zerolog.Nop())
	sc.RunCmd(ctx)

	reader := bufio.NewReader(out)
	outString, err := reader.ReadString('\n') // read one line
	if err != nil {
		panic(err)
	}
	req.NotEmpty(outString)
	req.True(strings.Contains(outString, "PID TTY  "))
}
