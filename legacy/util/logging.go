package util

import (
	"github.com/rs/zerolog"
	"os"
	"time"
	"strings"
)

var (
	Logger zerolog.Logger
)

func LogInit(inlevel string) {
	var level zerolog.Level
	switch strings.ToLower(inlevel) {
	case "debug":
		level = zerolog.DebugLevel
	case "trace":
		level = zerolog.TraceLevel
	case "warn":
		level = zerolog.WarnLevel
	default:
		level = zerolog.InfoLevel
	}
	Logger = zerolog.New(
		zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339},
	).Level(level).With().Timestamp().Caller().Logger()

	Logger.Info().Msgf("logging initialized at level %v", level)
}