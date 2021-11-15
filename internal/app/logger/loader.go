package logger

import (
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	log "github.com/sirupsen/logrus"
)

func Load(cfg config.Config) error {
	log.SetLevel(defineLogLevel(cfg))

	return nil
}

func NewLogger(cfg config.Config) *log.Logger {
	logger := log.New()
	logger.SetLevel(defineLogLevel(cfg))

	logger.SetFormatter(&log.TextFormatter{})

	return logger
}

func defineLogLevel(cfg config.Config) log.Level {
	level := log.DebugLevel

	switch strings.ToUpper(cfg.LogLevel[:1]) {
	case "T", "V":
		level = log.TraceLevel
	case "D":
		level = log.DebugLevel
	case "I":
		level = log.InfoLevel
	case "W":
		level = log.WarnLevel
	case "E":
		level = log.ErrorLevel
	case "F":
		level = log.FatalLevel
	}

	return level
}
