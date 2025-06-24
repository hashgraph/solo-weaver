package logx

import (
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"testing"
)

func TestInitialize_FileLogging(t *testing.T) {
	tempDir := t.TempDir()
	logFile := "test.log"

	err := Initialize(&LoggingConfig{
		Level:       "info",
		FileLogging: true,
		Directory:   tempDir,
		Filename:    logFile,
		MaxSize:     1,
		MaxBackups:  1,
		MaxAge:      1,
		Compress:    false,
	})
	assert.NoError(t, err)

	logger := As()
	assert.NotNil(t, logger)
	logger.Info().Msg("Test info message")

	// Verify log file exists
	logFilePath := filepath.Join(tempDir, logFile)
	_, err = os.Stat(logFilePath)
	assert.NoError(t, err)
}

func TestInitialize_InvalidLogLevel(t *testing.T) {
	err := Initialize(&LoggingConfig{
		Level:          "invalid",
		ConsoleLogging: true,
	})
	assert.Error(t, err)
}

func TestExecutionTime(t *testing.T) {
	StartTimer()
	assert.NotEmpty(t, ExecutionTime())
}

func TestGetPid(t *testing.T) {
	pid := GetPid()
	assert.Equal(t, os.Getpid(), pid)
}
