package steps

import (
	"os/exec"
	"strings"

	"github.com/joomcode/errorx"
)

// runCmd runs a bash command and returns its trimmed output or an error
var runCmd = func(script string) (string, error) {
	out, err := exec.Command("bash", "-c", script).Output()
	if err != nil {
		return "", errorx.IllegalState.Wrap(err, "failed to execute bash command: %s", script)
	}
	val := strings.TrimSpace(string(out))
	return val, nil
}
