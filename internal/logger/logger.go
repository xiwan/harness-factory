// Package logger provides structured stderr logging with level control.
// Set HARNESS_LOG_LEVEL=debug|info|error (default: info).
package logger

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	LevelDebug = iota
	LevelInfo
	LevelError
)

var level = LevelInfo

func Init() {
	setFromEnv()
}

func SetLevel(lvl string) {
	switch strings.ToLower(lvl) {
	case "debug":
		level = LevelDebug
	case "error":
		level = LevelError
	default:
		level = LevelInfo
	}
}

func setFromEnv() {
	SetLevel(os.Getenv("HARNESS_LOG_LEVEL"))
}

func log(lvl int, tag, msg string) {
	if lvl < level {
		return
	}
	labels := []string{"DBG", "INF", "ERR"}
	ts := time.Now().Format("15:04:05.000")
	fmt.Fprintf(os.Stderr, "%s [%s] %s: %s\n", ts, labels[lvl], tag, msg)
}

func Debug(tag, msg string)              { log(LevelDebug, tag, msg) }
func Info(tag, msg string)               { log(LevelInfo, tag, msg) }
func Error(tag, msg string)              { log(LevelError, tag, msg) }
func Debugf(tag, f string, a ...any)     { log(LevelDebug, tag, fmt.Sprintf(f, a...)) }
func Infof(tag, f string, a ...any)      { log(LevelInfo, tag, fmt.Sprintf(f, a...)) }
func Errorf(tag, f string, a ...any)     { log(LevelError, tag, fmt.Sprintf(f, a...)) }
