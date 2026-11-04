// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"slices"

	"github.com/automa-saga/errx"
	"github.com/automa-saga/logx"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
)

// NewEntry starts the action history entry for intent run with inputs. Every
// writer builds its entry here; the writer fills in what the run did.
func NewEntry(_ context.Context, intent models.Intent, inputs any) ActionHistory {
	return ActionHistory{Intent: intent, Inputs: inputs}
}

// WithError returns e with err recorded the way an operator reads it: the
// message without its printable reason, which goes in Reason instead.
func (e ActionHistory) WithError(err error) ActionHistory {
	e.Error = doctor.OperatorMessage(err)
	if reason, ok := errx.ReasonOf(err); ok {
		e.Reason = reason
	}
	return e
}

// ReadActionHistory returns every entry of dir's action history, oldest
// first, and none when the file does not exist. An entry that can't be
// decoded, such as one a full disk cut short, is skipped with a warning so
// the rest stay readable.
func ReadActionHistory(dir string) ([]ActionHistory, error) {
	path := ActionHistoryFilePath(dir)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errorx.ExternalError.Wrap(err, "failed to read %s", path)
	}

	var entries []ActionHistory
	for i, doc := range historyDocuments(b) {
		var e ActionHistory
		if err := yaml.Unmarshal(doc, &e); err != nil {
			logx.As().Warn().Err(err).Str("path", path).Int("document", i).Msg("Skipping an action history entry that can't be decoded")
			continue
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// historyDocuments splits the history into its YAML documents. Every append
// starts its entry on a "---" line of its own, and yaml.Marshal never writes
// one inside a value, so each such line starts an entry.
func historyDocuments(b []byte) [][]byte {
	var docs [][]byte
	var cur []byte
	flush := func() {
		if len(bytes.TrimSpace(cur)) > 0 {
			docs = append(docs, cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 0, 64*1024), len(b)+1)
	for sc.Scan() {
		if sc.Text() == "---" {
			flush()
			continue
		}
		cur = append(cur, sc.Bytes()...)
		cur = append(cur, '\n')
	}
	flush()
	return docs
}

// everyStateFile is every name the action history gives a state file.
func everyStateFile() []ChangedFile {
	files := make([]ChangedFile, 0, len(AllComponentIDs)+2)
	for _, id := range AllComponentIDs {
		files = append(files, id.File())
	}
	return append(files, ChangedProvisioner, ChangedLegacy)
}

// targetFiles is, for each target, every state file its commands can change.
// A handler's managed components and a RunOwning call's ids must be in their
// target's row; NewBaseHandler and RunOwning refuse anything else.
var targetFiles = map[models.TargetType][]ChangedFile{
	// Install and upgrade also write the profile and firewall records, and an
	// install that creates the cluster writes the system record and stamps the
	// provisioner version.
	models.TargetBlockNode:     {ComponentBlockNode.File(), ComponentProfile.File(), ComponentFirewall.File(), ComponentSystem.File(), ChangedProvisioner},
	models.TargetConsensusNode: {ComponentConsensus.File(), ComponentProfile.File()},
	// kube cluster install writes the system record and stamps the version;
	// kube cluster uninstall removes every state file.
	models.TargetCluster: everyStateFile(),
	// The node agent is also a host binary, so it writes the system record.
	models.TargetTeleportNode:    {ComponentTeleport.File(), ComponentSystem.File()},
	models.TargetTeleportCluster: {ComponentTeleport.File()},
	models.TargetFirewall:        {ComponentFirewall.File()},
	// A startup migration pass can rewrite every state file.
	models.TargetSystem: everyStateFile(),
}

// TargetFiles returns every state file target's commands can change, in the
// order the action history lists them, or nil for a target that writes no
// state.
func TargetFiles(target models.TargetType) []ChangedFile {
	return slices.Clone(targetFiles[target])
}

// CheckTargetOwns fails when ids holds a component target's commands don't
// own, so a writer can't drift from the table diagnose tooling reads.
func CheckTargetOwns(target models.TargetType, ids []ComponentID) error {
	for _, id := range ids {
		if !slices.Contains(targetFiles[target], id.File()) {
			return errorx.IllegalArgument.New("target %q does not own the %s state file; add it to state.targetFiles", target, id)
		}
	}
	return nil
}
