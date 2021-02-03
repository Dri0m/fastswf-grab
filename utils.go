package main

import (
	"io"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

func initLogger() *logrus.Logger {
	mw := io.MultiWriter(os.Stdout, &lumberjack.Logger{
		Filename:   "log.log",
		MaxSize:    500, // megabytes
		MaxAge:     0,   //days
		MaxBackups: 0,
		Compress:   true,
	})
	l := logrus.New()
	l.SetFormatter(&logrus.TextFormatter{
		DisableColors:   true,
		FullTimestamp:   true,
		TimestampFormat: time.RFC3339Nano,
	})
	l.SetOutput(mw)
	l.SetLevel(logrus.TraceLevel)
	l.SetReportCaller(true)
	return l
}

func newBucketLimiter(d time.Duration, capacity int) (chan bool, *time.Ticker) {
	bucket := make(chan bool, capacity)
	ticker := time.NewTicker(d)
	go func() {
		for {
			select {
			case <-ticker.C:
				bucket <- true
			}
		}
	}()
	return bucket, ticker
}
