// SPDX-License-Identifier: Apache-2.0

//go:build linux

package mount

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/hashgraph/solo-weaver/internal/core"
	os2 "github.com/hashgraph/solo-weaver/pkg/os"
	"github.com/joomcode/errorx"
	"golang.org/x/sys/unix"
)

const (
	DefaultFstabFile = "/etc/fstab"
)

// use var to allow mocking in tests
var (
	fstabFile = DefaultFstabFile
)

type BindMount struct {
	Source string
	Target string
}

// fstabEntry represents an entry in /etc/fstab
type fstabEntry struct {
	source  string // Device or source path
	target  string // Mount point
	fsType  string // Filesystem type
	options string // Mount options
	dump    string // Dump frequency (usually 0)
	pass    string // Fsck pass number (usually 0)
}

// SetupBindMountsWithFstab sets up a bind mount for the given target
// It adds an entry to /etc/fstab and performs the mount
func SetupBindMountsWithFstab(mount BindMount) error {
	// Step 1: Add entry to /etc/fstab
	entry := fstabEntry{
		source:  mount.Source,
		target:  mount.Target,
		fsType:  "none",
		options: "bind,nofail",
		dump:    "0",
		pass:    "0",
	}

	if err := addFstabEntry(entry); err != nil {
		return err
	}

	// Step 2: Mount the bind mount
	err := setupBindMount(mount)
	if err != nil {
		return err
	}

	return nil
}

// RemoveBindMountsWithFstab undoes bind mount setup
// It unmounts the bind mount and removes the entry from /etc/fstab
func RemoveBindMountsWithFstab(mount BindMount) error {
	// Step 1: Unmount the bind mount
	if err := unmountBindMount(mount); err != nil {
		return err
	}

	// Step 2: Remove entry from /etc/fstab
	if err := removeFstabEntry(mount.Target); err != nil {
		return err
	}

	return nil
}

// String returns the fstab entry as a formatted string
func (e fstabEntry) String() string {
	return fmt.Sprintf("%s %s %s %s %s %s", e.source, e.target, e.fsType, e.options, e.dump, e.pass)
}

// IsBindMountedWithFstab checks if the bind mount is currently mounted
// and if the fstab entry exists
func IsBindMountedWithFstab(mount BindMount) (alreadyMounted bool, fstabEntryAlreadyAdded bool, err error) {
	// Check if bind mount is mounted
	mounted, err := isBindMounted(mount)
	if err != nil {
		return false, false, errorx.ExternalError.Wrap(err, "failed to check if %s is mounted on %s", mount.Source, mount.Target)
	}

	// Check if fstab entry exists
	entries, _, err := readFstab(fstabFile)
	if err != nil {
		return false, false, err
	}

	var entryExists bool
	for _, e := range entries {
		if e.target == mount.Target && e.source == mount.Source {
			entryExists = true
			break
		}
	}

	return mounted, entryExists, nil
}

// isBindMounted checks if the source is already bind mounted on the target
// by comparing device and inode numbers
func isBindMounted(mount BindMount) (bool, error) {
	// If either source or target doesn't exist, it's not mounted
	if _, err := os.Stat(mount.Source); os.IsNotExist(err) {
		return false, nil
	}
	if _, err := os.Stat(mount.Target); os.IsNotExist(err) {
		return false, nil
	}

	// Get file stats for source and target
	var sourceStat, targetStat unix.Stat_t
	if err := unix.Stat(mount.Source, &sourceStat); err != nil {
		return false, err
	}
	if err := unix.Stat(mount.Target, &targetStat); err != nil {
		return false, err
	}

	// For a bind mount, the target should point to the same inode as the source
	// We compare both device number and inode number
	return sourceStat.Dev == targetStat.Dev && sourceStat.Ino == targetStat.Ino, nil
}

