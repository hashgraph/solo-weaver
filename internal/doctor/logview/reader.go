// SPDX-License-Identifier: Apache-2.0

package logview

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
)

// Scan reads the file from the start and calls visit for each complete line that
// passes filter. Visit returns false to stop early. A missing file is an empty
// log. The returned offset is the end of the last complete line read, so a
// follower can resume there without a gap or a repeat.
func Scan(path, source string, filter Filter, visit func(Entry) bool) (int64, error) {
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, openError(err, path)
	}
	defer func() { _ = file.Close() }()

	var offset int64
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			// A last line without a newline is still being written; the follower
			// picks it up once it is complete.
			if errors.Is(err, io.EOF) {
				return offset, nil
			}
			return offset, errorx.ExternalError.Wrap(err, "failed to read log file %s", path)
		}
		offset += int64(len(line))

		entry := ParseLine(source, trimEOL(line))
		if filter.Match(entry) && !visit(entry) {
			return offset, nil
		}
	}
}

// Tail returns the last n entries that pass filter, and the offset to follow from.
func Tail(path, source string, filter Filter, n int) ([]Entry, int64, error) {
	var ring []Entry
	offset, err := Scan(path, source, filter, func(e Entry) bool {
		if n == 0 {
			return true
		}
		if len(ring) == n {
			copy(ring, ring[1:])
			ring = ring[:n-1]
		}
		ring = append(ring, e)
		return true
	})
	return ring, offset, err
}

// Head returns the first n entries that pass filter.
func Head(path, source string, filter Filter, n int) ([]Entry, error) {
	var out []Entry
	if n == 0 {
		return out, nil
	}
	_, err := Scan(path, source, filter, func(e Entry) bool {
		out = append(out, e)
		return len(out) < n
	})
	return out, err
}

// Merge combines entries from several sources in time order and keeps the last
// n (or the first n when fromStart is true). An entry without a time, such as a
// line that is not JSON, sorts with the entry before it in its own source, so
// each source keeps its file order.
func Merge(groups [][]Entry, n int, fromStart bool) []Entry {
	type keyed struct {
		entry Entry
		key   time.Time
	}

	var all []keyed
	for _, g := range groups {
		var last time.Time
		for _, e := range g {
			if !e.Time.IsZero() {
				last = e.Time
			}
			all = append(all, keyed{e, last})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].key.Before(all[j].key) })

	if len(all) > n {
		if fromStart {
			all = all[:n]
		} else {
			all = all[len(all)-n:]
		}
	}

	out := make([]Entry, 0, len(all))
	for _, k := range all {
		out = append(out, k.entry)
	}
	return out
}

// Follow streams entries appended to path after offset until ctx is done. It
// survives rotation (a new file at the same path) and truncation, and waits for
// the file to appear if it does not exist yet.
func Follow(ctx context.Context, path, source string, offset int64, filter Filter, poll time.Duration, emit func(Entry)) error {
	var (
		file    *os.File
		info    os.FileInfo
		pos     = offset
		partial []byte
	)
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()

	for {
		cur, statErr := os.Stat(path)

		// Drain the open file first so lines written just before a rotation are not lost.
		if file != nil {
			var err error
			if pos, partial, err = drain(file, pos, partial, source, filter, emit); err != nil {
				return err
			}
		}

		switch {
		case statErr == nil && (file == nil || !os.SameFile(info, cur)):
			if file != nil {
				_ = file.Close()
				pos, partial = 0, nil
			}
			f, err := os.Open(path)
			if err != nil {
				return openError(err, path)
			}
			file, info = f, cur
		case statErr == nil && cur.Size() < pos:
			pos, partial = 0, nil
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(poll):
		}
	}
}

// drain reads everything after pos and emits each complete line.
func drain(file *os.File, pos int64, partial []byte, source string, filter Filter, emit func(Entry)) (int64, []byte, error) {
	if _, err := file.Seek(pos, io.SeekStart); err != nil {
		return pos, partial, errorx.ExternalError.Wrap(err, "failed to seek log file %s", file.Name())
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return pos, partial, errorx.ExternalError.Wrap(err, "failed to read log file %s", file.Name())
	}
	pos += int64(len(data))

	buf := append(partial, data...)
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return pos, buf, nil
		}
		entry := ParseLine(source, trimEOL(buf[:i+1]))
		if filter.Match(entry) {
			emit(entry)
		}
		buf = buf[i+1:]
	}
}

func trimEOL(b []byte) string {
	return strings.TrimRight(string(b), "\r\n")
}

func openError(err error, path string) error {
	if errors.Is(err, fs.ErrPermission) {
		return errx.Decorate(errorx.ExternalError.Wrap(err, "failed to open log file %s", path),
			reasons.PermissionDenied, "Run with sudo, or check that the file is readable: ls -l "+path)
	}
	return errorx.ExternalError.Wrap(err, "failed to open log file %s", path)
}
