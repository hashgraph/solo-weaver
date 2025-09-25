package steps

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/automa-saga/automa"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	"os/exec"
	"strings"
)

// Note: All these helper functions are set as variables so that we can mock as needed

// PrintWorkflowReport prints the workflow execution report in YAML format
var PrintWorkflowReport = func(report *automa.Report) {
	b, _ := yaml.Marshal(report)
	fmt.Printf("Workflow Execution Report:%s\n", b)
}

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

// generateKubeadmToken generates a random kubeadm token in the format [a-z0-9]{6}.[a-z0-9]{16}
var generateKubeadmToken = func() string {
	// 3 bytes = 6 hex chars, 8 bytes = 16 hex chars
	r := make([]byte, 11)
	_, err := rand.Read(r)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s.%s", hex.EncodeToString(r[:3]), hex.EncodeToString(r[3:]))
}
