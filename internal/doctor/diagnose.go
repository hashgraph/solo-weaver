package doctor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/automa-saga/automa"
	"github.com/automa-saga/logx"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-weaver/internal/config"
	"golang.hedera.com/solo-weaver/internal/core"
	"golang.hedera.com/solo-weaver/internal/version"
)

type ErrorDiagnosis struct {
	Error              error             `yaml:"error" json:"error"`
	Message            string            `yaml:"message" json:"message"`
	Cause              string            `yaml:"cause" json:"cause"`
	ErrorType          string            `yaml:"errorType" json:"errorType"`
	TraceId            string            `yaml:"traceId" json:"traceId"`
	Commit             string            `yaml:"commit" json:"commit"`
	Version            string            `yaml:"version" json:"version"`
	Pid                int               `yaml:"pid" json:"pid"`
	StackTrace         []string          `yaml:"stackTrace" json:"stackTrace"`
	Code               int               `yaml:"code" json:"code"`
	Logfile            string            `yaml:"log" json:"log"`
	ProfilingSnapshots map[string]string `yaml:"ProfilingSnapshots" json:"profilingSnapshots"`
	Resolution         []string          `yaml:"steps" json:"steps"`
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

	return e.Message(), fmt.Sprintf("%s", e.Cause())
}

func findResolution(err error) []string {
	switch {
	case errorx.IsOfType(err, errorx.IllegalArgument):
		if arg, ok := errorx.ExtractProperty(err, errorx.PropertyPayload()); ok {
			return []string{fmt.Sprintf("Ensure %q is provided.", arg.(string))}
		}
		return []string{fmt.Sprintf("Ensure all required arguments are provided.")}
	case errorx.IsOfType(err, errorx.IllegalFormat):
		return []string{"Ensure provided data is in correct format."}
	case errorx.IsOfType(err, config.NotFoundError):
		if arg, ok := errorx.ExtractProperty(err, errorx.PropertyPayload()); ok {
			return []string{fmt.Sprintf("Ensure configuration file %q exists, is correctly formatted and accessible", arg.(string))}
		}
		return []string{"Ensure configuration file exists and is accessible."}
	default:
		return []string{"Check error message for details or contact support"}
	}
}

func takeProfilingSnapshots(ex error) map[string]string {
	timestamp := time.Now().Format("20060102-150405")

	snapshotDir := path.Join(core.Paths().DiagnosticsDir, timestamp)
	if err := os.MkdirAll(snapshotDir, core.DefaultDirOrExecPerm); err != nil {
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
	return &ErrorDiagnosis{
		Error:              ex,
		ErrorType:          errorx.GetTypeName(ex),
		Message:            msg,
		Cause:              cause,
		TraceId:            traceId,
		Code:               toErrorCode(ex),
		Commit:             version.Commit(),
		Version:            version.Number(),
		Pid:                os.Getpid(),
		Logfile:            config.Get().Log.Filename,
		ProfilingSnapshots: takeProfilingSnapshots(ex),
		Resolution:         findResolution(ex),
	}
}

// CheckErr prints diagnosis and exit with error code 1
// Optional instructions can be provided to give additional context to the user
func CheckErr(ctx context.Context, err error, instructions ...string) {

	logx.As().Error().Err(err).Msg("error occurred")
	fmt.Printf("%+v\n", err)
	resp := Diagnose(ctx, err)

	fmt.Printf("\n%s%s************************************** Error Diagnostics ******************************************%s\n", Bold, Red, Reset)
	fmt.Printf("%s*%s\t%sError:%s %s\n", Red, Reset, Bold+White, Reset, resp.Message)
	if resp.Cause != "" {
		fmt.Printf("%s*%s\t%sCause:%s %s\n", Red, Reset, Bold+White, Reset, resp.Cause)
	}
	fmt.Printf("%s*%s\t%sError Type:%s %s\n", Red, Reset, Bold+White, Reset, resp.ErrorType)
	fmt.Printf("%s*%s\t%sError Code:%s %d\n", Red, Reset, Bold+White, Reset, resp.Code)
	fmt.Printf("%s*%s\t%sCommit:%s %s\n", Red, Reset, Gray, Reset, resp.Commit)
	fmt.Printf("%s*%s\t%sPid:%s %d\n", Red, Reset, Gray, Reset, resp.Pid)
	fmt.Printf("%s*%s\t%sTraceId:%s %s\n", Red, Reset, Gray, Reset, resp.TraceId)
	fmt.Printf("%s*%s\t%sVersion:%s %s\n", Red, Reset, Gray, Reset, resp.Version)
	if resp.Logfile != "" {
		fmt.Printf("%s*%s\t%sLogfile:%s %s\n", Red, Reset, Cyan, Reset, resp.Logfile)
	}
	if resp.ProfilingSnapshots != nil {
		fmt.Printf("%s*%s\t%sProfiling:%s\n", Red, Reset, Cyan, Reset)
		for key, snapshotFile := range resp.ProfilingSnapshots {
			fmt.Printf("%s*%s\t  %s- %s:%s %s\n", Red, Reset, Cyan, key, Reset, snapshotFile)
		}
	}
	fmt.Printf("%s%s***************************************************************************************************%s\n", Bold, Red, Reset)
	fmt.Printf("\n%s%s****************************************** Resolution *********************************************%s\n", Bold, Yellow, Reset)

	// Print custom instructions first if provided
	if len(instructions) > 0 && instructions[0] != "" {
		for _, line := range strings.Split(instructions[0], "\n") {
			if line == "" {
				fmt.Printf("%s*%s\n", Yellow, Reset)
			} else {
				fmt.Printf("%s*%s\t%s\n", Yellow, Reset, Bold+White+line+Reset)
			}
		}
		if len(resp.Resolution) > 0 {
			fmt.Printf("%s*%s\n", Yellow, Reset)
		}
	}

	// Print default resolution steps
	for _, r := range resp.Resolution {
		fmt.Printf("%s*%s\t%s\n", Yellow, Reset, White+r+Reset)
	}

	fmt.Printf("%s%s***************************************************************************************************%s\n", Bold, Yellow, Reset)

	os.Exit(1)
}

// GetInstructionsFromReport recursively searches for instructions in report metadata.
// Returns the first non-empty instructions found in the report tree, or an empty string if none exist.
func GetInstructionsFromReport(report *automa.Report) string {
	if report == nil {
		return ""
	}

	// Check if this report has instructions
	if instructions, ok := report.Metadata["instructions"]; ok {
		return instructions
	}

	// Recursively check nested step reports
	for _, stepReport := range report.StepReports {
		if instructions := GetInstructionsFromReport(stepReport); instructions != "" {
			return instructions
		}
	}

	return ""
}
