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

package otl

import (
	"fmt"
	"runtime"
	"strings"
)

// getCallerFuncName returns the name of the caller function
// It returns the name of the caller of the caller of this function
func getCallerFuncName() string {
	pc, _, _, _ := runtime.Caller(3) // skip 3 to retrieve the caller outside the notel package
	parts := strings.Split(runtime.FuncForPC(pc).Name(), ".")
	funcName := parts[len(parts)-1]
	name := fmt.Sprintf("%s", funcName)
	return name
}
