package controller

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/buger/jsonparser"
	"github.com/gorilla/websocket"
	"github.com/thxcode/rancher1.x-restarting-controller/pkg/utils"
)

type httpClient struct {
	client *http.Client

	cattleAK string
	cattleSK string
}

func (r *httpClient) get(url string) ([]byte, error) {
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

func (r *httpClient) post(url string, body io.Reader) (int, error) {
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

func newHttpClient(cattleAccessKey, cattleSecretKey string, timeoutSecond time.Duration) *httpClient {
	return &httpClient{
		&http.Client{Timeout: timeoutSecond},
		cattleAccessKey,
		cattleSecretKey,
	}
}

func newWebsocketConn(cattleAddress, cattleAccessKey, cattleSecretKey string) *websocket.Conn {
	// get dialer address
	hc := newHttpClient(cattleAccessKey, cattleSecretKey, 5*time.Second)

	projectsResponseBytes, err := hc.get(cattleAddress + "/projects")
	if err != nil {
		panic(errors.New(fmt.Sprintf("cannot get project info, %v", err)))
	}
	projectLinksSelf, err := jsonparser.GetString(projectsResponseBytes, "data", "[0]", "links", "self")
	if err != nil {
		panic(errors.New(fmt.Sprintf("cannot get project self address, %v", err)))
	}
	if strings.HasPrefix(projectLinksSelf, "http://") {
		projectLinksSelf = strings.Replace(projectLinksSelf, "http://", "ws://", -1)
	} else {
		projectLinksSelf = strings.Replace(projectLinksSelf, "https://", "wss://", -1)
	}
	dialAddress := projectLinksSelf + "/subscribe?eventNames=resource.change&limit=-1&sockId=1"

	httpHeaders := http.Header{}
	httpHeaders.Add("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cattleAccessKey+":"+cattleSecretKey)))
	websocketConn, _, err := websocket.DefaultDialer.Dial(dialAddress, httpHeaders)
	if err != nil {
		panic(err)
	}

	return websocketConn
}

type log struct {
	id string
	op int
}

type daily struct {
	limit    int
	logChan  chan log
	stopChan chan interface{}
}

func (d *daily) close() {
	close(d.logChan)
	close(d.stopChan)
}

func (d *daily) record(dockerId string) {
	d.logChan <- log{
		dockerId,
		1,
	}
}

func newDaily(cattleAddress, cattleAccessKey, cattleSecretKey string, limit int, intolerableInterval time.Duration) *daily {
	glog := utils.GetGlobalLogger()

	d := &daily{
		limit,
		make(chan log, 10),
		make(chan interface{}),
	}

	go func() {
		logMap := make(map[string]int, 32)

		go func() {
			ticker := time.NewTicker(intolerableInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					for dockerId := range logMap {
						d.logChan <- log{
							dockerId,
							-1,
						}
					}
				case <-d.stopChan:
					break
				}
			}
		}()

		for l := range d.logChan {
			newValue := 1
			if oldValue, ok := logMap[l.id]; ok {
				newValue = oldValue + l.op
			}
			logMap[l.id] = newValue

			if newValue <= 0 {
				delete(logMap, l.id)
			} else if newValue > d.limit {
				go func(dockerId string, op int) {
					hc := newHttpClient(cattleAccessKey, cattleSecretKey, 60*time.Second)

					instancesBytes, err := hc.get(cattleAddress + "/instances?externalId=" + dockerId)
					if err != nil {
						glog.Errorln("cannot get instance by", dockerId, err)
						return
					}

					servicesAddress, _ := jsonparser.GetString(instancesBytes, "data", "[0]", "links", "services")
					if servicesAddress == "" {
						return
					}
					servicesBytes, err := hc.get(servicesAddress)
					if err != nil {
						glog.Errorln("cannot get service by", servicesAddress, err)
						return
					}

					jsonparser.ArrayEach(servicesBytes, func(serviceBytes []byte, dataType jsonparser.ValueType, offset int, err error) {
						deactivateAction, _ := jsonparser.GetString(serviceBytes, "actions", "deactivate")
						if deactivateAction != "" {
							statusCode, _ := hc.post(deactivateAction, bytes.NewBufferString("{}"))

							if statusCode == http.StatusAccepted {
								glog.Infoln("stop the service of docker", dockerId)
							}
						} else {
							cancelupgradeAction, _ := jsonparser.GetString(serviceBytes, "actions", "cancelupgrade")
							if cancelupgradeAction != "" {
								statusCode, _ := hc.post(cancelupgradeAction, bytes.NewBufferString("{}"))

								if statusCode == http.StatusAccepted {
									glog.Infoln("cancel the upgrade of the service of docker", dockerId)
								}
							}
						}
					}, "data")

					d.logChan <- log{
						dockerId,
						-newValue,
					}

				}(l.id, newValue)
			}
		}
	}()

	return d
}

var (
	d    *daily
	conn *websocket.Conn
)

func Start(cattleAddress, cattleAccessKey, cattleSecretKey, ignoreLabel string, intolerableInterval, tolerableCounts int) error {
	glog := utils.GetGlobalLogger()

	// create daily
	d = newDaily(cattleAddress, cattleAccessKey, cattleSecretKey, tolerableCounts, time.Duration(intolerableInterval)*time.Second)

	// create dialer
	conn = newWebsocketConn(cattleAddress, cattleAccessKey, cattleSecretKey)

	// watching
	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			glog.Warnln(err)
			break
		}

		if resourceType, _ := jsonparser.GetString(messageBytes, "resourceType"); resourceType == "container" {
			resourceBytes, _, _, err := jsonparser.Get(messageBytes, "data", "resource")
			if err != nil {
				continue
			}

			if state, _ := jsonparser.GetString(resourceBytes, "state"); state == "stopped" || state == "error" {
				if val, err := jsonparser.GetString(resourceBytes, "labels", ignoreLabel); val != "true" || err == jsonparser.KeyPathNotFoundError {
					dockerId, _ := jsonparser.GetString(resourceBytes, "externalId")
					d.record(dockerId)
				}

			}
		}
	}

	return nil
}

func Stop() error {
	if conn != nil {
		conn.Close()
	}

	if d != nil {
		d.close()
	}

	return nil
}
