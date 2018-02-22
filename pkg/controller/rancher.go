package controller

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/buger/jsonparser"
	"github.com/thxcode/rancher1.x-restarting-controller/pkg/utils"
)

type rancherClient struct {
	client *http.Client

	cattleAK string
	cattleSK string
}

func (r *rancherClient) get(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(r.cattleAK, r.cattleSK)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if bs, err := ioutil.ReadAll(resp.Body); err != nil {
		return nil, err
	} else {
		return bs, nil
	}
}

func (r *rancherClient) post(url string, body io.Reader) (int, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return 0, err
	}

	req.SetBasicAuth(r.cattleAK, r.cattleSK)
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}

func newRancherClient(cattleAccessKey, cattleSecretKey string) *rancherClient {
	return &rancherClient{
		&http.Client{Timeout: 60 * time.Second},
		cattleAccessKey,
		cattleSecretKey,
	}
}

type restartingContainer struct {
	pid               int64
	restartCount      int64
	restartCountLimit int64
	deactivateActions []string
}

func (rc *restartingContainer) shouldBeStopped(newPid int64) bool {
	if rc.pid != newPid {
		rc.pid = newPid

		rc.restartCount += 1
	}

	return rc.restartCount > rc.restartCountLimit
}

func (rc *restartingContainer) stop(r *rancherClient) {
	for _, deactivateAction := range rc.deactivateActions {
		r.post(deactivateAction, bytes.NewBufferString("{}"))
	}
}

var (
	mutex                  = &sync.Mutex{}
	stopChan               = make(chan interface{}, 1)
	restartingContainerMap = map[string]*restartingContainer{}
)

func Start(cattleAddress, cattleAccessKey, cattleSecretKey, watchLabel string) error {
	glog := utils.GetGlobalLogger()
	rc := newRancherClient(cattleAccessKey, cattleSecretKey)

	glog.Infoln("scraping cattle labels...")
	for {
		select {
		case <-stopChan:
			break
		default:
			mutex.Lock()

			// scrap labels
			labelsScrapAddress := cattleAddress + "/labels?limit=100&key=" + watchLabel
			for {
				glog.Debugln("<start> get labels:", labelsScrapAddress)
				labelsBytes, err := rc.get(labelsScrapAddress)
				if err != nil {
					glog.Warnln(err)
					break
				}

				// iterate labels
				jsonparser.ArrayEach(labelsBytes, func(labelBytes []byte, dataType jsonparser.ValueType, offset int, labelErr error) {
					labelValueString, err := jsonparser.GetString(labelBytes, "value") // restartCount
					if err != nil {
						glog.Warnln(err)
						return
					}

					labelValue, err := strconv.ParseInt(labelValueString, 10, 0)
					if err != nil {
						glog.Warnln(err)
						return
					}
					// scrap instances by label
					instancesScrapAddress, err := jsonparser.GetString(labelBytes, "links", "instances")
					if err != nil {
						glog.Warnln(err)
						return
					}
					instancesScrapAddress += "?limit=100&kind=container&allocationState=active"
					for {
						glog.Debugln("<start> get instances:", labelsScrapAddress)
						instancesBytes, err := rc.get(instancesScrapAddress)
						if err != nil {
							glog.Warnln(err)
							return
						}

						// iterate instances
						jsonparser.ArrayEach(instancesBytes, func(instanceBytes []byte, dataType jsonparser.ValueType, offset int, instanceErr error) {
							if instanceType, err := jsonparser.GetString(instanceBytes, "type"); err != nil {
								glog.Warnln(err)
								return
							} else if instanceType != "container" {
								return
							}

							if instanceAllocationState, err := jsonparser.GetString(instanceBytes, "allocationState"); err != nil {
								glog.Warnln(err)
								return
							} else if instanceAllocationState != "active" {
								return
							}

							containerId, err := jsonparser.GetString(instanceBytes, "id") // unexpected containerId
							if err != nil {
								glog.Warnln(err)
								return
							}

							pid, err := jsonparser.GetInt(instanceBytes, "data", "dockerInspect", "State", "Pid")
							if err != nil {
								glog.Warnln(err)
								return
							}

							if c, ok := restartingContainerMap[containerId]; !ok {
								var deactivateActions []string
								// scrap services by instance
								servicesScrapAddress, err := jsonparser.GetString(instanceBytes, "links", "services")
								if err != nil {
									glog.Warnln(err)
									return
								}
								servicesScrapAddress += "?limit=100"
								for {
									glog.Debugln("<start> get services:", servicesScrapAddress)
									servicesBytes, err := rc.get(servicesScrapAddress)
									if err != nil {
										glog.Warnln(err)
										return
									}

									// iterate services
									jsonparser.ArrayEach(servicesBytes, func(serviceBytes []byte, dataType jsonparser.ValueType, offset int, serviceErr error) {
										if serviceState, err := jsonparser.GetString(serviceBytes, "state"); err != nil {
											glog.Warnln(err)
											return
										} else if serviceState == "inactive" {
											return
										}

										if serviceHealthState, err := jsonparser.GetString(serviceBytes, "healthState"); err != nil {
											glog.Warnln(err)
											return
										} else if serviceHealthState == "healthy" {
											return
										}

										if deactivateAction, err := jsonparser.GetString(serviceBytes, "actions", "deactivate"); err != nil && err != jsonparser.KeyPathNotFoundError {
											glog.Warnln(err)
											return
										} else if deactivateAction != "" {
											deactivateActions = append(deactivateActions, deactivateAction)
										}

									}, "data")

									if next, _ := jsonparser.GetString(servicesBytes, "pagination", "next"); next != "" {
										servicesScrapAddress = next
									} else {
										glog.Debugln("<end> get services:", servicesScrapAddress)
										break
									}
								}

								if len(deactivateActions) != 0 {
									restartingContainerMap[containerId] = &restartingContainer{
										pid,
										0,
										labelValue,
										deactivateActions,
									}
								}
							} else if c.shouldBeStopped(pid) {
								glog.Infoln("going to stop container", containerId)
								c.stop(rc)
								delete(restartingContainerMap, containerId)
							}
						}, "data")

						if next, _ := jsonparser.GetString(instancesBytes, "pagination", "next"); next != "" {
							instancesScrapAddress = next
						} else {
							glog.Debugln("<end> get instances:", instancesScrapAddress)
							break
						}
					}

				}, "data")

				if next, _ := jsonparser.GetString(labelsBytes, "pagination", "next"); next != "" {
					labelsScrapAddress = next
				} else {
					glog.Debugln("<end> get labels:", labelsScrapAddress)
					break
				}
			}

			mutex.Unlock()
		}
	}

	return nil
}

func Stop() error {
	close(stopChan)

	return nil
}
