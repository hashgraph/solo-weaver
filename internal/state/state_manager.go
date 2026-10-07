// SPDX-License-Identifier: Apache-2.0

package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/automa-saga/logx"
	"github.com/automa-saga/version"
	"github.com/hashgraph/solo-weaver/pkg/fsx"
	"github.com/joomcode/errorx"
	"gopkg.in/yaml.v3"
	htime "helm.sh/helm/v3/pkg/time"
)

// Reader is the read-only view of managed application state.
// Consumers that only need to inspect state (e.g. resolvers, reality checkers)
// should depend on this narrow interface rather than the full DefaultStateManager.
type Reader interface {
	// State returns a snapshot of the current in-memory state.
	State() State
	// HasPersistedState reports whether a state file already exists on disk.
	HasPersistedState() (os.FileInfo, bool, error)
}

// Writer is the mutation-only view of managed application state.
// Consumers that record side-effects (e.g. the BLL after a workflow run) should
// depend on this narrow interface so that their dependencies are explicit.
//
// Set and AddActionHistory return Writer (not DefaultStateManager) so that
// callers depending only on Writer can chain calls without importing the full
// DefaultStateManager type.
type Writer interface {
	// Set replaces the entire in-memory state and returns the Writer for chaining.
	Set(s State) Writer
	// AddActionHistory appends an entry to the pending action log and updates
	// State.LastAction. Entries are flushed to disk on the next Flush() call.
	// Returns the Writer for chaining.
	AddActionHistory(entry ActionHistory) Writer
	// FlushState persists every component's state without flushing the action history.
	FlushState() error
	// FlushScoped persists only the listed components' state, leaving every
	// other component's file untouched — not read, not rewritten, not reverted.
	// A component with no prior baseline (first write) is written outright.
	FlushScoped(ids ...ComponentID) error
	// FlushActionHistory persists only the pending action history to disk without flushing the full state.
	FlushActionHistory() error
	// FlushAll persists both state and action history
	FlushAll() error
}

// Persister groups lifecycle operations (load + save) that are needed at the
// composition root (cmd layer) but not inside domain logic.
type Persister interface {
	// Refresh reloads the persisted state from disk, overwriting in-memory state.
	Refresh() error
	// FileManager returns the underlying file-system abstraction.
	FileManager() fsx.Manager
}

// Manager defines the interface for managing the application state with IO operations.
// It is just a thin wrapper around State with added thread-safe disk persistence & refresh operations.
// However, the State itself is not thread-safe for mutations since State is a data model.
// It composes Reader, Writer and Persister so existing callers need no changes.
type Manager interface {
	Reader
	Writer
	Persister
}

// DefaultStateManager encapsulates a State and all IO operations (flush/refresh).
type stateManager struct {
	mu        sync.Mutex
	flushMu   sync.Mutex // serializes Flush() calls
	state     State
	actions   []ActionHistory
	fm        fsx.Manager
	stateFile string
	// baselineHash is the canonical hash of each component's file as last read
	// from or written to disk. A component with no entry has never been
	// persisted by this manager (either no file exists yet, or Refresh has not
	// been called) and is written outright on its next flush.
	baselineHash map[ComponentID]string
}

// dir returns the directory holding every component's file and the shared
// action_history.yaml, derived from the configured state file's parent.
func (m *stateManager) dir() string {
	return filepath.Dir(m.state.StateFile)
}

type ManagerOption func(*stateManager) error

func WithFileManager(fm fsx.Manager) ManagerOption {
	return func(m *stateManager) error {
		if fm == nil {
			return errorx.IllegalArgument.New("file manager cannot be nil")
		}
		m.fm = fm
		return nil
	}
}

func WithState(s State) ManagerOption {
	return func(m *stateManager) error {
		m.state = s
		return nil
	}
}

func WithStateFile(path string) ManagerOption {
	return func(m *stateManager) error {
		m.state.StateFile = path
		return nil
	}
}

// NewStateManager creates a Manager with the provided options.
// Caller must call Refresh() to load the persisted state from disk before accessing the state.
func NewStateManager(opts ...ManagerOption) (Manager, error) {
	m := &stateManager{
		actions: []ActionHistory{},
	}

	for _, opt := range opts {
		if err := opt(m); err != nil {
			return nil, err
		}
	}

	if m.fm == nil {
		fm, err := fsx.NewManager()
		if err != nil {
			return nil, errorx.InternalError.Wrap(err, "failed to create file manager for state manager")
		}
		m.fm = fm
	}

	if m.state.StateFile == "" {
		m.state = NewState(m.stateFile)
	}

	return m, nil
}

