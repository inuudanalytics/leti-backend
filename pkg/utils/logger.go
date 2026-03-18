package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/sirupsen/logrus"
)

var Logger = logrus.New()

func InitLogger() {
	env := os.Getenv("APP_ENV")

	Logger.SetReportCaller(true)

	Logger.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05Z07:00",
		PrettyPrint:     env != "production",
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			filename := filepath.Base(f.File)
			return "", filename + ":" + strconv.Itoa(f.Line)
		},
	})

	level := os.Getenv("LOG_LEVEL")
	switch level {
	case "debug":
		Logger.SetLevel(logrus.DebugLevel)
	case "warn":
		Logger.SetLevel(logrus.WarnLevel)
	case "error":
		Logger.SetLevel(logrus.ErrorLevel)
	default:
		Logger.SetLevel(logrus.InfoLevel)
	}

	Logger.Out = os.Stdout
}
