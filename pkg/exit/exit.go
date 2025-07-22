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

package exit

import (
	"fmt"
	"os"
)

type Code int

func (ec Code) String() string {
	return fmt.Sprintf("%d", ec)
}

func (ec Code) Int() int {
	return int(ec)
}

func (ec Code) TerminateProcess() {
	os.Exit(int(ec))
}

func (ec Code) Is(other int) bool {
	return int(ec) == other
}

const MinValidExitCode Code = 0
const MaxValidExitCode Code = 255

// POSIX standard exit code definitions.

const NormalTermination Code = 0
const GeneralError Code = 1
const UsageError Code = 64
const DataFormatError Code = 65
const MissingInputError Code = 66
const UserUnknown Code = 67
const HostUnknown Code = 68
const ServiceUnavailable Code = 69
const InternalError Code = 70
const SystemError Code = 71
const CriticalFileMissing Code = 72
const FileCreationError Code = 73
const InputOutputError Code = 74
const TemporaryFailure Code = 75
const ProtocolError Code = 76
const PermissionDenied Code = 77
const ConfigurationError Code = 78

// Application specific exit code definitions.
