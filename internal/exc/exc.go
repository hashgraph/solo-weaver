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
	"context"
	"github.com/rs/zerolog"
	"golang.hedera.com/solo-provisioner/internal/otl"
	"os"
	"os/exec"
	"syscall"
)

// CmdExecution executes a command and manages its lifecycle
// It forcefully terminates the child process if ctx.Done() signal is received
type CmdExecution struct {
	done   chan bool
	cmd    *exec.Cmd
	logger *zerolog.Logger
}

func NewCmdExecution(cmd *exec.Cmd, logger zerolog.Logger) *CmdExecution {
	sc := &CmdExecution{
		done:   make(chan bool),
		cmd:    cmd,
		logger: &logger,
	}

	return sc
}

// StopCmd gracefully stops the command execution
func (sc *CmdExecution) StopCmd(ctx context.Context) {
	ctx, span := otl.StartSpan(ctx)
	defer otl.EndSpan(ctx, span)
	close(sc.done)
}

// RunCmd starts running the command while monitoring any ctx.Done() signal
func (sc *CmdExecution) RunCmd(ctx context.Context) error {
	ctx, span := otl.StartSpan(ctx)
	defer otl.EndSpan(ctx, span)

	curDir, err := os.Getwd()
	if err != nil {
		return err
	}

	defer func() {
		sc.StopCmd(ctx)
	}()

	// start the command
	sc.logger.Debug().
		Str(logFields.execCmd, sc.cmd.String()).
		Str(logFields.execDir, curDir).
		Msg("Executing command")

	// start the command
	sc.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := sc.cmd.Start(); err != nil {
		return err
	}

	// monitor for interrupt signals to forcefully terminate the command process if needed
	go func() {
		select {
		case <-ctx.Done():
			sc.logger.Debug().
				Str(logFields.execCmd, sc.cmd.String()).
				Str(logFields.execDir, curDir).
				Int(logFields.execPid, sc.cmd.Process.Pid).
				Msg("Force terminating command")

			err = syscall.Kill(sc.cmd.Process.Pid, syscall.SIGKILL)
			if err != nil {
				sc.logger.Warn().
					Int(logFields.execPid, sc.cmd.Process.Pid).
					Err(err).
					Msg("Error occurred while terminating the process")
			}

			return
		case <-sc.done: // stop this coroutine
			return
		}
	}()

	// wait for the command to finish
	sc.logger.Debug().
		Str(logFields.execCmd, sc.cmd.String()).
		Int(logFields.execPid, sc.cmd.Process.Pid).
		Msg("Waiting for command to finish execution")

	otl.EndSpan(ctx, span) // end span so that we can have an early trace. Avoid adding logs afterward
	if err = sc.cmd.Wait(); err != nil {
		return err
	}

	return nil
}