// PersistProvisionerVersion records the running binary's version in the on-disk
// state file so version-boundary startup migrations are not re-evaluated — and
// non-idempotent ones (e.g. the Cilium agent restart) not re-run — on the next
// invocation.
//
// It Refresh()es first, so any existing reality-detected software/cluster/
// block-node fields are preserved and the optimistic-concurrency baseline is set;
// on a host with no state file it writes a minimal one from NewState defaults. A
// missing file is not an error. Call only after a successful startup-migration
// pass and only on a provisioned host, so a genuinely fresh machine keeps having
// no state file.
func PersistProvisionerVersion(opts ...ManagerOption) error {
	sm, err := NewStateManager(opts...)
	if err != nil {
		return err
	}

	if err := sm.Refresh(); err != nil && !errorx.IsOfType(err, NotFoundError) {
		return err
	}

	s := sm.State()
	s.ProvisionerState.Version = version.Get().Version
	return sm.Set(s).FlushState()
}

// State returns a copy of the current in-memory state (thread-safe).
// Returns a value copy so callers cannot mutate the manager's internals
// through the returned value.
func (m *stateManager) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// Set replaces the entire in-memory state (thread-safe) and returns the Writer
// for chaining.
func (m *stateManager) Set(s State) Writer {
	m.mu.Lock()
	defer m.mu.Unlock()
	logx.As().Debug().Any("newState", s).Msg("Setting new state in memory")
	m.state = s
	return m
}

// FileManager returns the file manager used by the state manager
func (m *stateManager) FileManager() fsx.Manager {
	return m.fm
}

// Refresh reloads every component's persisted file from disk with write lock.
// A component whose file is missing keeps its current in-memory value (its
// defaults from NewState, or whatever a prior Refresh/Set left it at) — the
// same "no file on disk, keep initial state" behavior the single-file version
// had, applied per component instead of to the whole state.
func (m *stateManager) Refresh() error {
	// Prevent Refresh from interleaving with an in-progress FlushState.
	m.flushMu.Lock()
	defer m.flushMu.Unlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	composed, err := m.state.Clone()
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to clone current state for refresh")
	}

	baseline := make(map[ComponentID]string, len(AllComponentIDs))
	dir := m.dir()

	for _, id := range AllComponentIDs {
		b, readErr := m.fm.ReadFile(componentFilePath(dir, id), -1)
		if readErr != nil {
			if errorx.IsOfType(readErr, fsx.FileNotFound) {
				continue // never persisted; this component keeps its current in-memory value
			}
			return errorx.InternalError.Wrap(readErr, "failed to read %s state file from %s", id, componentFilePath(dir, id))
		}

		var fileState State
		if err := yaml.Unmarshal(b, &fileState); err != nil {
			return errorx.InternalError.Wrap(err, "failed to unmarshal %s state from YAML", id)
		}
		applyComponentSection(composed, id, fileState)

		// Use the stored hash as the baseline if available (written by a prior
		// flush). Fall back to recomputing for hand-edited or legacy files
		// without a hash.
		if fileState.Hash != "" {
			baseline[id] = fileState.Hash
		} else {
			canonical, err := canonicalJSON(fileState.Hashable())
			if err != nil {
				return errorx.InternalError.Wrap(err, "failed to canonicalize refreshed %s state for baseline hash", id)
			}
			sum := sha256.Sum256(canonical)
			baseline[id] = hex.EncodeToString(sum[:])
		}
	}

	// Stamp the current CLI version so the provisioner.version field on disk
	// always reflects the binary that last ran, establishing the two-value
	// invariant used by startup migrations:
	//   lastCLIVersion  = the version read from disk before this Refresh()
	//   currentCLIVersion = version.Get().Version (the running binary)
	composed.ProvisionerState.Version = version.Get().Version

	// A pending action recorded by AddActionHistory before this Refresh (for
	// the flush this call is in service of) must survive the machine file's
	// on-disk value, which reflects the previous run's last action, not this
	// one's.
	composed.LastAction = m.state.LastAction

	m.state = *composed
	m.baselineHash = baseline

	return nil
}

// FlushState persists every component's state to disk with canonical hashing
// and atomic writes, one file per component.
func (m *stateManager) FlushState() error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()

	return m.flushComponents(AllComponentIDs)
}

