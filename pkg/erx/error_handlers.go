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
	"github.com/cockroachdb/errors"
	"github.com/rs/zerolog"
	"os"
)

// TerminateIfError terminates the process if there is an error
func TerminateIfError(ctx context.Context, err error, logger zerolog.Logger) {
	if err != nil {
		MarkFatal(&logger, err.Error())
		logger.Error().Err(err).Msgf("%+v", err)
		if errors.Is(err, &CommandError{}) {
			err.(*CommandError).ExitCode().TerminateProcess()
		} else {
			os.Exit(-1)
		}
	}
}
