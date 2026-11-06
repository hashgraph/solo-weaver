// SPDX-License-Identifier: Apache-2.0

//go:build linux

package policy

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/joomcode/errorx"
)

// ListInScratchNetns loads doc into a throwaway network namespace and returns
// nft's listing, so it compares with the live table as nft prints it.
func ListInScratchNetns(ctx context.Context, bin, doc string) ([]byte, error) {
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		// Never unlocked: Go retires the thread, and the namespace, on return.
		runtime.LockOSThread()
		if err := syscall.Unshare(syscall.CLONE_NEWNET); err != nil {
			done <- result{err: errorx.ExternalError.Wrap(err, "failed to create a scratch network namespace")}
			return
		}
		out, err := loadAndList(ctx, bin, doc)
		done <- result{out: out, err: err}
	}()
	r := <-done
	return r.out, r.err
}

// loadAndList runs `nft -f -` then `nft -j list table` in the calling thread's
// network namespace; children forked from a locked thread inherit it.
func loadAndList(ctx context.Context, bin, doc string) ([]byte, error) {
	load := exec.CommandContext(ctx, bin, "-f", "-")
	load.Stdin = strings.NewReader(doc)
	var stderr bytes.Buffer
	load.Stderr = &stderr
	if err := load.Run(); err != nil {
		return nil, errorx.ExternalError.Wrap(err, "loading the expected %s into a scratch namespace failed: %s",
			TableName, strings.TrimSpace(stderr.String()))
	}
	return ListTableJSON(ctx, bin)
}
