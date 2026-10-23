// SPDX-License-Identifier: Apache-2.0

package common

import (
	"encoding/json"
	"fmt"
	"io"
)

// PrintStateWarnings shows the operator the state warnings no workflow summary
// took, such as a provisioner version or an action history entry the startup
// migrations could not record. It writes to w, which callers point at stderr:
// one NDJSON warn record per warning under --output=json, so stdout stays the
// command's own document, and one line per warning otherwise.
func PrintStateWarnings(w io.Writer, warnings []string) {
	for _, warning := range warnings {
		if OutputIsJSON() {
			b, err := json.Marshal(map[string]string{"level": "warn", "message": warning})
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintln(w, string(b))
			continue
		}
		_, _ = fmt.Fprintln(w, "⚠ "+warning)
	}
}
