// Package debug provides Sentry error tracking and debug logging.
package debug

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/getsentry/sentry-go"
)

var (
	debugLog *os.File
	debugDir string
)

// InitSentry initializes Sentry error tracking.
func InitSentry() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              "https://c0ab26ee01eab325170c4002b86f0971@o4506729220669440.ingest.us.sentry.io/4506729224536064",
		Release:          "shfs@0.2.0",
		Environment:      "production",
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		AttachStacktrace: true,
		ServerName:       getHostname(),
	})
	if err != nil {
		log.Printf("Sentry init: %v", err)
	}
}

// InitDebugLog creates a debug log file in the config directory.
func InitDebugLog(configDir string) {
	debugDir = configDir
	os.MkdirAll(configDir, 0755)
	path := filepath.Join(configDir, "debug.log")

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Cannot create debug log at %s: %v", path, err)
		return
	}
	debugLog = f
	Debug("=== SHFS Debug Log Started %s ===", time.Now().Format(time.RFC3339))
	Debug("OS: %s Arch: %s Go: %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
}

// Debug writes a message to the debug log file.
func Debug(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	line := time.Now().Format("15:04:05.000") + " " + msg
	log.Print(line)
	if debugLog != nil {
		debugLog.WriteString(line + "\n")
		debugLog.Sync()
	}
}

// DebugConfig logs config values for troubleshooting.
func DebugConfig(cfg interface{}) {
	Debug("Config loaded: %+v", cfg)
}

// DebugPath logs path resolution steps.
func DebugPath(step, path string) {
	Debug("Path[%s]: %q", step, path)
}

// DebugURL logs URL resolution.
func DebugURL(raw, decoded, result string) {
	Debug("URL: raw=%q decoded=%q result=%s", raw, decoded, result)
}

// CaptureError sends an error to Sentry and logs it.
func CaptureError(err error) {
	if err == nil {
		return
	}
	Debug("ERROR: %v", err)
	sentry.CaptureException(err)
}

// CaptureMessage sends a message to Sentry.
func CaptureMessage(msg string) {
	Debug("MSG: %s", msg)
	sentry.CaptureMessage(msg)
}

// RecoverPanic can be used in defer to capture panics.
func RecoverPanic() {
	if r := recover(); r != nil {
		Debug("PANIC: %v\nStack: %s", r, stackTrace())
		sentry.CurrentHub().Recover(r)
		sentry.Flush(2 * time.Second)
		panic(r) // re-panic after capture
	}
}

func stackTrace() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

func getHostname() string {
	h, _ := os.Hostname()
	return h
}

// Close flushes Sentry and closes the debug log.
func Close() {
	sentry.Flush(2 * time.Second)
	if debugLog != nil {
		debugLog.Close()
	}
}
