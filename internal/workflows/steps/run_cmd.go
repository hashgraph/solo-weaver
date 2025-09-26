package steps

import (
	"github.com/joomcode/errorx"
	"os/exec"
	"strings"
)

// RunCmd runs a command and returns an error if it fails
var RunCmd = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

// RunCmdOutput runs a bash command and returns its trimmed output or an error
var RunCmdOutput = func(script string) (string, error) {
	out, err := exec.Command("bash", "-c", script).Output()
	if err != nil {
		return "", errorx.IllegalState.Wrap(err, "failed to execute bash command: %s", script)
	}
	val := strings.TrimSpace(string(out))
	return val, nil
}
