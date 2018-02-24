# Rancher1.x Restart Action Controller

A controller of Rancher1.x to manage the restart action of container.

[![](https://img.shields.io/badge/Github-thxcode/rancher1.x--restarting--controller-orange.svg)](https://github.com/thxcode/rancher1.x-restarting-controller)&nbsp;[![](https://img.shields.io/badge/Docker_Hub-maiwj/rancher1.x--restarting--controller-orange.svg)](https://hub.docker.com/r/maiwj/rancher1.x-restarting-controller)&nbsp;[![](https://img.shields.io/docker/build/maiwj/rancher1.x-restarting-controller.svg)](https://hub.docker.com/r/maiwj/rancher1.x-restarting-controller)&nbsp;[![](https://img.shields.io/docker/pulls/maiwj/rancher1.x-restarting-controller.svg)](https://store.docker.com/community/images/maiwj/rancher1.x-restarting-controller)&nbsp;[![](https://img.shields.io/github/license/thxcode/rancher1.x-restarting-controller.svg)](https://github.com/thxcode/rancher1.x-restarting-controller)

[![](https://images.microbadger.com/badges/image/maiwj/rancher1.x-restarting-controller.svg)](https://microbadger.com/images/maiwj/rancher1.x-restarting-controller)&nbsp;[![](https://images.microbadger.com/badges/version/maiwj/rancher1.x-restarting-controller.svg)](http://microbadger.com/images/maiwj/rancher1.x-restarting-controller)&nbsp;[![](https://images.microbadger.com/badges/commit/maiwj/rancher1.x-restarting-controller.svg)](http://microbadger.com/images/maiwj/rancher1.x-restarting-controller.svg)

## References

### Rancher version supported

- [v1.6.12 and above](https://github.com/rancher/rancher/releases/tag/v1.6.12)

## How to use this image

- This controller is deployed on each "Environment" (as same as "Infrastructure") and labeled by `io.rancher.container.create_agent=true, io.rancher.container.agent.role=environment`, it will detect the `stopped` status of each "Container".
- By default, this controller have `300` intolerable seconds, which means it can allow one "Container" to be restarted below `3` times during the 300 seconds.
- If one "Container" has been labeled by `io.rancher.restarting_controller.ignore=true`, this controller will never manage it.

### Running parameters

```bash
$ rancher-restarting-controller -h
NAME:
   rancher-restarting-controller - A controller of Rancher1.x to manage the restart action of container.

USAGE:
   rancher-restarting-controller [global options] command [command options] [arguments...]

...

COMMANDS:
     help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
 --cattle_url value            The URL of Rancher Server API, e.g. http://127.0.0.1:8080 [$CATTLE_URL]
 --cattle_access_key value     The access key for Rancher API [$CATTLE_ACCESS_KEY]
 --cattle_secret_key value     The secret key for Rancher API [$CATTLE_SECRET_KEY]
 --log_level value             Set the logging level (default: "debug") [$LOG_LEVEL]
 --intolerable_interval value  How many seconds can not be tolerated (default: 300) [$INTOLERABLE_INTERVAL]
 --tolerable_counts value      The number of restarts that can be tolerated in an interval that can not be tolerated (default: 3) [$TOLERABLE_COUNTS]
 --ignore_label value          Set the ignoring label (default: "io.rancher.restarting_controller.ignore") [$IGNORE_LABEL]
 --help, -h                    show help
 --version, -v                 print the version

```

### Start an instance

To start a container, use the following:

``` bash
$ docker run -d --name test-rrc -p 9173:9173 -e CATTLE_URL=<cattel_url> -e CATTLE_ACCESS_KEY=<cattel_ak> -e CATTLE_SECRET_KEY=<cattel_sk> maiwj/rancher1.x-restarting-controller

```

## License

- Rancher is released under the [Apache License 2.0](https://github.com/rancher/rancher/blob/master/LICENSE)
- This image is released under the [MIT License](LICENSE)