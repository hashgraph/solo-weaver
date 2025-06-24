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

package detect

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseMemorySize(t *testing.T) {
	req := require.New(t)

	var testCases = []struct {
		input  string
		output uint64
		err    string
	}{
		{
			input:  "1k",
			output: 1e3,
			err:    "",
		},
		{
			input:  "1kb",
			output: 1e3,
			err:    "",
		},
		{
			input:  "1m",
			output: 1e6,
			err:    "",
		},
		{
			input:  "1mb",
			output: 1e6,
			err:    "",
		},
		{
			input:  "1g",
			output: 1e9,
			err:    "",
		},
		{
			input:  "1gb",
			output: 1e9,
			err:    "",
		},
		{
			input:  "1t",
			output: 1e12,
			err:    "",
		},
		{
			input:  "1tb",
			output: 1e12,
			err:    "",
		},
		{
			input:  "1p",
			output: 1e15,
			err:    "",
		},
		{
			input:  "1pb",
			output: 1e15,
			err:    "",
		},
		{
			input:  "1",
			output: 1,
			err:    "",
		},
		{
			input:  "1K",
			output: 1e3,
			err:    "",
		},
		{
			input:  "1KB",
			output: 1e3,
			err:    "",
		},
		{
			input:  "1M",
			output: 1e6,
			err:    "",
		},
		{
			input:  "1MB",
			output: 1e6,
			err:    "",
		},
		{
			input:  "1G",
			output: 1e9,
			err:    "",
		},
		{
			input:  "1GB",
			output: 1e9,
			err:    "",
		},
		{
			input:  "1T",
			output: 1e12,
			err:    "",
		},
		{
			input:  "1TB",
			output: 1e12,
			err:    "",
		},
		{
			input:  "1P",
			output: 1e15,
			err:    "",
		},
		{
			input:  "1PB",
			output: 1e15,
			err:    "",
		},
		{
			input:  "     1k     ", // test trim
			output: 1e3,
			err:    "",
		},
		{
			input:  "1x", // test invalid unit
			output: 0,
			err:    "invalid suffix: 'x'",
		},
		{
			input:  "1!@#$$x*", // test wrong unit specifier
			output: 0,
			err:    "invalid suffix: '!@#$$x*'",
		},
		{
			input:  "!@#$$x*", // test wrong number
			output: 0,
			err:    "invalid size: '!@#$$x*'",
		},
		{
			input:  "1!@#$$x %", // test wrong number
			output: 0,
			err:    "parsing \"1!@#$$x\": invalid syntax",
		},
	}

	for _, testCase := range testCases {
		val, err := ParseMemorySizeInBytes(testCase.input)
		if testCase.err != "" {
			req.Error(err)
			req.Contains(err.Error(), testCase.err)
		}
		req.Equal(testCase.output, val)

	}

}

func TestHumanizeBytes(t *testing.T) {
	req := require.New(t)
	req.Equal("1GB", HumanizeBytes(1e9))
	req.Equal("1.01GB", HumanizeBytes(1.01e9))
	req.Equal("1.012GB", HumanizeBytes(1.01234567e9))
}

func TestAddBuffer(t *testing.T) {
	req := require.New(t)
	req.Equal(uint64(0), AddBuffer(0))
	req.Equal(uint64(150), AddBuffer(100))
}

func TestHumanizeBytesAsJavaSpec(t *testing.T) {
	req := require.New(t)
	testCases := []struct {
		size uint64
		str  string
	}{
		{
			size: 0,
			str:  "0b",
		},
		{
			size: 1e3,
			str:  "1k",
		},
		{
			size: 1e6,
			str:  "1m",
		},
		{
			size: 1e9,
			str:  "1g",
		},
		{
			size: 1e12,
			str:  "1t",
		},
		{
			size: 1e15,
			str:  "1p",
		},
		{
			size: 1e18,
			str:  "1e",
		},
	}

	for _, test := range testCases {
		req.Equal(test.str, HumanizeBytesAsJavaSpec(test.size))
	}
}
