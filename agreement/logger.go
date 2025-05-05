package agreement

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

// EnvLogLevel is the name of the environment variable to change the logging
// level.
const EnvLogLevel = "GLOG"

const defaultLevel = zerolog.Disabled

func init() {
	lvl := os.Getenv(EnvLogLevel)

	var level zerolog.Level

	switch lvl {
	case "error":
		level = zerolog.ErrorLevel
	case "warn":
		level = zerolog.WarnLevel
	case "info":
		level = zerolog.InfoLevel
	case "debug":
		level = zerolog.DebugLevel
	case "trace":
		level = zerolog.TraceLevel
	case "":
		level = defaultLevel
	case "no":
		level = zerolog.Disabled
	default:
		level = defaultLevel
	}

	// Logger = Logger.Level(level)
	// log.Logger = log.With().Timestamp().Logger().With().Caller().Logger()
	zerolog.SetGlobalLevel(level)
	logger = logger.Level(level)
}

var logout = zerolog.ConsoleWriter{
	Out:        os.Stdout,
	TimeFormat: time.RFC3339,
}

// Logger is a globally available logger instance. By default, it only prints
// error level messages but it can be changed through a environment variable.

var logger = zerolog.New(logout).Level(defaultLevel).
	With().Timestamp().Logger().
	With().Caller().Logger()