// FlushScoped persists only the listed components, leaving every other
// component's file untouched.
func (m *stateManager) FlushScoped(ids ...ComponentID) error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()

	return m.flushComponents(ids)
}

func (m *stateManager) FlushActionHistory() error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()
	return m.flushActionHistory()
}

func (m *stateManager) FlushAll() error {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()

	if err := m.flushComponents(AllComponentIDs); err != nil {
		return err
	}

	if err := m.flushActionHistory(); err != nil {
		return err
	}

	return nil
}

func (m *stateManager) flushComponents(ids []ComponentID) error {
	for _, id := range ids {
		if err := m.flushComponent(id); err != nil {
			return err
		}
	}
	return nil
}

// flushComponent writes id's file with canonical hashing and an atomic write.
// It is the per-component equivalent of the single-file flushState this
// package had before the #1231 split; see
// docs/claude/plans/01231-per-component-state-files.md for why both the
// per-file hash check here and the per-file flock in AcquireComponentLocks
// exist side by side.
func (m *stateManager) flushComponent(id ComponentID) error {
	// Capture state and baseline under lock, then release before I/O.
	m.mu.Lock()
	full := m.state
	baseline := m.baselineHash[id] // "" when this component has no prior baseline
	m.mu.Unlock()

	path := componentFilePath(m.dir(), id)
	projected := projectComponentSection(full, id)

	logx.As().Debug().Str("component", string(id)).Any("state", projected).Msg("Flushing component state to disk")

	// Compute deterministic canonical JSON over only the domain record (envelope
	// fields and all LastSync timestamps are excluded by Hashable()).
	canonical, err := canonicalJSON(projected.Hashable())
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to create canonical representation of %s state for hashing", id)
	}
	sum := sha256.Sum256(canonical)
	newHashHex := hex.EncodeToString(sum[:])

	// Attach computed hash and real LastSync to the copy we will write.
	toWrite := projected
	toWrite.Hash = newHashHex
	toWrite.HashAlgo = "sha256"
	toWrite.LastSync = htime.Now()

	// Marshal YAML to write to disk.
	b, err := yaml.Marshal(toWrite)
	if err != nil {
		return errorx.InternalError.Wrap(err, "failed to marshal %s state to YAML", id)
	}

	// Optimistic concurrency: compare on-disk state against the BASELINE (what we
	// last read/wrote for this component), not against the new state. This
	// detects external changes without false-positives after Refresh() + Set()
	// cycles. It is a different guarantee than AcquireComponentLocks: the lock
	// stops two callers of this manager racing each other; this check catches a
	// change made by anything else (a hand edit, a process that bypassed the
	// lock) regardless of whether the lock was ever involved.
	if _, exists, err := m.fm.PathExists(path); err != nil {
		return errorx.InternalError.Wrap(err, "failed to stat %s state file before flush", id)
	} else if exists {
		if baseline == "" {
			return errorx.IllegalState.New("cannot flush %s without a baseline; call Refresh() first", id)
		}

		existing, err := m.fm.ReadFile(path, -1)
		if err != nil {
			return errorx.InternalError.Wrap(err, "failed to read %s state file before flush", id)
		}

		var existingState State
		if err := yaml.Unmarshal(existing, &existingState); err != nil {
			return errorx.IllegalState.New("%s state file at %s is not parseable YAML; aborting flush to avoid overwrite", id, path)
		}

		var diskHash string
		if existingState.Hash != "" {
			diskHash = existingState.Hash
		} else {
			// Fallback for hand-edited or legacy files without a stored hash.
			canonicalExisting, err := canonicalJSON(existingState.Hashable())
			if err != nil {
				return errorx.InternalError.Wrap(err, "failed to canonicalize existing %s state on disk", id)
			}
			sumExisting := sha256.Sum256(canonicalExisting)
			diskHash = hex.EncodeToString(sumExisting[:])
		}

		// Compare on-disk hash against baseline, NOT against the new hash.
		if diskHash != baseline {
			return errorx.IllegalState.New(
				"%s state file changed externally on disk at %s (expected baseline %s, found %s); aborting flush to avoid overwrite",
				id, path, baseline, diskHash,
			)
		}
	}

	// Atomic write: write to temp file in same directory and rename.
	if err := atomicWriteFile(path, b); err != nil {
		return errorx.InternalError.Wrap(err, "failed to write %s state file to %s", id, path)
	}

	// Update in-memory state's section, and advance the baseline hash.
	m.mu.Lock()
	applyComponentSection(&m.state, id, toWrite)
	if m.baselineHash == nil {
		m.baselineHash = make(map[ComponentID]string, len(AllComponentIDs))
	}
	m.baselineHash[id] = newHashHex // the new state is now this component's on-disk baseline
	m.mu.Unlock()

	return nil
}

