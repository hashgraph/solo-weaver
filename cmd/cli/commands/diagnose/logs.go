// SPDX-License-Identifier: Apache-2.0

package diagnose

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/cmd/cli/commands/common"
	"github.com/hashgraph/solo-weaver/internal/doctor"
	"github.com/hashgraph/solo-weaver/internal/doctor/logview"
	"github.com/hashgraph/solo-weaver/internal/ui"
	"github.com/hashgraph/solo-weaver/pkg/models"
	"github.com/hashgraph/solo-weaver/pkg/reasons"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"
)

const (
	sourceCLI    = "cli"
	sourceDaemon = "daemon"
	sourceAll    = "all"

	cliLogFile    = "solo-provisioner.log"
	daemonLogFile = "solo-provisioner-daemon.log"

	followPoll = 250 * time.Millisecond
)

type logsOptions struct {
	source    string
	tail      int
	head      int
	follow    bool
	lastError bool
	filter    logview.FilterOptions
}

func newLogsCmd() *cobra.Command {
	var opts logsOptions

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Read, follow and filter the CLI and daemon logs",
		Long: "Read the CLI and daemon log files, which are written as JSON, and show them as readable " +
			"console lines. By default it shows the last 50 entries of the CLI log.\n\n" +
			"All filters must match. When any filter is set, lines that are not JSON are left out. " +
			"With --output json the matching entries are printed unchanged, one JSON object per line.",
		Example: `  # Follow the CLI log
  solo-provisioner diagnose logs -f

  # Follow the daemon log, warnings and above
  solo-provisioner diagnose logs --source daemon -f --level warn

  # Entries from both logs in the last hour for one field value
  solo-provisioner diagnose logs --source all --since 1h --field reason=UpgradeMonitorWatchClosed

  # The first 20 entries mentioning cilium, as JSON
  solo-provisioner diagnose logs --head 20 --grep cilium -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(cmd, opts)
		},
	}

	common.FlagLogsSource().SetVar(cmd, &opts.source, false)
	common.FlagLogsTail().SetVar(cmd, &opts.tail, false)
	common.FlagLogsHead().SetVar(cmd, &opts.head, false)
	common.FlagLogsFollow().SetVar(cmd, &opts.follow, false)
	common.FlagLogsLastError().SetVar(cmd, &opts.lastError, false)
	common.FlagLogsLevel().SetVar(cmd, &opts.filter.Level, false)
	common.FlagLogsSince().SetVar(cmd, &opts.filter.Since, false)
	common.FlagLogsGrep().SetVar(cmd, &opts.filter.Grep, false)
	common.FlagLogsMessage().SetVar(cmd, &opts.filter.Message, false)
	field := common.FlagLogsField()
	cmd.Flags().StringArrayVar(&opts.filter.Fields, field.Name, nil, field.Description)

	cmd.MarkFlagsMutuallyExclusive(common.FlagLogsTail().Name, common.FlagLogsHead().Name)
	cmd.MarkFlagsMutuallyExclusive(common.FlagLogsHead().Name, common.FlagLogsFollow().Name)
	for _, other := range []string{
		common.FlagLogsTail().Name, common.FlagLogsHead().Name,
		common.FlagLogsFollow().Name, common.FlagLogsLevel().Name,
	} {
		cmd.MarkFlagsMutuallyExclusive(common.FlagLogsLastError().Name, other)
	}

	common.SkipGlobalChecks(cmd)
	return cmd
}

type logSource struct {
	name string
	path string
}

func resolveSources(source string) ([]logSource, error) {
	dir := models.Paths().LogsDir
	cli := logSource{sourceCLI, filepath.Join(dir, cliLogFile)}
	daemon := logSource{sourceDaemon, filepath.Join(dir, daemonLogFile)}

	switch source {
	case sourceCLI:
		return []logSource{cli}, nil
	case sourceDaemon:
		return []logSource{daemon}, nil
	case sourceAll:
		return []logSource{cli, daemon}, nil
	default:
		return nil, errx.Decorate(errorx.IllegalArgument.New("unknown --source %q", source),
			reasons.InvalidArgument, "Use one of: cli, daemon, all.")
	}
}

func runLogs(cmd *cobra.Command, opts logsOptions) error {
	if opts.tail < 0 || opts.head < 0 {
		return errx.Decorate(errorx.IllegalArgument.New("--tail and --head must not be negative"),
			reasons.InvalidArgument, "Use 0 or a positive number of entries.")
	}

	sources, err := resolveSources(opts.source)
	if err != nil {
		return err
	}
	filter, err := logview.NewFilter(opts.filter, time.Now())
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	renderer := logview.Renderer{
		W:          out,
		Format:     logview.FormatText,
		Color:      isColorOutput(out),
		ShowSource: len(sources) > 1,
	}
	if common.OutputIsJSON() {
		renderer.Format = logview.FormatJSON
	}

	if opts.lastError {
		return showLastError(sources, opts.filter, renderer)
	}
	if cmd.Flags().Changed(common.FlagLogsHead().Name) {
		return showHead(sources, filter, opts.head, renderer)
	}
	return showTail(cmd.Context(), sources, filter, opts, renderer)
}

// showLastError prints the newest error-level entry across sources that also
// passes the user's filters, in the verbose -V layout (or raw with --output json).
func showLastError(sources []logSource, userOpts logview.FilterOptions, r logview.Renderer) error {
	errOpts := userOpts
	errOpts.Level = "error"
	filter, err := logview.NewFilter(errOpts, time.Now())
	if err != nil {
		return err
	}

	groups := make([][]logview.Entry, 0, len(sources))
	for _, s := range sources {
		entries, _, err := logview.Tail(s.path, s.name, filter, 1)
		if err != nil {
			return err
		}
		groups = append(groups, entries)
	}

	last := logview.Merge(groups, 1, false)
	if len(last) == 0 {
		if r.Format != logview.FormatJSON {
			fmt.Fprintln(r.W, "No error entries found.")
		}
		return nil
	}

	e := last[0]
	if r.Format == logview.FormatJSON {
		r.Write(e)
		return nil
	}
	path := ""
	for _, s := range sources {
		if s.name == e.Source {
			path = s.path
		}
	}
	doctor.RenderLoggedError(r.W, e, path, r.Color)
	return nil
}

func showHead(sources []logSource, filter logview.Filter, n int, r logview.Renderer) error {
	groups := make([][]logview.Entry, 0, len(sources))
	for _, s := range sources {
		entries, err := logview.Head(s.path, s.name, filter, n)
		if err != nil {
			return err
		}
		groups = append(groups, entries)
	}
	for _, e := range logview.Merge(groups, n, true) {
		r.Write(e)
	}
	return nil
}

func showTail(ctx context.Context, sources []logSource, filter logview.Filter, opts logsOptions, r logview.Renderer) error {
	groups := make([][]logview.Entry, 0, len(sources))
	offsets := make([]int64, 0, len(sources))
	for _, s := range sources {
		entries, offset, err := logview.Tail(s.path, s.name, filter, opts.tail)
		if err != nil {
			return err
		}
		groups = append(groups, entries)
		offsets = append(offsets, offset)
	}
	for _, e := range logview.Merge(groups, opts.tail, false) {
		r.Write(e)
	}
	if !opts.follow {
		return nil
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	emit := func(e logview.Entry) {
		mu.Lock()
		defer mu.Unlock()
		r.Write(e)
	}
	for i, s := range sources {
		wg.Add(1)
		go func(s logSource, offset int64) {
			defer wg.Done()
			if err := logview.Follow(ctx, s.path, s.name, offset, filter, followPoll, emit); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				cancel()
			}
		}(s, offsets[i])
	}
	wg.Wait()
	return firstErr
}

// isColorOutput reports whether out is an interactive terminal.
func isColorOutput(out io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := out.(*os.File)
	if !ok || f != os.Stdout {
		return false
	}
	return !ui.IsUnformatted()
}
