package logger

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gameap/daemon/internal/app/config"
	"github.com/pkg/errors"
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

	if cfg.OutputLog == "" {
		return logger
	}

	oldLogFile, err := os.Stat(cfg.OutputLog)
	if err == nil {
		name := strings.TrimSuffix(oldLogFile.Name(), filepath.Ext(oldLogFile.Name()))
		_ = os.Rename(
			cfg.OutputLog,
			fmt.Sprintf(
				"%s/%s_%s%s",
				path.Dir(cfg.OutputLog),
				name,
				time.Now().Format("20060102_1504"),
				filepath.Ext(oldLogFile.Name()),
			),
		)
	}

	if _, err = os.Stat(filepath.Dir(cfg.OutputLog)); errors.Is(err, os.ErrNotExist) {
		err = os.MkdirAll(filepath.Dir(cfg.OutputLog), 0640)
	}

	if err == nil {
		outputLog, err := os.OpenFile(cfg.OutputLog, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			logger.Error(err)
			return logger
		}

		logger.SetOutput(outputLog)
	}

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
