package logx

import (
	"context"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
	"io"
	"log"
	"os"
	"path"
	"time"
)

// global logger instances
var logger zerolog.Logger
var nolog = zerolog.Nop()

var startTime time.Time
var pid = os.Getpid()

// LoggingConfig holds the configuration for logging.
type LoggingConfig struct {
	// Level is the log level to use (e.g., "Info", "Debug").
	Level string
	// ConsoleLogging enables logging to the console.
	ConsoleLogging bool
	// FileLogging enables logging to a file.
	FileLogging bool
	// Directory specifies the directory for log files (used if FileLogging is enabled).
	Directory string
	// Filename is the name of the log file.
	Filename string
	// MaxSize is the maximum size (in MB) of a log file before it is rolled.
	MaxSize int
	// MaxBackups is the maximum number of rolled log files to keep.
	MaxBackups int
	// MaxAge is the maximum age (in days) to keep a log file.
	MaxAge int
	// Compress enables compression of rolled log files.
	Compress bool
}

func init() {
	StartTimer()
	err := WithConfig(&LoggingConfig{
		Level:          "debug",
		ConsoleLogging: true,
	}, nil)
	if err != nil {
		log.Fatalf("failed to initialize logging: %v", err)
	}
}

func WithConfig(cfg *LoggingConfig, fields map[string]string) error {
	l, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		return err
	}
	zerolog.SetGlobalLevel(l)

	console := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	var writers []io.Writer
	if cfg.FileLogging {
		logFile, err := newRollingFile(cfg)
		if err != nil {
			return err
		}

		fileWriter := zerolog.New(logFile).With().Timestamp().Logger()
		writers = append(writers, console, fileWriter)
	} else {
		writers = append(writers, console)
	}

	mw := zerolog.MultiLevelWriter(writers...)
	c := zerolog.New(mw).
		With().
		Timestamp().
		Int("pid", pid)

	for k, v := range fields {
		c = c.Str(k, v)
	}

	logger = c.Logger()
	return nil
}

func As() *zerolog.Logger {
	return &logger
}

func WithContext(ctx context.Context, fields map[string]string) *zerolog.Logger {
	c := zerolog.New(logger).
		With().
		Ctx(ctx)

	if fields == nil {
		fields = map[string]string{}
	}

	traceId := ctx.Value("traceId")
	if traceId != nil {
		fields["traceId"] = traceId.(string)
	}

	for k, v := range fields {
		c = c.Str(k, v)
	}

	logger = c.Logger()
	return &logger
}

func Nop() *zerolog.Logger {
	return &nolog
}

func StartTimer() {
	startTime = time.Now()
}

func ExecutionTime() string {
	return time.Since(startTime).Round(time.Second).String()
}

func GetPid() int {
	return pid
}

func newRollingFile(cfg *LoggingConfig) (io.Writer, error) {
	return &lumberjack.Logger{
		Filename:   path.Join(cfg.Directory, cfg.Filename),
		MaxBackups: cfg.MaxBackups, // files
		MaxSize:    cfg.MaxSize,    // megabytes
		MaxAge:     cfg.MaxAge,     // days
		Compress:   cfg.Compress,
	}, nil
}
