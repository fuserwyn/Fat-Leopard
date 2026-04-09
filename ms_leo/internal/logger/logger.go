package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

type Logger interface {
	Info(args ...interface{})
	Infof(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
	Debug(args ...interface{})
	Debugf(format string, args ...interface{})
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
}

type logger struct {
	*logrus.Logger
}

func New(level string) Logger {
	log := logrus.New()
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	switch level {
	case "debug":
		log.SetLevel(logrus.DebugLevel)
	case "info":
		log.SetLevel(logrus.InfoLevel)
	case "warn":
		log.SetLevel(logrus.WarnLevel)
	case "error":
		log.SetLevel(logrus.ErrorLevel)
	default:
		log.SetLevel(logrus.InfoLevel)
	}

	return &logger{log}
}

func (l *logger) Info(args ...interface{}) {
	l.Logger.Info(args...)
}

func (l *logger) Infof(format string, args ...interface{}) {
	l.Logger.Infof(format, args...)
}

func (l *logger) Error(args ...interface{}) {
	l.Logger.Error(args...)
}

func (l *logger) Errorf(format string, args ...interface{}) {
	l.Logger.Errorf(format, args...)
}

func (l *logger) Warn(args ...interface{}) {
	l.Logger.Warn(args...)
}

func (l *logger) Warnf(format string, args ...interface{}) {
	l.Logger.Warnf(format, args...)
}

func (l *logger) Debug(args ...interface{}) {
	l.Logger.Debug(args...)
}

func (l *logger) Debugf(format string, args ...interface{}) {
	l.Logger.Debugf(format, args...)
}

func (l *logger) Fatal(args ...interface{}) {
	l.Logger.Fatal(args...)
}

func (l *logger) Fatalf(format string, args ...interface{}) {
	l.Logger.Fatalf(format, args...)
} 