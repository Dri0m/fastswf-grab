package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

const folderName = "files"

type app struct {
	l *logrus.Logger
}

var fileRegex = regexp.MustCompile(`.*?gon\.path=\"([^"]*)\"\;.*?`)

func (a *app) downloadFile(filepath string, url string) ([]byte, error) {
	a.l.Debugf("getting URL '%s'...", url)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	a.l.Debugf("creating file '%s'...", filepath)
	out, err := os.Create(filepath)
	if err != nil {
		return nil, err
	}
	defer out.Close()

	result := new(bytes.Buffer)
	mw := io.MultiWriter(result, out)

	a.l.Debugf("writing data to '%s'...", filepath)
	_, err = io.Copy(mw, resp.Body)
	if err != nil {
		return nil, err
	}

	return result.Bytes(), nil
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
			// get random URL
			u, err := a.getRandomURL()
			if err != nil {
				a.l.WithError(err).Errorln("get random url error")
				continue
			}
			a.l.Debugf("got url: %s", u)

			url, err := url.Parse(u)
			if err != nil {
				a.l.WithError(err).WithField("url", u).Errorln("page url parse error")
				continue
			}

			// download random URL
			filenameHTML := fmt.Sprintf("%s/%s.html", folderName, url.Path)
			fileExists := true
			if _, err := os.Stat(filenameHTML); os.IsNotExist(err) {
				fileExists = false
			}
			if fileExists {
				a.l.WithField("filename", filenameHTML).Infof("file exists, skipping")
				continue
			}

			data, err := a.downloadFile(filenameHTML, u)
			if err != nil {
				a.l.WithError(err).Errorln("html download error")
				os.Remove(filenameHTML)
				continue
			}

			// find file URL
			matches := fileRegex.FindSubmatch(data)
			if len(matches) != 2 {
				a.l.Errorln("regex matches != 2")
				continue
			}

			fileURL := string(matches[1])
			fileURL = strings.Replace(fileURL, `\u0026`, "&", -1)
			url, err = url.Parse(fileURL)
			if err != nil {
				a.l.WithError(err).WithField("url", fileURL).Errorln("file url parse error")
				continue
			}

			// download file
			splitPath := strings.Split(url.Path, "/")
			filename := fmt.Sprintf("%s/%s", folderName, splitPath[len(splitPath)-1])
			_, err = a.downloadFile(filename, fileURL)
			if err != nil {
				a.l.WithError(err).Errorln("file download error")
				continue
			}
		}
	}
}

func (a *app) memoryPrinter(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer a.l.Infoln("memory printer stopped")

	bucket, ticker := newBucketLimiter(60*time.Second, 1)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			a.l.Infoln("context cancelled, stopping getter")
			return
		case <-bucket:
			a.l.Debugf("memory stats: %s", getMemUsageString())
		}
	}
}

func main() {
	if _, err := os.Stat(folderName); os.IsNotExist(err) {
		os.Mkdir("folderName", os.ModeDir)
	}
	a := app{
		l: initLogger(),
	}
	a.l.Infoln("hello")
	ctx, cancelFunc := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	wg.Add(1)
	go a.getter(ctx, wg)
	wg.Add(1)
	go a.memoryPrinter(ctx, wg)

	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	<-term
	a.l.Infoln("signal received, waiting for all goroutines to finish...")
	cancelFunc()
	wg.Wait()
	a.l.Infoln("goodbye")
}
