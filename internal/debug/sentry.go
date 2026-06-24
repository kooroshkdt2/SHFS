// Package debug provides Sentry error tracking and debug logging.
package debug

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/getsentry/sentry-go"
)

const Version = "0.2.1"

var (
	debugLog  *os.File
	crashLog  *os.File
	debugDir  string
)

// EarlyCrashLog MUST be called as the very first thing in main(),
// before any Sentry init or config loading. On Windows with -H windowsgui
// the console is hidden, so panics are lost. This captures them to a file.
func EarlyCrashLog() {
	// Write crash log next to the executable, or in the working directory.
	// On Windows this is critical since -H windowsgui hides the console.
	var crashPath string
	exe, err := os.Executable()
	if err == nil {
		crashPath = filepath.Join(filepath.Dir(exe), "shfs-crash.log")
	} else {
		crashPath = "shfs-crash.log"
	}

	f, err := os.OpenFile(crashPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Last resort: try temp dir
		crashPath = filepath.Join(os.TempDir(), "shfs-crash.log")
		f, err = os.OpenFile(crashPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return // nothing we can do
		}
	}

	crashLog = f
	timestamp := time.Now().Format(time.RFC3339)
	fmt.Fprintf(f, "=== SHFS v%s Crash Log %s ===\n", Version, timestamp)
	fmt.Fprintf(f, "OS: %s Arch: %s Go: %s\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Fprintf(f, "Executable: %s\n", exe)
	f.Sync()

	// Redirect log output to also write to the crash log.
	// On Windows with -H windowsgui, stderr/stdout are detached,
	// so this is the only way to capture log/panic output.
	log.SetOutput(io.MultiWriter(log.Writer(), f))
}

// InitSentry initializes Sentry error tracking.
func InitSentry() {
	dsn := "https://c0ab26ee01eab325170c4002b86f0971@o4506729220669440.ingest.us.sentry.io/4506729224536064"

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              dsn,
		Release:          "shfs@" + Version,
		Environment:      "production",
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		AttachStacktrace: true,
		ServerName:       getHostname(),
	})
	if err != nil {
		log.Printf("Sentry init: %v", err)
		if crashLog != nil {
			fmt.Fprintf(crashLog, "Sentry init failed: %v\n", err)
		}
	}

	// Log to crash file that Sentry is initialized
	if crashLog != nil {
		fmt.Fprintf(crashLog, "Sentry initialized OK at %s\n", time.Now().Format(time.RFC3339))
		crashLog.Sync()
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

// Debug writes a message to the debug log file and crash log.
func Debug(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	line := time.Now().Format("15:04:05.000") + " " + msg
	log.Print(line)
	if debugLog != nil {
		debugLog.WriteString(line + "\n")
		debugLog.Sync()
	}
	if crashLog != nil {
		crashLog.WriteString(line + "\n")
		crashLog.Sync()
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
	msg := fmt.Sprintf("ERROR: %v", err)
	Debug("%s", msg)
	hub := sentry.CurrentHub().Clone()
	hub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
	})
	hub.CaptureException(err)
}

// CaptureFatal logs a fatal error, flushes Sentry, and exits.
func CaptureFatal(err error) {
	if err == nil {
		return
	}
	msg := fmt.Sprintf("FATAL: %v", err)
	line := time.Now().Format("15:04:05.000") + " " + msg
	log.Print(line)
	if crashLog != nil {
		crashLog.WriteString(line + "\n")
		crashLog.Sync()
	}
	if debugLog != nil {
		debugLog.WriteString(line + "\n")
		debugLog.Sync()
	}
	sentry.CaptureException(err)
	sentry.Flush(2 * time.Second)
}

// CaptureMessage sends a message to Sentry.
func CaptureMessage(msg string) {
	Debug("MSG: %s", msg)
	sentry.CaptureMessage(msg)
}

// RecoverPanic can be used in defer to capture panics.
// DOES NOT re-panic — exits cleanly after flushing logs/Sentry.
func RecoverPanic() {
	if r := recover(); r != nil {
		stack := stackTrace()
		msg := fmt.Sprintf("PANIC: %v\nStack:\n%s", r, stack)
		line := time.Now().Format("15:04:05.000") + " " + msg
		log.Print(line)
		if crashLog != nil {
			crashLog.WriteString(line + "\n")
			crashLog.Sync()
		}
		if debugLog != nil {
			debugLog.WriteString(line + "\n")
			debugLog.Sync()
		}
		// Capture to Sentry if available
		sentry.CurrentHub().Recover(r)
		sentry.Flush(2 * time.Second)
		// Exit cleanly — do NOT re-panic. On Windows with -H windowsgui,
		// a re-panic is silent and the crash log is our only evidence.
		if crashLog != nil {
			crashLog.Close()
		}
		os.Exit(1)
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
	if crashLog != nil {
		fmt.Fprintf(crashLog, "SHFS shutting down normally at %s\n", time.Now().Format(time.RFC3339))
	}
	sentry.Flush(2 * time.Second)
	if debugLog != nil {
		debugLog.Close()
	}
	if crashLog != nil {
		crashLog.Close()
	}
}

// FlushSentry forces an immediate flush of Sentry events.
// Call this after capturing fatal errors before exit.
func FlushSentry() {
	sentry.Flush(2 * time.Second)
}
