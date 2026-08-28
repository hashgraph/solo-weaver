// SPDX-License-Identifier: Apache-2.0

package doctor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/automa-saga/logx"
	"path"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/errx"
	"github.com/hashgraph/solo-weaver/pkg/config"
	"github.com/hashgraph/solo-weaver/pkg/models"

	"github.com/automa-saga/version"
	"github.com/joomcode/errorx"
)

type ErrorDiagnosis struct {
	Error              error             `yaml:"error" json:"error"`
	Message            string            `yaml:"message" json:"message"`
	Cause              string            `yaml:"cause" json:"cause"`
	ErrorType          string            `yaml:"errorType" json:"errorType"`
	TraceId            string            `yaml:"traceId" json:"traceId"`
	Commit             string            `yaml:"commit" json:"commit"`
	Version            string            `yaml:"version" json:"version"`
	GoVersion          string            `yaml:"goVersion" json:"goVersion"`
	Pid                int               `yaml:"pid" json:"pid"`
	StackTrace         []string          `yaml:"stackTrace" json:"stackTrace"`
	Code               int               `yaml:"code" json:"code"`
	Logfile            string            `yaml:"log" json:"log"`
	ProfilingSnapshots map[string]string `yaml:"ProfilingSnapshots" json:"profilingSnapshots"`
	Resolution         []string          `yaml:"steps" json:"steps"`
	WhyFloor           string            `yaml:"whyFloor" json:"whyFloor"`

	// Reason is the errx reason code, "" when the call site is not decorated.
	Reason string `yaml:"reason" json:"reason"`
}

func toErrorCode(err error) int {
	switch {
	case errorx.IsOfType(err, errorx.IllegalArgument):
		return 10400
	default:
		if errorx.HasTrait(err, errorx.NotFound()) {
			return 10404
		}
		return 10500
	}
}

func toErrorMessage(err error) (string, string) {
	e := errorx.Cast(err)
	if e == nil {
		return err.Error(), ""
	}

	return e.Message(), OperatorMessage(e.Cause())
}

// OperatorMessage renders err for a human surface (the TUI failure line, the
// panel's Cause line): err.Error() minus errx's reason code, which those surfaces
// already show separately. Log lines keep it inline — see docs/dev/error-handling.md.
func OperatorMessage(err error) string {
	if err == nil {
		return ""
	}

	// err.Error() renders the whole cause chain, so every reason along it leaks —
	// not just the outermost one errx.ReasonOf reports.
	msg := err.Error()
	for _, reason := range reasonsInChain(err) {
		msg = stripPrintableProperty(msg, "reason", reason)
	}

	return msg
}

