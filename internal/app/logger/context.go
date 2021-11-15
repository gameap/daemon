package logger

import (
	"context"
	"io/ioutil"

	log "github.com/sirupsen/logrus"
)

const loggerKey = "logger"

func WithLogger(ctx context.Context, logger log.FieldLogger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func Logger(ctx context.Context) log.FieldLogger {
	logger, exists := ctx.Value(loggerKey).(log.FieldLogger)
	if exists {
		return logger
	}

	nilLogger := log.New()
	nilLogger.Out = ioutil.Discard

	return nilLogger
}

func LoggerWithField(ctx context.Context, key string, value interface{}) log.FieldLogger {
	return Logger(ctx).WithField(key, value)
}

func LoggerWithFields(ctx context.Context, fields log.Fields) log.FieldLogger {
	return Logger(ctx).WithFields(fields)
}

func Debug(ctx context.Context, args ...interface{}) {
	Logger(ctx).Debug(args...)
}

func Debugf(ctx context.Context, format string, args ...interface{}) {
	Logger(ctx).Debugf(format, args...)
}


func Info(ctx context.Context, args ...interface{}) {
	Logger(ctx).Info(args...)
}

func Print(ctx context.Context, args ...interface{}) {
	Logger(ctx).Print(args...)
}

func Warn(ctx context.Context, args ...interface{}) {
	Logger(ctx).Warn(args...)
}

func Error(ctx context.Context, args ...interface{}) {
	Logger(ctx).Error(args...)
}
