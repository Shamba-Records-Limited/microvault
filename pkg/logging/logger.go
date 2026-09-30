package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// LevelEnv names the environment variable Setup reads the log level from.
const LevelEnv = "LOG_LEVEL"

// New returns a JSON logger writing to w at level, wrapped in Handler.
func New(w io.Writer, level slog.Leveler) *slog.Logger {
	return slog.New(NewHandler(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})))
}

// ParseLevel parses debug, info, warn or error, case-insensitively. An empty
// string is info.
func ParseLevel(s string) (slog.Level, error) {
	var level slog.Level
	if strings.TrimSpace(s) == "" {
		return slog.LevelInfo, nil
	}
	err := level.UnmarshalText([]byte(strings.TrimSpace(s)))
	return level, err
}

// Setup builds a stdout logger at the level named by LOG_LEVEL, installs it as
// the slog default and returns it. An unparseable level falls back to info and
// is reported through the new logger.
func Setup() *slog.Logger {
	raw := os.Getenv(LevelEnv)
	level, err := ParseLevel(raw)
	if err != nil {
		level = slog.LevelInfo
	}
	logger := New(os.Stdout, level)
	slog.SetDefault(logger)
	if err != nil {
		logger.Warn("invalid log level, using info", slog.String("env", LevelEnv), slog.String("value", raw))
	}
	return logger
}