// reasonsInChain returns each distinct reason value attached along err's chain,
// outermost first. Shares walkChain's blind spot: a reason on an underlying
// error still renders, exactly as errx.ReasonOf cannot see one.
func reasonsInChain(err error) []string {
	var out []string
	seen := map[string]bool{}

	walkChain(err, func(e error) bool {
		if v, ok := errorx.ExtractProperty(e, errx.PropertyReason); ok {
			if s, ok := v.(string); ok && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
		return true
	})

	return out
}

// stripPrintableProperty removes one "<label>: <value>" pair from the "{...}" group
// errorx renders printable properties into, dropping a group left empty.
func stripPrintableProperty(msg, label, value string) string {
	pair := label + ": " + value

	msg = strings.ReplaceAll(msg, pair+", ", "")
	msg = strings.ReplaceAll(msg, ", "+pair, "")
	msg = strings.ReplaceAll(msg, " {"+pair+"}", "")
	msg = strings.ReplaceAll(msg, "{"+pair+"}", "")

	return strings.TrimSpace(msg)
}

func findResolution(err error) []string {
	// Covers errx.WithHints and direct property attachments alike.
	if hints, ok := resolutionHints(err); ok {
		return hints
	}

	switch {
	case errorx.IsOfType(err, errorx.IllegalArgument):
		if arg, ok := errorx.ExtractProperty(err, errorx.PropertyPayload()); ok {
			return []string{fmt.Sprintf("Ensure %q is provided.", arg.(string))}
		}
		return []string{fmt.Sprintf("Ensure all required arguments are provided with valid values.")}
	case errorx.IsOfType(err, errorx.IllegalFormat):
		return []string{"Ensure provided data is in correct format."}
	case errorx.IsOfType(err, NotFoundError):
		if arg, ok := errorx.ExtractProperty(err, errorx.PropertyPayload()); ok {
			return []string{fmt.Sprintf("Ensure configuration file %q exists, is correctly formatted and accessible", arg.(string))}
		}
		return []string{"Ensure configuration file exists and is accessible."}
	case errorx.IsOfType(err, errorx.NotImplemented):
		return []string{"This feature is not yet implemented. Contact support for more information."}
	default:
		return []string{"Check error message for details or contact support"}
	}
}

// walkChain calls fn for each error in err's chain, outermost first, until fn
// returns false. Mirrors errx.extract (unexported): walks the errorx causes and
// errors.Unwrap, so it cannot see less than errx.Hints / errx.ReasonOf do.
func walkChain(err error, fn func(error) bool) {
	for e := err; e != nil; {
		if !fn(e) {
			return
		}
		if ex := errorx.Cast(e); ex != nil {
			e = ex.Cause()
			continue
		}
		e = errors.Unwrap(e)
	}
}

// extractProperty returns key's value from anywhere in err's chain, outermost first.
func extractProperty(err error, key errorx.Property) (any, bool) {
	var value any
	var found bool

	walkChain(err, func(e error) bool {
		value, found = errorx.ExtractProperty(e, key)
		return !found
	})

	return value, found
}

// resolutionHints returns the hints attached anywhere in err's chain, outermost
// first. New code attaches []string; the bare-string branch carries the legacy
// sites still awaiting their package's migration — see docs/dev/error-handling.md.
func resolutionHints(err error) ([]string, bool) {
	resolution, ok := extractProperty(err, ErrPropertyResolution)
	if !ok {
		return nil, false
	}
	if steps, ok := resolution.([]string); ok {
		return steps, len(steps) > 0
	}
	if s, ok := resolution.(string); ok {
		return []string{s}, s != ""
	}
	return []string{fmt.Sprintf("%s", resolution)}, true
}

func findWhyFloor(err error) string {
	if why, ok := extractProperty(err, models.ErrPropertyWhyFloor); ok {
		return fmt.Sprintf("%s", why)
	}
	return ""
}

func takeProfilingSnapshots(ex error) map[string]string {
	timestamp := time.Now().Format("20060102-150405")

	snapshotDir := path.Join(models.Paths().DiagnosticsDir, timestamp)
	if err := os.MkdirAll(snapshotDir, models.DefaultDirOrExecPerm); err != nil {
		log.Printf("failed to create logs directory: %v", err)
		return nil
	}

	files := make(map[string]string)

	// Error stack trace
	stacktraceFile := filepath.Join(snapshotDir, "stacktrace-"+timestamp+".txt")
	f, err := os.Create(stacktraceFile)
	if err == nil {
		if ex != nil {
			_, _ = fmt.Fprintf(f, "%+v\n", ex)
			files["stacktrace"] = stacktraceFile
		} else {
			// Capture current stack trace if no error provided
			buf := make([]byte, 1<<16)
			n := runtime.Stack(buf, true)
			_, _ = f.Write(buf[:n])
			files["stacktrace"] = stacktraceFile
		}
		f.Close()
	}

	// CPU profile
	cpuFile := filepath.Join(snapshotDir, "pprof-cpu-"+timestamp+".pb.gz")
	f, err = os.Create(cpuFile)
	if err == nil {
		if err := pprof.StartCPUProfile(f); err == nil {
			time.Sleep(2 * time.Second)
			pprof.StopCPUProfile()
			files["cpu"] = cpuFile
		} else {
			log.Printf("failed to start CPU profile: %v", err)
		}
		f.Close()
	} else {
		log.Printf("failed to create CPU profile file: %v", err)
	}

	// Heap profile
	heapFile := filepath.Join(snapshotDir, "pprof-heap-"+timestamp+".pb.gz")
	f, err = os.Create(heapFile)
	if err == nil {
		runtime.GC()
		if err := pprof.WriteHeapProfile(f); err == nil {
			files["heap"] = heapFile
		} else {
			log.Printf("failed to write heap profile: %v", err)
		}
		f.Close()
	} else {
		log.Printf("failed to create heap profile file: %v", err)
	}

	// Goroutine profile
	goroutineFile := filepath.Join(snapshotDir, "pprof-goroutine-"+timestamp+".pb.gz")
	f, err = os.Create(goroutineFile)
	if err == nil {
		if err := pprof.Lookup("goroutine").WriteTo(f, 1); err == nil {
			files["goroutine"] = goroutineFile
		} else {
			log.Printf("failed to write goroutine profile: %v", err)
		}
		f.Close()
	} else {
		log.Printf("failed to create goroutine profile file: %v", err)
	}

	// Threadcreate profile
	threadFile := filepath.Join(snapshotDir, "pprof-threadcreate-"+timestamp+".pb.gz")
	f, err = os.Create(threadFile)
	if err == nil {
		if err := pprof.Lookup("threadcreate").WriteTo(f, 1); err == nil {
			files["threadcreate"] = threadFile
		} else {
			log.Printf("failed to write threadcreate profile: %v", err)
		}
		f.Close()
	} else {
		log.Printf("failed to create threadcreate profile file: %v", err)
	}

	// Block profile
	blockFile := filepath.Join(snapshotDir, "pprof-block-"+timestamp+".pb.gz")
	f, err = os.Create(blockFile)
	if err == nil {
		runtime.SetBlockProfileRate(1)
		if err := pprof.Lookup("block").WriteTo(f, 1); err == nil {
			files["block"] = blockFile
		} else {
			log.Printf("failed to write block profile: %v", err)
		}
		f.Close()
		runtime.SetBlockProfileRate(0)
	} else {
		log.Printf("failed to create block profile file: %v", err)
	}

	// Mutex profile
	mutexFile := filepath.Join(snapshotDir, "pprof-mutex-"+timestamp+".pb.gz")
	f, err = os.Create(mutexFile)
	if err == nil {
		runtime.SetMutexProfileFraction(1)
		if err := pprof.Lookup("mutex").WriteTo(f, 1); err == nil {
			files["mutex"] = mutexFile
		} else {
			log.Printf("failed to write mutex profile: %v", err)
		}
		f.Close()
		runtime.SetMutexProfileFraction(0)
	} else {
		log.Printf("failed to create mutex profile file: %v", err)
	}

	return files
}

// Diagnose attempts to find a resolution and provide a human friendly error response
// In the future, it may connect to a remote API to provide a better and AI driven response
func Diagnose(ctx context.Context, ex error) *ErrorDiagnosis {
	var traceId string
	if ctx.Value("traceId") == nil {
		traceId = ""
	} else {
		traceId = ctx.Value("traceId").(string)
	}

	msg, cause := toErrorMessage(ex)
	vinfo := version.Get()

	reason, _ := errx.ReasonOf(ex)

	return &ErrorDiagnosis{
		Error:      ex,
		ErrorType:  errorx.GetTypeName(ex),
		Message:    msg,
		Cause:      cause,
		TraceId:    traceId,
		Code:       toErrorCode(ex),
		Commit:     vinfo.Commit,
		Version:    vinfo.Version,
		GoVersion:  runtime.Version(),
		Pid:        os.Getpid(),
		Logfile:    config.Get().Log.Filename,
		Resolution: findResolution(ex),
		WhyFloor:   findWhyFloor(ex),
		Reason:     reason.String(),
	}
}

// VerboseLevel controls error output detail. When >= 1 (-V), CheckErr displays
// the full stacktrace and profiling data. Otherwise a compact human-friendly
// error panel is shown and details are written only to the log file.
var VerboseLevel int

// CheckErr prints diagnosis and exit with error code 1.
//
// Remediation steps come from the error itself — attach them with
// errx.Decorate / errx.WithHints at the boundary that returns err. There is
// deliberately no way to pass them alongside: a second channel renders the same
// hints differently depending on which one happened to be populated.
func CheckErr(ctx context.Context, err error) {
	// Full stacktrace to the log file only, with the reason and hints as
	// structured fields so the log alone carries the remediation steps.
	ev := logx.As().Error()
	// build_version/build_commit are already global fields on every log line (set
	// in the CLI/daemon logger init), so the error line is self-identifying.
	if reason, ok := errx.ReasonOf(err); ok {
		ev = ev.Str("reason", reason.String())
	}
	if hints, ok := resolutionHints(err); ok {
		ev = ev.Strs("hints", hints)
	}
	ev.Msgf("%+v", err)

	resp := Diagnose(ctx, err)

	if VerboseLevel >= 1 {
		checkErrVerbose(resp)
	} else {
		checkErrCompact(resp)
	}

	os.Exit(1)
}

// checkErrCompact prints a concise, human-friendly error panel to stderr.
func checkErrCompact(resp *ErrorDiagnosis) {
	fmt.Fprintf(os.Stderr, "\n  %s%s✗ Error:%s %s\n", Bold, Red, Reset, resp.Message)
	if resp.Cause != "" {
		fmt.Fprintf(os.Stderr, "  %s  Cause:%s %s\n", White, Reset, resp.Cause)
	}
	if resp.WhyFloor != "" {
		fmt.Fprintf(os.Stderr, "  %s Set by:%s %s\n", White, Reset, resp.WhyFloor)
	}

	// Print resolution
	if len(resp.Resolution) > 0 {
		fmt.Fprintf(os.Stderr, "\n  %s%sResolution:%s\n", Bold, Yellow, Reset)
		for i, r := range resp.Resolution {
			fmt.Fprintf(os.Stderr, "    %d. %s\n", i+1, r)
		}
	}

	// Always show the log file path so users can dig deeper
	logfile := resp.Logfile
	if logfile == "" {
		logfile = config.Get().Log.Filename
	}
	if logfile != "" {
		fmt.Fprintf(os.Stderr, "\n  %sSee logs:%s %s\n", Cyan, Reset, logfile)
	}
	fmt.Fprintf(os.Stderr, "  %sBuild:%s %s (commit %s)\n", Gray, Reset, resp.Version, version.Info{Commit: resp.Commit}.ShortCommit(12))
	fmt.Fprintf(os.Stderr, "  %sUse -V for full diagnostics%s\n", Gray, Reset)
}

// checkErrVerbose prints the full legacy error output with stacktrace and profiling.
func checkErrVerbose(resp *ErrorDiagnosis) {
	fmt.Printf("\n%s%s************************************** Error Stacktrace ******************************************%s\n", Bold, Gray, Reset)
	fmt.Printf("\n%+v\n", resp.Error) // Print full error with stack trace

	fmt.Printf("\n%s%s************************************** Error Diagnostics ******************************************%s\n", Bold, Red, Reset)
	fmt.Printf("\t%sError:%s %s\n", Bold+White, Reset, resp.Message)
	if resp.Cause != "" {
		fmt.Printf("\t%sCause:%s %s\n", Bold+White, Reset, resp.Cause)
	}
	fmt.Printf("\t%sError Type:%s %s\n", Bold+White, Reset, resp.ErrorType)
	fmt.Printf("\t%sError Code:%s %d\n", Bold+White, Reset, resp.Code)
	if resp.Reason != "" {
		// Bare, to grep against the log's reason= field.
		fmt.Printf("\t%sReason:%s %s\n", Bold+White, Reset, resp.Reason)
	}
	fmt.Printf("\t%sCommit:%s %s\n", Gray, Reset, resp.Commit)
	fmt.Printf("\t%sPid:%s %d\n", Gray, Reset, resp.Pid)
	fmt.Printf("\t%sTraceId:%s %s\n", Gray, Reset, resp.TraceId)
	fmt.Printf("\t%sVersion:%s %s\n", Gray, Reset, resp.Version)
	fmt.Printf("\t%sGO:%s %s\n", Gray, Reset, resp.GoVersion)
	if resp.Logfile != "" {
		fmt.Printf("\t%sLogfile:%s %s\n", Cyan, Reset, resp.Logfile)
	}
	if resp.ProfilingSnapshots != nil {
		fmt.Printf("\t%sProfiling:%s\n", Cyan, Reset)
		for key, snapshotFile := range resp.ProfilingSnapshots {
			fmt.Printf("\t  %s- %s:%s %s\n", Cyan, key, Reset, snapshotFile)
		}
	}

	fmt.Printf("\n%s%s****************************************** Resolution *********************************************%s\n", Bold, Yellow, Reset)

	for i, r := range resp.Resolution {
		fmt.Printf("\t%s\n", White+fmt.Sprintf("%d. %s", i+1, r)+Reset)
	}
}

// CheckReportErr checks an automa.Report for errors and runs diagnosis if any are found
func CheckReportErr(ctx context.Context, report *automa.Report) {
	if report == nil {
		return
	}

	if report.Error != nil {
		// Diagnose the leaf error, not automa's "completed with N failures"
		// wrapper: the hints and reason are attached where the step failed.
		rootErr := DeepestFailureError(report)
		if rootErr == nil {
			rootErr = report.Error
		}

		CheckErr(ctx, rootErr)
	}
}

// DeepestFailureError descends through nested StepReports to return the
// leaf-level failed step's Error, so that errx hints and reasons attached on a
// deeply nested step (e.g. the preflight superuser check, the weaver
// installation check) are not masked by automa's workflow-level
// "completed with N failures" wrapper. Returns nil when no step failed.
//
// Failure is automa's own IsFailed predicate — a failed status *or* an attached
// error — so a step that reports an error without setting the status still gets
// its hints rendered.
func DeepestFailureError(r *automa.Report) error {
	if r == nil {
		return nil
	}
	for _, sr := range r.StepReports {
		if sr == nil || !sr.IsFailed() {
			continue
		}
		if deeper := DeepestFailureError(sr); deeper != nil {
			return deeper
		}
		if sr.Error != nil {
			return sr.Error
		}
		return errorx.IllegalState.New("step %q failed", sr.Id)
	}
	return nil
}
