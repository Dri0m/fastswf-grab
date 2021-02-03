package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

type app struct {
	l *logrus.Logger
}

func (a *app) downloadFile(filepath string, url string) error {

	a.l.Debugf("getting URL '%s'...", url)
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	a.l.Debugf("creating file '%s'...", filepath)
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	a.l.Debugf("writing data to '%s'...", filepath)
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
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

	bucket, ticker := newBucketLimiter(11*time.Second, 1)
	defer ticker.Stop()

	for {
		a.l.Debugln("waiting...")
		select {
		case <-ctx.Done():
			a.l.Infoln("context cancelled, stopping getter")
			return
		case <-bucket:
			u, err := a.getRandomURL()
			if err != nil {
				a.l.WithError(err).Errorln("get random url error")
			} else {
				a.l.Debugf("got url: %s", u)
			}

			url, err := url.Parse(u)
			if err != nil {
				a.l.WithError(err).Errorln("url parse error")
			}

			filename := fmt.Sprintf("%s.html", url.Path)
			err = a.downloadFile(filename, u)
			if err != nil {
				a.l.WithError(err).Errorln("filed download error")
				os.Remove(filename)
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
