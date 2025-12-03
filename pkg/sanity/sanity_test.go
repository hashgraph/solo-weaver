// SPDX-License-Identifier: Apache-2.0

package sanity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanity_Alphanumeric(t *testing.T) {
	req := require.New(t)
	testCases := []struct {
		input  string
		output string
	}{
		{
			input:  "a,bc9",
			output: "abc9",
		},
		{
			input:  "a-,bc_9!",
			output: "abc9",
		},
		{
			input:  "",
			output: "",
		},
	}

	for _, testCase := range testCases {
		req.Equal(testCase.output, Alphanumeric(testCase.input), testCase.input)

	}
}

func TestSanity_Filename(t *testing.T) {
	req := require.New(t)
	testCases := []struct {
		input  string
		output string
		err    error
	}{
		{
			input:  "a,bc9",
			output: "abc9",
		},
		{
			input:  "_a-,bc_9!",
			output: "_a-bc_9",
		},
		{
			input:  "\\u2318",
			output: "u2318",
		},
		{
			input:  "日本語",
			output: "",
			err:    ErrInvalidFilename,
		},
		{
			input:  "⌘",
			output: "",
			err:    ErrInvalidFilename,
		},
		{
			input:  "",
			output: "",
			err:    ErrInvalidFilename,
		},
	}

	for _, testCase := range testCases {
		output, err := Filename(testCase.input)
		req.Equal(testCase.output, output, testCase.input)
		req.Equal(testCase.err, err, testCase.input)
	}
}
