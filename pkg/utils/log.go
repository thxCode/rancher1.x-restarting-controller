package utils

import (
	"github.com/Sirupsen/logrus"
	"sync"
)

var (
	mutex        = &sync.Mutex{}
	globalLogger = logrus.New()
)

func NewLogger(logLevel string) *logrus.Logger {
	log := logrus.New()

	// set logger
	switch logLevel {
	case "debug":
		log.Level = logrus.DebugLevel
	case "warn":
		log.Level = logrus.WarnLevel
	case "fatal":
		log.Level = logrus.FatalLevel
	case "panic":
		log.Level = logrus.PanicLevel
	default:
		log.Level = logrus.InfoLevel
	}

	return log
}

func NewGlobalLogger(logLevel string) *logrus.Logger {
	log := NewLogger(logLevel)

	SetGlobalLogger(log)
	return log
}

func SetGlobalLogger(logger *logrus.Logger) {
	mutex.Lock()
	defer mutex.Unlock()

	globalLogger = logger
}

func GetGlobalLogger() *logrus.Logger {
	return globalLogger
}
