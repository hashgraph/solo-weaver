/*
 * Copyright 2016-2023 Hedera Hashgraph, LLC
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

// Package erx markers exposes utility functions to trigger log markers that are required by DevOps as specified
// in the notion page: https://www.notion.so/swirldslabs/DevOps-NMT-Dependencies-92c4f85a4f0e4e20a6846af6e7c8baf6?pvs=4
//
// Note that, once external inspection tools are updated with the new log messages, this marker package can be removed.
package erx

import (
	"github.com/rs/zerolog"
)

// MarkNMTBackupStart logs a specific message denoting the start of Node Management Tools folder backup
func MarkNMTBackupStart(logger *zerolog.Logger) {
	logger.Info().Msg("Tools Snapshot: Beginning Node Management Tools Backup")
}

// MarkFatal logs an error message with FATAL keyword
func MarkFatal(logger *zerolog.Logger, msg string) {
	logger.Error().Msgf("FATAL: %s", msg)
}
