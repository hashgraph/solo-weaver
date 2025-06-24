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

package models

import (
	"bytes"
	"context"
	"html/template"
)

// template related constants that are used in the config
// For example:
//
//	paths:
//		- path: "{{.HederaAppDir}}/services-hedera/HapiApp2.0/data/upgrade/current"
//	events:
//		- CREATE
//	filter: "*.mf"
//	execute:
//		command: "{{.NodeMgmtTools}}/bin/nmt-incron-dispatch.sh"
//		arguments:
//			- "{{.EventName}}"
//		- "{{.EventFile}}"
const (
	KeyHederaAppDir     = "HederaAppDir"
	KeyNodeMgmtToolsDir = "NodeMgmtToolsDir"
	KeyEventName        = "EventName"
	KeyEventFile        = "EventFile"
)

// ParseConfigTemplate parse the template string in the config
func ParseConfigTemplate(ctx context.Context, templateStr string, templateVars map[string]string) (string, error) {
	var parsed bytes.Buffer

	t, err := template.New("tmp").Parse(templateStr)
	if err != nil {
		return "", err
	}

	if err := t.Execute(&parsed, templateVars); err != nil {
		return "", err
	}

	return parsed.String(), nil
}

// ParseExecuteCommand parses WatchPath.execute config into command and arguments
func ParseExecuteCommand(ctx context.Context,
	wp WatchPath,
	templateVars map[string]string,
) (command string, arguments []string, err error) {
	cmdTemplates := append([]string{wp.Execute.Command}, wp.Execute.Arguments...)
	for i, cmdStr := range cmdTemplates {
		parsed, err := ParseConfigTemplate(ctx, cmdStr, templateVars)
		if err != nil {
			return "", nil, err
		}

		if parsed != "" {
			if i == 0 {
				command = parsed
			} else {
				arguments = append(arguments, parsed)
			}
		}
	}
	return command, arguments, nil
}
