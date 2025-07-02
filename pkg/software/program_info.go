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

package software

import "os"

// programInfo implements ProgramInfo interface
type programInfo struct {
	path       string
	mode       os.FileMode
	version    string
	sha256Hash string
}

func (p *programInfo) GetVersion() string {
	return p.version
}

func (p *programInfo) GetHash() string {
	return p.sha256Hash
}

func (p *programInfo) GetFileMode() os.FileMode {
	return p.mode
}

func (p *programInfo) GetPath() string {
	return p.path
}

func (p *programInfo) IsExecAll() bool {
	return p.mode&0111 == 0111
}

func (p *programInfo) IsExecAny() bool {
	return p.mode&0111 != 0
}

func (p *programInfo) IsExecOwner() bool {
	return p.mode&0100 != 0
}

func (p *programInfo) IsExecGroup() bool {
	return p.mode&0010 != 0
}
