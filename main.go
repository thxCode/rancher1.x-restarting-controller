package main

import (
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/prometheus/common/version"
	"github.com/thxcode/rancher1.x-restarting-controller/pkg/controller"
	"github.com/thxcode/rancher1.x-restarting-controller/pkg/utils"
	"github.com/urfave/cli"
)

var (
	cattleURL       string
	cattleAccessKey string
	cattleSecretKey string
	logLevel        string
	watchLabel      string
)

func main() {
	app := cli.NewApp()
	app.Name = "rancher-restarting-controller"
	app.Version = version.Print("rancher-restarting-controller")
	app.Usage = "A controller of Rancher1.x to manage the restart action of container."
	app.Action = appAction

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "cattle_url",
			Usage:       "The URL of Rancher Server API, e.g. http://127.0.0.1:8080",
			EnvVar:      "CATTLE_URL",
			Destination: &cattleURL,
		},
		cli.StringFlag{
			Name:        "cattle_access_key",
			Usage:       "The access key for Rancher API",
			EnvVar:      "CATTLE_ACCESS_KEY",
			Destination: &cattleAccessKey,
		},
		cli.StringFlag{
			Name:        "cattle_secret_key",
			Usage:       "The secret key for Rancher API",
			EnvVar:      "CATTLE_SECRET_KEY",
			Destination: &cattleSecretKey,
		},
		cli.StringFlag{
			Name:        "log_level",
			Usage:       "Set the logging level",
			EnvVar:      "LOG_LEVEL",
			Value:       "info",
			Destination: &logLevel,
		},
		cli.StringFlag{
			Name:        "watch_label",
			Usage:       "Set the watching label name",
			EnvVar:      "WATCH_LABEL",
			Value:       "io.rancher.controller.stop_when_restarting",
			Destination: &watchLabel,
		},
	}

	app.Run(os.Args)
}

func appAction(c *cli.Context) {
	glog := utils.NewGlobalLogger(logLevel)
	stoppingChan := make(chan os.Signal, 10)
	doneChan := make(chan interface{}, 1)

	/* params deal */
	if cattleURL == "" {
		panic(errors.New("cattle_url must be set and non-empty"))
	} else {
		cattleURL = strings.Replace(cattleURL, "/v1", "/v2-beta", -1)

		if !strings.Contains(cattleURL, "/v2-beta") {
			cattleURL += "/v2-beta"
		}
	}

	/* watch */
	go func() {
		defer func() {
			if err := recover(); err != nil {
				glog.Errorln(err)

				os.Exit(0)
			}
			close(doneChan)
		}()

		if err := controller.Start(cattleURL, cattleAccessKey, cattleSecretKey, watchLabel); err != nil && err != http.ErrServerClosed {
			panic(err)
		}

	}()

	glog.Infoln("Starting rancher-restarting-controller")
	signal.Notify(stoppingChan, os.Kill, os.Interrupt)
	select {
	case s := <-stoppingChan:
		glog.Infof("Stopping rancher-restarting-controller with signal [%s]", s.String())
		controller.Stop()
	case <-doneChan:
		glog.Warnln("Stopped rancher-restarting-controller")
	}
}
