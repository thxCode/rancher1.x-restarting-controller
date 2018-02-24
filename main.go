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

	tolerableCounts     int
	intolerableInterval int
	ignoreLabel         string
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
			Value:       "debug",
			Destination: &logLevel,
		},
		cli.IntFlag{
			Name:        "intolerable_interval",
			Usage:       "How many seconds can not be tolerated",
			EnvVar:      "INTOLERABLE_INTERVAL",
			Value:       300,
			Destination: &intolerableInterval,
		},
		cli.IntFlag{
			Name:        "tolerable_counts",
			Usage:       "The number of restarts that can be tolerated in an interval that can not be tolerated",
			EnvVar:      "TOLERABLE_COUNTS",
			Value:       3,
			Destination: &tolerableCounts,
		},
		cli.StringFlag{
			Name:        "ignore_label",
			Usage:       "Set the ignoring label",
			EnvVar:      "IGNORE_LABEL",
			Value:       "io.rancher.restarting_controller.ignore",
			Destination: &ignoreLabel,
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

		if err := controller.Start(cattleURL, cattleAccessKey, cattleSecretKey, ignoreLabel, intolerableInterval, tolerableCounts); err != nil && err != http.ErrServerClosed {
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
