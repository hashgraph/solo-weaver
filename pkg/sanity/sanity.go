// SPDX-License-Identifier: Apache-2.0

package sanity

import "github.com/joomcode/errorx"

var (
	ErrInvalidFilename = errorx.IllegalArgument.New("invalid filename")
)

// Alphanumeric ensures the input string to be ascii alphanumeric
func Alphanumeric(s string) string {
	sb := []byte(s)
	j := 0
	for _, b := range sb {
		if ('a' <= b && b <= 'z') ||
			('A' <= b && b <= 'Z') ||
			('0' <= b && b <= '9') {
			sb[j] = b
			j++
		}
	}
	return string(sb[:j])
}

// Filename sanitize the input string to be safe filename
// It only allows alphanumeric characters (a-z, 0-9) and underscore
// It returns error if the filename is empty string after the sanitization
func Filename(s string) (string, error) {
	sb := []byte(s)
	j := 0
	for _, b := range sb {
		if ('a' <= b && b <= 'z') ||
			('A' <= b && b <= 'Z') ||
			('0' <= b && b <= '9') ||
			b == '_' ||
			b == '-' {
			sb[j] = b
			j++
		}
	}

	if j == 0 {
		return "", ErrInvalidFilename
	}

	return string(sb[:j]), nil
}
