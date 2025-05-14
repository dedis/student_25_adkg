package logging

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/rs/zerolog"
)

var (
	logout = zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
		// Format the node ID
		FormatPrepare: func(e map[string]interface{}) error {
			e["nodeID"] = fmt.Sprintf("[%s]", e["nodeID"])
			return nil
		},
		// Change the order in which things appear
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.LevelFieldName,
			"nodeID",
			zerolog.MessageFieldName,
		},
		// Prevent the nodeID from being printed again
		FieldsExclude: []string{"nodeID"},
	}
)

// GetLogger returns a formatted logger using the given logger id
func GetLogger(id int) zerolog.Logger {
	// Disable logging based on the GLOG environment variable
	var logLevel zerolog.Level
	if os.Getenv("GLOG") == "no" {
		logLevel = zerolog.Disabled
	} else {
		logLevel = zerolog.InfoLevel
	}

	return zerolog.New(logout).
		Level(logLevel).
		With().
		Timestamp().
		Str("nodeID", strconv.Itoa(id)).
		Logger()
}
