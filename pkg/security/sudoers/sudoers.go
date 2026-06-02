// SPDX-License-Identifier: Apache-2.0

package sudoers

import (
	"strings"

	"github.com/joomcode/errorx"

	"github.com/hashgraph/solo-weaver/pkg/fsx"
)

// ValidateContent checks that content is structurally valid sudoers syntax.
// It joins continuation lines then verifies that each non-comment, non-empty
// logical line contains a host/runas separator ('='), a command tag (':'), and
// only absolute-path commands. This catches the most common malformations that
// would break sudo without requiring a shell invocation.
func ValidateContent(content []byte) error {
	raw := strings.Split(string(content), "\n")

	var logical []string
	var buf strings.Builder
	for _, line := range raw {
		stripped := strings.TrimRight(line, " \t")
		if strings.HasSuffix(stripped, "\\") {
			buf.WriteString(strings.TrimSuffix(stripped, "\\"))
			buf.WriteString(" ")
		} else {
			buf.WriteString(stripped)
			logical = append(logical, buf.String())
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		logical = append(logical, buf.String())
	}

	for _, line := range logical {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "=") {
			return errorx.IllegalFormat.New("sudoers line missing '=' separator: %q", line)
		}
		idx := strings.LastIndex(line, ":")
		if idx < 0 {
			return errorx.IllegalFormat.New("sudoers line missing command tag (e.g. NOPASSWD:): %q", line)
		}
		for _, cmd := range strings.Split(line[idx+1:], ",") {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				continue
			}
			if cmd != "ALL" && !strings.HasPrefix(cmd, "/") {
				return errorx.IllegalFormat.New("sudoers command must be an absolute path or ALL, got: %q", cmd)
			}
		}
	}
	return nil
}

// WriteEntry validates content then atomically installs it at dst with mode 0o440.
// Delegates to fsx.AtomicWriteFile so partial writes are never visible to sudo.
func WriteEntry(dst string, content []byte) error {
	if err := ValidateContent(content); err != nil {
		return errorx.IllegalFormat.Wrap(err, "sudoers content failed validation")
	}
	if err := fsx.AtomicWriteFile(dst, content, 0o440); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to write sudoers entry at %s", dst)
	}
	return nil
}

// Cleanup removes the installed sudoers entry at dst.
// Intended for rollback handlers; errors are logged to stdout but not returned.
func Cleanup(dst string) {
	fsx.Remove(dst)
}