// setupBindMount creates a bind mount from source to target
// It creates the target directory if it doesn't exist
func setupBindMount(mount BindMount) error {
	mounted, err := isBindMounted(mount)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to check if %s is mounted on %s", mount.Source, mount.Target)
	}
	if mounted {
		return nil // already mounted -> idempotent
	}

	// Create target directory if it doesn't exist
	if err := os.MkdirAll(mount.Target, core.DefaultDirOrExecPerm); err != nil {
		return errorx.ExternalError.Wrap(err, "failed to create directory %s", mount.Target)
	}

	// Perform the bind mount
	err = unix.Mount(mount.Source, mount.Target, "", unix.MS_BIND, "")
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to bind mount %s to %s", mount.Source, mount.Target)
	}

	return nil
}

// unmountBindMount unmounts the bind mount at the target
func unmountBindMount(mount BindMount) error {
	// Check if it's mounted
	mounted, err := isBindMounted(mount)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to check if %s is mounted", mount.Target)
	}
	if !mounted {
		return nil // not mounted, nothing to do
	}

	// Perform the unmount
	err = unix.Unmount(mount.Target, 0)
	if err != nil {
		return errorx.ExternalError.Wrap(err, "failed to unmount %s", mount.Target)
	}

	return nil
}

// parseFstabEntry parses a line from /etc/fstab into an fstabEntry
func parseFstabEntry(line string) (*fstabEntry, error) {
	// Remove comments
	if idx := strings.IndexByte(line, '#'); idx >= 0 {
		line = line[:idx]
	}

	fields := strings.Fields(line)
	if len(fields) < 4 {
		return nil, nil // Not a valid entry
	}

	entry := &fstabEntry{
		source:  fields[0],
		target:  fields[1],
		fsType:  fields[2],
		options: fields[3],
		dump:    "0",
		pass:    "0",
	}

	if len(fields) >= 5 {
		entry.dump = fields[4]
	}
	if len(fields) >= 6 {
		entry.pass = fields[5]
	}

	return entry, nil
}

// addFstabEntry adds an entry to /etc/fstab if it doesn't already exist
// It checks if an entry with the same target already exists before adding
func addFstabEntry(entry fstabEntry) error {
	entries, lines, err := readFstab(fstabFile)
	if err != nil {
		return err
	}

	// Check if entry already exists
	for _, e := range entries {
		if e.target == entry.target {
			// Entry already exists, nothing to do
			return nil
		}
	}

	// Add the new entry
	lines = append(lines, entry.String())

	return writeFstab(fstabFile, lines)
}

// removeFstabEntry removes an entry from /etc/fstab by target
func removeFstabEntry(target string) error {
	_, lines, err := readFstab(fstabFile)
	if err != nil {
		return err
	}

	var newLines []string
	var entryFound bool
	for _, line := range lines {
		entry, err := parseFstabEntry(line)
		if err != nil {
			return err
		}
		if entry != nil && entry.target == target {
			entryFound = true
			continue // Skip this entry, effectively removing it
		}
		newLines = append(newLines, line)
	}

	if !entryFound {
		return nil // Entry not found, nothing to do
	}

	return writeFstab(fstabFile, newLines)
}

// readFstab reads and parses /etc/fstab
func readFstab(fstabPath string) ([]*fstabEntry, []string, error) {
	file, err := os.Open(fstabPath)
	if err != nil {
		if os.IsNotExist(err) {
			// fstab doesn't exist, return empty
			return nil, nil, nil
		}
		return nil, nil, os2.ErrFileInaccessible.Wrap(err, "failed to open %s", fstabPath)
	}
	defer file.Close()

	var entries []*fstabEntry
	var lines []string

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)

		entry, err := parseFstabEntry(line)
		if err != nil {
			return nil, nil, err
		}
		if entry != nil {
			entries = append(entries, entry)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, os2.ErrFileRead.Wrap(err, "failed to scan %s", fstabPath)
	}

	return entries, lines, nil
}

// writeFstab writes lines to /etc/fstab
func writeFstab(fstabPath string, lines []string) error {
	// Get original file info for permissions
	info, err := os.Stat(fstabPath)
	var mode os.FileMode = core.DefaultFilePerm
	if err == nil {
		mode = info.Mode()
	}

	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	err = os.WriteFile(fstabPath, []byte(content), mode)
	if err != nil {
		return os2.ErrFileWrite.Wrap(err, "failed to write %s", fstabPath)
	}

	return nil
}
