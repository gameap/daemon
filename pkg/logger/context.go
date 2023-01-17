package logger

import (
	"context"
	"io"

	log "github.com/sirupsen/logrus"
)

type key int

const (
	loggerKey key = iota
)

func WithLogger(ctx context.Context, logger log.FieldLogger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func Logger(ctx context.Context) log.FieldLogger {
	logger, exists := ctx.Value(loggerKey).(log.FieldLogger)
	if exists {
		return logger
	}

	nilLogger := log.New()
	nilLogger.Out = io.Discard

	return nilLogger
}

func WithField(ctx context.Context, key string, value interface{}) log.FieldLogger {
	return Logger(ctx).WithField(key, value)
}

func WithError(ctx context.Context, err error) log.FieldLogger {
	return Logger(ctx).WithError(err)
}

func WithFields(ctx context.Context, fields log.Fields) log.FieldLogger {
	return Logger(ctx).WithFields(fields)
}

func Debug(ctx context.Context, args ...interface{}) {
	Logger(ctx).Debug(args...)
}

//nolint
func Debugf(ctx context.Context, format string, args ...interface{}) {
	Logger(ctx).Debugf(format, args...)
}

//nolint
func Trace(ctx context.Context, args ...interface{}) {
	Logger(ctx).Debug(args...)
}

func Tracef(ctx context.Context, format string, args ...interface{}) {
	Logger(ctx).Debugf(format, args...)
}

func Info(ctx context.Context, args ...interface{}) {
	Logger(ctx).Info(args...)
}

func Infof(ctx context.Context, format string, args ...interface{}) {
	Logger(ctx).Infof(format, args...)
}

//nolint
func Print(ctx context.Context, args ...interface{}) {
	Logger(ctx).Print(args...)
}

func Warn(ctx context.Context, args ...interface{}) {
	Logger(ctx).Warn(args...)
}

func Error(ctx context.Context, args ...interface{}) {
	Logger(ctx).Error(args...)
}
