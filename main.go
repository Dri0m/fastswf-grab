package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"
)

type app struct {
	l *logrus.Logger
}

func (a *app) downloadFile(filepath string, url string) error {

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check server response
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

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

func (a *app) getRandomURL() (string, error) {
	const url = "http://www.fastswf.com/random"
	a.l.Debugf("getting '%s'...", url)
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	result := resp.Request.Response.Header.Get("Location")

	if len(result) == 0 {
		return "", fmt.Errorf("no location header")
	}

	return result, nil
}

func (a *app) getter(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer a.l.Infoln("getter stopped")

	bucket, ticker := newBucketLimiter(11000*time.Millisecond, 1)
	defer ticker.Stop()

	for {
		a.l.Debugln("waiting...")
		select {
		case <-ctx.Done():
			a.l.Infoln("context cancelled, stopping getter")
			return
		case <-bucket:
			url, err := a.getRandomURL()
			if err != nil {
				a.l.WithError(err).Errorln("get random url error")
			} else {
				a.l.Debugf("got url: %s", url)
			}
		}
	}
}

func main() {
	a := app{
		l: initLogger(),
	}
	a.l.Infoln("hello")
	ctx, cancelFunc := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}
	wg.Add(1)

	go a.getter(ctx, wg)

	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-term
	a.l.Infoln("signal received, waiting for all goroutines to finish...")
	cancelFunc()
	wg.Wait()
	a.l.Infoln("goodbye")
}
