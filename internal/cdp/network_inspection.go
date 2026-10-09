package cdp

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/trace"
)

type networkInspection struct {
	postData  map[string]string
	postOrder []string
	postBytes int
	redirects map[string]map[string]any
}

func (s *session) rememberPostData(id, data string) {
	if data == "" || len(data) > 1<<20 {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.networkInspection == nil {
		s.networkInspection = &networkInspection{}
	}
	history := s.networkInspection
	if history.postData == nil {
		history.postData = make(map[string]string)
	}
	if old, ok := history.postData[id]; ok {
		history.postBytes -= len(old)
	} else {
		history.postOrder = append(history.postOrder, id)
	}
	history.postData[id] = data
	history.postBytes += len(data)
	for len(history.postOrder) > 128 || history.postBytes > 4<<20 {
		old := history.postOrder[0]
		history.postOrder = history.postOrder[1:]
		history.postBytes -= len(history.postData[old])
		delete(history.postData, old)
	}
}

func (s *session) networkEvent(e trace.Event) {
	if !s.domainEnabled("Network") || strings.HasPrefix(stringValue(e.Data["url"]), "data:") {
		return
	}
	frameID := stringValue(e.Data["context"])
	if frameID == "" {
		frameID = s.page.Top.ID
	}
	loaderID := s.frameLoaderID(frameID)
	if e.Data["initiator"] == network.Iframe {
		loaderID = stringValue(e.Data["id"])
	}
	id := stringValue(e.Data["id"])
	timestamp := float64(e.Time.UnixMilli()) / 1000
	if e.Name == "request" {
		postData := stringValue(e.Data["postData"])
		s.rememberPostData(id, postData)
		request := map[string]any{"url": e.Data["url"], "method": e.Data["method"], "headers": e.Data["headers"]}
		if postData != "" {
			request["postData"] = postData
			request["hasPostData"] = true
			request["postDataEntries"] = []any{map[string]any{"bytes": base64.StdEncoding.EncodeToString([]byte(postData))}}
		}
		payload := map[string]any{"requestId": id, "loaderId": loaderID, "documentURL": s.page.URL(), "request": request, "timestamp": timestamp, "wallTime": float64(e.Time.UnixMilli()) / 1000, "initiator": map[string]any{"type": "other"}, "type": resourceTypeFromTrace(e.Data["initiator"]), "frameId": frameID}
		s.stateMu.Lock()
		if history := s.networkInspection; history != nil {
			if redirect := history.redirects[id]; redirect != nil {
				payload["redirectResponse"] = redirect
				payload["redirectHasExtraInfo"] = false
				delete(history.redirects, id)
			}
		}
		s.stateMu.Unlock()
		s.event("Network.requestWillBeSent", payload)
	} else if e.Name == "response" {
		response := map[string]any{"url": e.Data["url"], "status": e.Data["status"], "statusText": http.StatusText(intValue(e.Data["status"], 0)), "headers": e.Data["headers"], "mimeType": e.Data["mimeType"], "connectionReused": e.Data["connectionReused"], "connectionId": s.server.networkConnectionID(stringValue(e.Data["connectionId"])), "protocol": cdpProtocol(e.Data["protocol"]), "timing": cdpResourceTiming(e.Data["transportTiming"]), "encodedDataLength": e.Data["encodedDataLength"], "securityState": "unknown", "fromDiskCache": e.Data["fromCache"]}
		status := intValue(e.Data["status"], 0)
		if (status == 301 || status == 302 || status == 303 || status == 307 || status == 308) && stringValue(e.Data["redirectLocation"]) != "" && stringValue(e.Data["redirectMode"]) != "manual" {
			s.stateMu.Lock()
			if s.networkInspection == nil {
				s.networkInspection = &networkInspection{}
			}
			if s.networkInspection.redirects == nil {
				s.networkInspection.redirects = make(map[string]map[string]any)
			}
			s.networkInspection.redirects[id] = response
			s.stateMu.Unlock()
			return
		}
		s.event("Network.responseReceived", map[string]any{"requestId": id, "loaderId": loaderID, "timestamp": timestamp, "type": resourceTypeFromTrace(e.Data["initiator"]), "response": response, "frameId": frameID, "hasExtraInfo": false})
		if partial, _ := e.Data["partial"].(bool); !partial {
			s.event("Network.dataReceived", map[string]any{"requestId": id, "timestamp": timestamp, "dataLength": e.Data["decodedBodySize"], "encodedDataLength": e.Data["encodedBodySize"]})
			s.event("Network.loadingFinished", map[string]any{"requestId": id, "timestamp": timestamp, "encodedDataLength": e.Data["encodedDataLength"]})
		}
	} else if e.Name == "failed" {
		s.stateMu.Lock()
		if s.networkInspection != nil {
			delete(s.networkInspection.redirects, id)
		}
		s.stateMu.Unlock()
		canceled, _ := e.Data["canceled"].(bool)
		errorText := e.Data["error"]
		if canceled {
			errorText = "net::ERR_ABORTED"
		}
		s.event("Network.loadingFailed", map[string]any{"requestId": id, "timestamp": timestamp, "type": resourceTypeFromTrace(e.Data["initiator"]), "errorText": errorText, "canceled": canceled})
	}
}