func (m *stateManager) flushActionHistory() error {
	m.mu.Lock()
	snapshot := m.state
	pendingActions := make([]ActionHistory, len(m.actions))
	copy(pendingActions, m.actions)
	m.mu.Unlock()

	// Append action history entries.
	// Do this before writing the state file to ensure history is preserved even if the state file flush fails (since pending actions are cleared on successful flush).
	actionHistoryFile := filepath.Join(filepath.Dir(snapshot.StateFile), "action_history.yaml")
	for _, entry := range pendingActions {
		entryBytes, marshalErr := yaml.Marshal(entry)
		if marshalErr != nil {
			return errorx.InternalError.Wrap(marshalErr, "failed to marshal action history entry to YAML")
		}
		doc := append([]byte("---\n"), entryBytes...)
		if appendErr := m.fm.AppendToFile(actionHistoryFile, doc); appendErr != nil {
			return errorx.InternalError.Wrap(appendErr, "failed to append action history entry to %s", actionHistoryFile)
		}
	}

	// Update in-memory state
	m.mu.Lock()
	m.actions = []ActionHistory{}
	m.mu.Unlock()

	return nil
}

// canonicalJSON returns a deterministic JSON encoding of v where object keys are sorted.
// It marshals v to JSON, decodes with UseNumber to preserve numeric fidelity, then re-encodes canonically.
func canonicalJSON(v interface{}) ([]byte, error) {
	// Marshal typed value to JSON bytes.
	j, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	// Decode into interface{} using UseNumber so numbers become json.Number (stable textual form).
	var iface interface{}
	dec := json.NewDecoder(bytes.NewReader(j))
	dec.UseNumber()
	if err := dec.Decode(&iface); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := encodeCanonical(&buf, iface); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeCanonical writes a canonical JSON encoding of iface to buf with map keys sorted.
// Supports json.Number to preserve integer/float textual representation.
func encodeCanonical(buf *bytes.Buffer, iface interface{}) error {
	switch val := iface.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if val {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		// json.Number already preserves the original textual form.
		buf.WriteString(val.String())
	case float64:
		// Fallback if float64 reached (should be rare with UseNumber).
		b, _ := json.Marshal(val)
		buf.Write(b)
	case string:
		b, _ := json.Marshal(val)
		buf.Write(b)
	case []interface{}:
		buf.WriteByte('[')
		for i, elem := range val {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := encodeCanonical(buf, elem); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]interface{}:
		// sort keys for deterministic output
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			if err := encodeCanonical(buf, val[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		// Fallback: marshal using encoding/json
		b, err := json.Marshal(val)
		if err != nil {
			return err
		}
		buf.Write(b)
	}
	return nil
}

// HasPersistedState checks if the state file exists on disk
// HasPersistedState reports whether any component has a persisted file yet.
// A host can have some components persisted and others not (e.g. a cluster
// installed but no block node yet), so this is "any", not "all".
func (m *stateManager) HasPersistedState() (os.FileInfo, bool, error) {
	m.mu.Lock()
	dir := m.dir()
	m.mu.Unlock()

	for _, id := range AllComponentIDs {
		fi, exists, err := m.fm.PathExists(componentFilePath(dir, id))
		if err != nil {
			return nil, false, err
		}
		if exists {
			return fi, true, nil
		}
	}
	return nil, false, nil
}

// AddActionHistory adds an entry to the in-memory action history and updates the last action in the state.
// The action history is flushed to disk as part of the Flush() operation, and is stored in a separate history file to
// avoid unbounded growth of the main state file.
// The timestamp of the entry is set to the current time when adding to history to ensure consistency.
func (m *stateManager) AddActionHistory(entry ActionHistory) Writer {
	entry.Timestamp = htime.Now() // force the timestamp to be set to the current time when adding to history to ensure consistency
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, entry)
	m.state.LastAction = entry
	logx.As().Debug().Any("entry", entry).Msg("Added action history entry")
	return m
}
