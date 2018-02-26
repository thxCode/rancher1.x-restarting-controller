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

type container struct {
	name      string
	op        int
	serviceId string
}

type daily struct {
	limit            int
	containersBuffer chan container
	stopChan         chan interface{}
}

func (d *daily) close() {
	close(d.containersBuffer)
	close(d.stopChan)
}

func (d *daily) record(name string, serviceId string) {
	d.containersBuffer <- container{
		name,
		1,
		serviceId,
	}
}

func newDaily(cattleAddress, cattleAccessKey, cattleSecretKey string, limit int, intolerableInterval time.Duration) *daily {
	glog := utils.GetGlobalLogger()

	d := &daily{
		limit,
		make(chan container, 10),
		make(chan interface{}),
	}

	go func() {
		containersMap := make(map[string]int, 32)

		go func() {
			ticker := time.NewTicker(intolerableInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					for containerName := range containersMap {
						d.containersBuffer <- container{
							name: containerName,
							op:   -1,
						}
					}
				case <-d.stopChan:
					break
				}
			}
		}()

		for c := range d.containersBuffer {
			newValue := 1
			if oldValue, ok := containersMap[c.name]; ok {
				newValue = oldValue + c.op
			}
			containersMap[c.name] = newValue

			if newValue <= 0 {
				delete(containersMap, c.name)
			} else if newValue > d.limit {
				go func(containerName, serviceId string, op int) {
					if len(serviceId) != 0 {
						hc := newHttpClient(cattleAccessKey, cattleSecretKey, 60*time.Second)

						serviceBytes, err := hc.get(cattleAddress + "/services/" + serviceId)
						if err != nil {
							glog.Errorln("cannot get service info by", serviceId, err)
							return
						}

						deactivateAction, _ := jsonparser.GetString(serviceBytes, "actions", "deactivate")
						if deactivateAction != "" {
							statusCode, _ := hc.post(deactivateAction, bytes.NewBufferString("{}"))

							if statusCode == http.StatusAccepted {
								glog.Infoln("stop the service", serviceId)
							}
						} else {
							cancelupgradeAction, _ := jsonparser.GetString(serviceBytes, "actions", "cancelupgrade")
							if cancelupgradeAction != "" {
								statusCode, _ := hc.post(cancelupgradeAction, bytes.NewBufferString("{}"))

								if statusCode == http.StatusAccepted {
									glog.Infoln("cancel the upgrading of the service", serviceId)
								}
							}
						}

					}

					d.containersBuffer <- container{
						name: containerName,
						op:   -op,
					}

				}(c.name, c.serviceId, newValue)
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
					containerName, _ := jsonparser.GetString(resourceBytes, "name")
					serviceId, _ := jsonparser.GetString(resourceBytes, "serviceIds", "[0]")
					glog.Debugf("catch info on service %s container %s", serviceId, containerName)
					d.record(containerName, serviceId)
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
