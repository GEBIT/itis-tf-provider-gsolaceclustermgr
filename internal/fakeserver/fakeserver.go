/**
fakeserver to mock the solace api for testing.
Code based on / inspired by https://github.com/Mastercard/terraform-provider-restapi/blob/master/fakeserver

Note that we intentionally do NOT use generator code here!
*/

package fakeserver

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

/* Fakeserver represents a HTTP server with objects to hold and return*/
type Fakeserver struct {
	server  *http.Server
	objects map[string]ServiceInfo
	debug   bool
	running bool
}

type ServiceInfo struct {
	ID                 string
	ServiceClassId     string
	DatacenterId       string
	Name               string
	State              string
	MsgVpnName         string
	EventBrokerVersion string
	CustomRouterName   string
	ClusterName        string
	MaxSpoolUsage      int32
	Created            time.Time
	Updated            time.Time
	ClientUsername     string
	ClientPassword     string
}

/* NewFakeServer creates a HTTP server used for tests and debugging*/
func NewFakeServer(iPort int, iObjects map[string]ServiceInfo, iStart bool, iDebug bool) *Fakeserver {
	serverMux := http.NewServeMux()

	svr := &Fakeserver{
		debug:   iDebug,
		objects: iObjects,
		running: false,
	}

	serverMux.HandleFunc("/api/v2/missionControl/", svr.handleBrokerServices)
	// subtrees are also handled
	// NOTE: the trailing slash will be added automatically to the URL even when not given
	apiObjectServer := &http.Server{
		Addr:              fmt.Sprintf("127.0.0.1:%d", iPort),
		Handler:           serverMux,
		ReadHeaderTimeout: time.Minute,
	}

	svr.server = apiObjectServer

	if iStart {
		svr.StartInBackground()
	}
	if svr.debug {
		log.Printf("fakeserver.go: Set up fakeserver: port=%d, debug=%t\n", iPort, svr.debug)
	}
	log.Printf("fakeserver ready")

	return svr
}

func (svr *Fakeserver) safeServe() {
	err := svr.server.ListenAndServe()
	if err != nil {
		log.Printf("fakeserver.go: serving failed: %s\n", err)
	}
}

/*StartInBackground starts the HTTP server in the background*/
func (svr *Fakeserver) StartInBackground() {
	go svr.safeServe()

	/* Let the server start */
	time.Sleep(1 * time.Second)
	svr.running = true
}

/*Shutdown closes the server*/
func (svr *Fakeserver) Shutdown() {
	svr.server.Close()
	svr.running = false
}

/*Running returns whether the server is running*/
func (svr *Fakeserver) Running() bool {
	return svr.running
}

/*GetServer returns the server object itself*/
func (svr *Fakeserver) GetServer() *http.Server {
	return svr.server
}

func (svr *Fakeserver) parseRequest(r *http.Request, parts *[]string) ([]byte, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("fakeserver.go: failed to read body: %s\n", err)
		return nil, err
	}

	/** we don't handle bearer token right now */

	if svr.debug {
		log.Printf("fakeserver.go: Received request: %+v\n", r)
		log.Printf("fakeserver.go: Headers:\n")
		for name, headers := range r.Header {
			name = strings.ToLower(name)
			for _, h := range headers {
				log.Printf("fakeserver.go:  %v: %v", name, h)
			}
		}
		log.Printf("fakeserver.go: BODY: %s\n", string(b))
	}

	path := r.URL.EscapedPath()

	*parts = strings.Split(path, "/") // note: the first part is empty
	if svr.debug {
		log.Printf("fakeserver.go: Request received: %s %s\n", r.Method, path)
		log.Printf("fakeserver.go: Split request up into %d parts: %v\n", len(*parts), *parts)
		if r.URL.RawQuery != "" {
			log.Printf("fakeserver.go: Query string: %s\n", r.URL.RawQuery)
		}
	}
	return b, nil
}

