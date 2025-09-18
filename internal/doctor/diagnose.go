package doctor

import (
	"context"
	"fmt"
	"github.com/joomcode/errorx"
	"golang.hedera.com/solo-provisioner/internal/config"
	"golang.hedera.com/solo-provisioner/internal/core"
	"golang.hedera.com/solo-provisioner/internal/version"
	"golang.hedera.com/solo-provisioner/pkg/logx"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"
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
			return []string{fmt.Sprintf("Ensure configuration file %q exists, correctly formatted and is accessible", arg.(string))}
		}
		return []string{"Ensure configuration file exists and is accessible."}
	default:
		return []string{"Check the error message for details or contact support"}
	}
}

func takeProfilingSnapshots(ex error) map[string]string {
	timestamp := time.Now().Format("20060102-150405")

	snapshotDir := path.Join(core.DiagnosticsDir, timestamp)
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
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
func CheckErr(ctx context.Context, err error) {
	logx.As().Error().Err(err).Msg("error occurred")
	fmt.Printf("%+v\n", err)
	resp := Diagnose(ctx, err)
	fmt.Println("\n************************************** Error Diagnostics ******************************************")
	fmt.Printf("*\tError: %s\n", resp.Message)
	fmt.Printf("*\tCause: %s\n", resp.Cause)
	fmt.Printf("*\tError Type: %s\n", resp.ErrorType)
	fmt.Printf("*\tError Code: %d\n", resp.Code)
	fmt.Printf("*\tCommit: %s\n", resp.Commit)
	fmt.Printf("*\tVersion: %s\n", resp.Version)
	fmt.Printf("*\tPid: %d\n", resp.Pid)
	if resp.Logfile != "" {
		fmt.Printf("*\tLogfile: %s\n", resp.Logfile)
	}
	if resp.ProfilingSnapshots != nil {
		fmt.Println("*\tProfiling:")
		for key, snapshotFile := range resp.ProfilingSnapshots {
			fmt.Printf("*\t  - %s: %s\n", key, snapshotFile)
		}
	}
	fmt.Printf("*\tTraceId: %s\n", resp.TraceId)
	fmt.Println("****************************************** Resolution *********************************************")
	for _, r := range resp.Resolution {
		fmt.Printf("*\t%s\n", r)
	}
	fmt.Println("***************************************************************************************************")
	os.Exit(1)
}
