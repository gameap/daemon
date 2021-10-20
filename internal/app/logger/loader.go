package logger

import (
	"strings"

	"github.com/gameap/daemon/internal/app/config"
	log "github.com/sirupsen/logrus"
)

func Load(cfg config.Config) error {
	log.SetLevel(log.DebugLevel)

	switch strings.ToUpper(cfg.LogLevel[:1]) {
	case "T":
		log.SetLevel(log.TraceLevel)
	case "V":
		log.SetLevel(log.TraceLevel)
	case "D":
		log.SetLevel(log.DebugLevel)
	case "I":
		log.SetLevel(log.InfoLevel)
	case "W":
		log.SetLevel(log.WarnLevel)
	case "E":
		log.SetLevel(log.ErrorLevel)
	case "F":
		log.SetLevel(log.FatalLevel)
	}

	return nil
}