func (svr *Fakeserver) handleCreate(w http.ResponseWriter, body []byte) {
	var jObj map[string]interface{}

	/* handle creation */
	sid := uuid.New().String()

	err := json.Unmarshal(body, &jObj)
	if err != nil {
		log.Printf("fakeserver.go: Unmarshal of request failed: %s\n", err)
		log.Printf("\nBEGIN passed data:\n%s\nEND passed data.", string(body))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// parse and store obj
	sInfo := ServiceInfo{
		ID:                 sid,
		Name:               jObj["name"].(string),
		State:              "PENDING",
		ServiceClassId:     jObj["serviceClassId"].(string),
		DatacenterId:       jObj["datacenterId"].(string),
		ClusterName:        orDefault(jObj["clusterName"], "test-cluster1"),
		MsgVpnName:         orDefault(jObj["msgVpnName"], "test-vpn1"),
		EventBrokerVersion: orDefault(jObj["eventBrokerVersion"], "1.0.0"),
		CustomRouterName:   orDefault(jObj["customRouterName"], "test-router1"),
		MaxSpoolUsage:      orDefaultInt32(jObj["maxSpoolUsage"], 20),
		Created:            time.Now(),
		ClientUsername:     "client-user",
		ClientPassword:     "client-passwd",
	}
	svr.objects[sid] = sInfo
	if svr.debug {
		log.Printf("Created Info: %v)", sInfo)
		log.Printf("vpn: %v, jobj: %v)", sInfo.MsgVpnName, jObj["msgVpnName"])
	}
	// return created obj
	result := map[string]interface{}{
		"data": map[string]interface{}{
			"id":            "O" + sInfo.ID, // the id of the operation
			"resourceId":    sInfo.ID,       // the id of the service resource
			"name":          sInfo.Name,
			"createdTime":   sInfo.Created.Format(time.RFC3339),
			"creationState": sInfo.State,
			// do not yet fill details
		},
		"meta": map[string]interface{}{
			"additionalProp": map[string]interface{}{},
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		log.Printf("fakeserver.go: failed to marshal result: %s\n", err)
		return
	}

	w.Header().Add("Content-Type", "json")
	w.WriteHeader(202)
	_, err2 := w.Write(b)
	if err2 != nil {
		log.Printf("fakeserver.go: failed to write result: %s\n", err)
	}
}

func (svr *Fakeserver) handleGet(w http.ResponseWriter, sInfo *ServiceInfo, id string) {
	// complete creation after a certain delay, so we can test PENDING answers
	if sInfo.State == "PENDING" {
		sInfo.Updated = time.Now() // the actual semantics of updated when pending are unclear, but not really important
		if time.Since(sInfo.Created).Seconds() > 5.0 {
			sInfo.State = "COMPLETED"
		}
		// writeback change
		svr.objects[id] = *sInfo
	}
	if svr.debug {
		log.Printf("fakeserver.go: GET service %v", sInfo)
	}

	result := map[string]interface{}{
		"data": map[string]interface{}{
			"id":                        sInfo.ID,
			"name":                      sInfo.Name,
			"serviceClassId":            sInfo.ServiceClassId,
			"datacenterId":              sInfo.DatacenterId,
			"createdTime":               sInfo.Created.Format(time.RFC3339),
			"updatedTime":               sInfo.Updated.Format(time.RFC3339),
			"creationState":             sInfo.State,
			"eventBrokerServiceVersion": sInfo.EventBrokerVersion,
			"broker": map[string]interface{}{
				"cluster": map[string]interface{}{
					"name":              sInfo.ClusterName,
					"primaryRouterName": sInfo.CustomRouterName,
				},
				"msgVpns": []interface{}{
					map[string]interface{}{
						"msgVpnName": sInfo.MsgVpnName,
						"serviceLoginCredential": map[string]interface{}{
							"username": sInfo.ClientUsername,
							"password": sInfo.ClientPassword,
						},
					},
				},
				"maxSpoolUsage": sInfo.MaxSpoolUsage,
			},
		},
		"meta": map[string]interface{}{
			"additionalProp": map[string]interface{}{},
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		log.Printf("fakeserver.go: failed to marshal result: %s\n", err)
		return
	}
	w.Header().Add("Content-Type", "json")
	_, err2 := w.Write(b)
	if err2 != nil {
		log.Printf("fakeserver.go: failed to write result: %s\n", err)
	}
}

func (svr *Fakeserver) handlePatch(w http.ResponseWriter, sInfo *ServiceInfo, id string, body []byte) {
	var jObj map[string]interface{}

	if svr.debug {
		log.Printf("fakeserver.go: PATCH service %v", sInfo)
	}

	err := json.Unmarshal(body, &jObj)
	if err != nil {
		log.Printf("fakeserver.go: Unmarshal of request failed: %s\n", err)
		log.Printf("\nBEGIN passed data:\n%s\nEND passed data.", string(body))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if svr.debug {
		log.Printf("fakeserver.go: receipt payload %v", jObj)
	}

	// handle update - only supported when get returns actual completed service
	sInfo.Name = jObj["name"].(string)
	sInfo.State = "PENDING"
	sInfo.Updated = time.Now()

	if svr.debug {
		log.Printf("fakeserver.go: PATCH service updated to %v", sInfo)
	}

	result := map[string]interface{}{
		"data": map[string]interface{}{
			"id":                        sInfo.ID,
			"serviceClassId":            sInfo.ServiceClassId,
			"name":                      sInfo.Name,
			"createdTime":               sInfo.Created.Format(time.RFC3339),
			"updatedTime":               sInfo.Updated.Format(time.RFC3339),
			"creationState":             sInfo.State,
			"eventBrokerServiceVersion": sInfo.EventBrokerVersion,

			"broker": map[string]interface{}{
				"cluster": map[string]interface{}{
					"name":              sInfo.ClusterName,
					"primaryRouterName": sInfo.CustomRouterName,
				},
				"msgVpns": []interface{}{
					map[string]interface{}{
						"msgVpnName": sInfo.MsgVpnName,
					},
				},
				"maxSpoolUsage": sInfo.MaxSpoolUsage,
			},
		},
		"meta": map[string]interface{}{
			"additionalProp": map[string]interface{}{},
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		log.Printf("fakeserver.go: failed to marshal result: %s\n", err)
		return
	}

	// writeback change
	svr.objects[id] = *sInfo

	w.Header().Add("Content-Type", "json")
	_, err2 := w.Write(b)
	if err2 != nil {
		log.Printf("fakeserver.go: failed to write result: %s\n", err)
	}
}

func (svr *Fakeserver) handleDelete(w http.ResponseWriter, sInfo *ServiceInfo, id string) {
	if svr.debug {
		log.Printf("fakeserver.go: DELETE service %v", sInfo)
	}
	// handle delete
	delete(svr.objects, id)
	// return status DELETING
	result := map[string]interface{}{
		"data": map[string]interface{}{
			"id":          "O" + sInfo.ID,
			"resourceId":  sInfo.ID,
			"name":        sInfo.Name,
			"createdTime": sInfo.Created.Format(time.RFC3339),
			"status":      "PENDING",
		},
		"meta": map[string]interface{}{
			"additionalProp": map[string]interface{}{},
		},
	}
	b, err := json.Marshal(result)
	if err != nil {
		log.Printf("fakeserver.go: failed to marshal result: %s\n", err)
		return
	}
	w.Header().Add("Content-Type", "json")
	w.WriteHeader(202)
	_, err2 := w.Write(b)
	if err2 != nil {
		log.Printf("fakeserver.go: failed to write result: %s\n", err)
	}
}

func (svr *Fakeserver) handleBrokerServices(w http.ResponseWriter, r *http.Request) {

	var sInfo ServiceInfo
	var id string
	var ok bool
	var parts []string
	var body []byte

	body, err := svr.parseRequest(r, &parts)
	if err != nil {
		return
	}

	if (len(parts) == 5 || (len(parts) == 6 && parts[5] == "")) && r.Method == "POST" {
		svr.handleCreate(w, body)
		return
	} else if len(parts) == 6 {
		// an obj was specified.
		id = parts[5]
		sInfo, ok = svr.objects[id]
		if svr.debug {
			log.Printf("fakeserver.go: Detected ID %s (exists: %t, method: %s)", id, ok, r.Method)
		}
		if !ok {
			log.Printf("Object with ID %s not found", id)
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		switch r.Method {
		case "GET":
			svr.handleGet(w, &sInfo, id)
			return
		case "PATCH":
			svr.handlePatch(w, &sInfo, id, body)
			return
		case "DELETE":
			svr.handleDelete(w, &sInfo, id)
			return
		default:
			log.Printf("fakeserver.go: unexpected method: %s\n", r.Method)
		}

	}
	// unexpected
	if svr.debug {
		log.Printf("fakeserver.go: Bad request!")
	}
	http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)

}

func orDefault(s interface{}, ds string) string {
	if s != nil && s.(string) != "" {
		return s.(string)
	}
	return ds
}

func orDefaultInt32(s interface{}, ds int32) int32 {
	if s != nil {
		// json will generically map numbers to float64
		return int32(s.(float64))
	}
	return ds
}
