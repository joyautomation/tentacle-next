//go:build api || all

package api

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/sparkplug"
	"github.com/joyautomation/tentacle/internal/topics"
)

// handleBrowseTags sends a browse request to the protocol scanner.
// POST /api/v1/browse/{protocol}
func (m *Module) handleBrowseTags(w http.ResponseWriter, r *http.Request) {
	protocol := chi.URLParam(r, "protocol")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	resp, err := m.bus.Request(topics.Browse(protocol), body, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "browse request failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleStreamBrowseProgress streams browse progress events via SSE.
// GET /api/v1/browse/{browseId}/progress
func (m *Module) handleStreamBrowseProgress(w http.ResponseWriter, r *http.Request) {
	browseID := chi.URLParam(r, "browseId")

	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Subscribe with wildcard to catch any protocol's browse progress.
	subject := fmt.Sprintf("*.browse.progress.%s", browseID)

	sub, err := m.bus.Subscribe(subject, func(_ string, data []byte, _ bus.ReplyFunc) {
		sse.WriteEvent("progress", json.RawMessage(data))
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	<-r.Context().Done()
}

// handleBrowseStartBus is the bus-side of startGatewayBrowse — wired by the
// api module's Start() to topics.GatewayBrowseStart. Decodes the request,
// runs the same start path the HTTP handler uses, and replies with a
// GatewayBrowseStartReply envelope.
func (m *Module) handleBrowseStartBus(_ string, data []byte, reply bus.ReplyFunc) {
	if reply == nil {
		return
	}
	var req topics.GatewayBrowseStartRequest
	if err := json.Unmarshal(data, &req); err != nil {
		respondReply(reply, topics.GatewayBrowseStartReply{Error: "invalid request: " + err.Error()})
		return
	}
	if req.Input == nil {
		req.Input = make(map[string]interface{})
	}
	browseID, deviceID, err := m.startGatewayBrowse(req.GatewayID, req.Input)
	if err != nil {
		respondReply(reply, topics.GatewayBrowseStartReply{DeviceID: deviceID, Error: err.Error()})
		return
	}
	respondReply(reply, topics.GatewayBrowseStartReply{BrowseID: browseID, DeviceID: deviceID})
}

func respondReply(reply bus.ReplyFunc, v interface{}) {
	data, _ := json.Marshal(v)
	_ = reply(data)
}

// handleStartGatewayBrowse initiates an async browse for a specific gateway device.
// POST /api/v1/gateways/{gatewayId}/browse
func (m *Module) handleStartGatewayBrowse(w http.ResponseWriter, r *http.Request) {
	gatewayID := chi.URLParam(r, "gatewayId")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	var params map[string]interface{}
	if err := json.Unmarshal(body, &params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}

	// Remote/target mode: forward to mantle's sparkplug-host, which publishes
	// a Node Control/Browse NCMD and waits (synchronously) for the edge's
	// Node Status/Browse NDATA reply. Result/cache lands later as a
	// `_meta/browse` DDATA — same async browse pipeline as local, just on
	// the other side of the broker.
	if t := parseTarget(r); t.IsRemote() {
		deviceID, _ := params["deviceId"].(string)
		protocol, _ := params["protocol"].(string)
		if protocol == "" {
			writeError(w, http.StatusBadRequest, "missing required field: protocol")
			return
		}
		input, _ := json.Marshal(params)
		brParams, _ := json.Marshal(sparkplug.BrowseRequestParams{
			GatewayID: gatewayID,
			DeviceID:  deviceID,
			Protocol:  protocol,
			Input:     input,
		})
		req, _ := json.Marshal(sparkplug.HostVerbRequest{
			GroupID:   t.Group,
			NodeID:    t.Node,
			Verb:      sparkplug.VerbBrowse,
			Params:    brParams,
			TimeoutMs: 15000,
		})
		respData, err := m.bus.Request(sparkplug.SubjectHostVerb, req, 20*time.Second)
		if err != nil {
			writeError(w, http.StatusBadGateway, "sparkplug-host unavailable: "+err.Error())
			return
		}
		// Reply may be either an RPCResponse or {"error":"..."} envelope from
		// the host when it failed before publishing NCMD.
		var envelope struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(respData, &envelope)
		if envelope.Error != "" {
			writeError(w, http.StatusBadGateway, envelope.Error)
			return
		}
		var resp sparkplug.RPCResponse
		if err := json.Unmarshal(respData, &resp); err != nil {
			writeError(w, http.StatusBadGateway, "decode reply: "+err.Error())
			return
		}
		if !resp.OK {
			writeError(w, http.StatusBadGateway, resp.Error)
			return
		}
		var result sparkplug.BrowseRequestResult
		_ = json.Unmarshal(resp.Result, &result)
		// Register an in-flight BrowseState so polling clients (e.g. the
		// tag-config page) can observe completion without inventing their own
		// tracking. Completion is signaled by the sparkplug-host bus event
		// SubjectHostBrowseCacheUpdated (subscribed in api.Start).
		if result.BrowseID != "" {
			m.browseMu.Lock()
			m.browseStates[result.BrowseID] = &BrowseState{
				BrowseID:  result.BrowseID,
				GatewayID: gatewayID,
				DeviceID:  deviceID,
				Protocol:  protocol,
				GroupID:   t.Group,
				NodeID:    t.Node,
				Status:    "browsing",
				StartedAt: time.Now().UnixMilli(),
			}
			m.browseMu.Unlock()
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"browseId": result.BrowseID,
			"deviceId": deviceID,
		})
		return
	}

	browseID, deviceID, err := m.startGatewayBrowse(gatewayID, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"browseId": browseID, "deviceId": deviceID})
}

// startGatewayBrowse is the bus-callable core of the gateway browse handler.
// Both the HTTP path and the mqtt bridge's NCMD dispatcher invoke this so
// the result subscription, KV persistence, and "_meta/browse" emission run
// the same way regardless of trigger source. Returns (browseID, deviceID, err).
// Implementation note: subscriptions are owned by this method's closure and
// torn down when the browse reaches a terminal state (completed/failed/cancelled).
func (m *Module) startGatewayBrowse(gatewayID string, params map[string]interface{}) (string, string, error) {
	protocol, _ := params["protocol"].(string)
	if protocol == "" {
		return "", "", fmt.Errorf("missing required field: protocol")
	}

	deviceID, _ := params["deviceId"].(string)

	// Generate a browseId if not provided.
	browseID, _ := params["browseId"].(string)
	if browseID == "" {
		b := make([]byte, 8)
		rand.Read(b)
		browseID = fmt.Sprintf("%x", b)
	}

	// Inject browseId and async flag into the request body for the scanner.
	params["browseId"] = browseID
	params["async"] = true
	enrichedBody, _ := json.Marshal(params)

	cacheKey := gatewayID + ":" + deviceID

	// cleanupBrowse unsubscribes all browse-related subscriptions.
	// Safe to call multiple times (each sub's Unsubscribe is idempotent).
	var subs []bus.Subscription
	cleanupBrowse := func() {
		for _, s := range subs {
			_ = s.Unsubscribe()
		}
	}

	// finishBrowse sets terminal state and triggers cleanup.
	finishBrowse := func(status string, result json.RawMessage) {
		m.browseMu.Lock()
		defer m.browseMu.Unlock()
		state := m.browseStates[browseID]
		if state == nil || state.Status != "browsing" {
			return // already terminal
		}
		state.Status = status
		state.Result = result
		go cleanupBrowse()
	}

	// Record browse as in-progress.
	m.browseMu.Lock()
	m.browseStates[browseID] = &BrowseState{
		BrowseID:  browseID,
		GatewayID: gatewayID,
		DeviceID:  deviceID,
		Protocol:  protocol,
		Status:    "browsing",
		StartedAt: time.Now().UnixMilli(),
		cleanup:   cleanupBrowse,
	}
	m.browseMu.Unlock()

	// Subscribe to the browse result subject before sending the request.
	resultSubject := fmt.Sprintf("%s.browse.result.%s", protocol, browseID)
	m.log.Info("api: subscribing to browse result", "subject", resultSubject)
	resultSub, err := m.bus.Subscribe(resultSubject, func(_ string, data []byte, _ bus.ReplyFunc) {
		m.log.Info("api: received browse result", "subject", resultSubject, "bytes", len(data))

		// Transform scanner result into frontend BrowseCache shape.
		cacheJSON := transformBrowseResult(data, deviceID, protocol)

		m.browseMu.Lock()
		state := m.browseStates[browseID]
		if state == nil || state.Status != "browsing" {
			m.browseMu.Unlock()
			return // cancelled or already terminal
		}
		m.browseCache[cacheKey] = cacheJSON
		state.Status = "completed"
		state.Result = cacheJSON
		m.browseMu.Unlock()

		// Persist to KV so cache survives restart.
		if _, err := m.bus.KVPut(topics.BucketBrowseCache, cacheKey, cacheJSON); err != nil {
			m.log.Warn("failed to persist browse cache to KV", "key", cacheKey, "error", err)
		}

		// Notify the mqtt bridge so the cache rides out as a Sparkplug
		// _meta/browse metric (DDATA now, included in next DBIRTH). Mantle's
		// sparkplug-host picks it up like any other metric and routes it to
		// its own KV — that's the cross-network half of the cache pipeline.
		// Cache lives at api layer regardless of whether mqtt is loaded;
		// publish is fire-and-forget so missing subscribers cost nothing.
		updatePayload, mErr := json.Marshal(topics.BrowseCacheUpdate{
			DeviceID:  deviceID,
			Cache:     json.RawMessage(cacheJSON),
			Timestamp: time.Now().UnixMilli(),
		})
		if mErr == nil {
			if err := m.bus.Publish(topics.MqttBrowseCache, updatePayload); err != nil {
				m.log.Warn("failed to publish browse cache update", "device", deviceID, "error", err)
			}
		}

		go cleanupBrowse()
	})
	if err != nil {
		return "", deviceID, fmt.Errorf("failed to subscribe to browse result: %w", err)
	}
	subs = append(subs, resultSub)

	// Subscribe to progress to detect failures.
	progressSubject := fmt.Sprintf("%s.browse.progress.%s", protocol, browseID)
	progressSub, err := m.bus.Subscribe(progressSubject, func(_ string, data []byte, _ bus.ReplyFunc) {
		var progress struct {
			Phase string `json:"phase"`
		}
		if err := json.Unmarshal(data, &progress); err != nil {
			return
		}
		if progress.Phase == "failed" {
			finishBrowse("failed", nil)
		}
	})
	if err == nil {
		subs = append(subs, progressSub)
	}

	// Send the browse request to the scanner (scanner replies immediately with browseId).
	_, err = m.bus.Request(topics.Browse(protocol), enrichedBody, 10*time.Second)
	if err != nil {
		cleanupBrowse()
		return "", deviceID, fmt.Errorf("browse request failed: %w", err)
	}

	return browseID, deviceID, nil
}

// transformBrowseResult converts a scanner BrowseResult into the frontend BrowseCache shape.
func transformBrowseResult(raw []byte, deviceID, protocol string) json.RawMessage {
	// Parse the scanner response generically (supports EtherNet/IP, SNMP, and network shapes).
	var scanner struct {
		Variables  []json.RawMessage          `json:"variables"`
		OIDs       []json.RawMessage          `json:"oids"`
		Udts       map[string]json.RawMessage `json:"udts"`
		StructTags map[string]string          `json:"structTags"`
		Instances  []json.RawMessage          `json:"instances"` // network: discovered UDT instances
	}
	if err := json.Unmarshal(raw, &scanner); err != nil {
		return raw // fallback to raw
	}

	// Transform variables/oids → items
	type browseCacheItem struct {
		Tag          string      `json:"tag"`
		Name         string      `json:"name"`
		Datatype     string      `json:"datatype"`
		Value        interface{} `json:"value"`
		ProtocolType string      `json:"protocolType"`
	}

	// EtherNet/IP / OPC UA: uses "variables" with variableId
	items := make([]browseCacheItem, 0, len(scanner.Variables)+len(scanner.OIDs))
	for _, v := range scanner.Variables {
		var vi struct {
			VariableID string      `json:"variableId"`
			Datatype   string      `json:"datatype"`
			Value      interface{} `json:"value"`
			CipType    string      `json:"cipType"`
		}
		if err := json.Unmarshal(v, &vi); err != nil {
			continue
		}
		items = append(items, browseCacheItem{
			Tag:          vi.VariableID,
			Name:         vi.VariableID,
			Datatype:     vi.Datatype,
			Value:        vi.Value,
			ProtocolType: vi.CipType,
		})
	}

	// SNMP: uses "oids" with oid/name/snmpType
	for _, o := range scanner.OIDs {
		var oi struct {
			OID      string      `json:"oid"`
			Name     string      `json:"name"`
			Datatype string      `json:"datatype"`
			Value    interface{} `json:"value"`
			SnmpType string      `json:"snmpType"`
		}
		if err := json.Unmarshal(o, &oi); err != nil {
			continue
		}
		tag := oi.OID
		name := oi.Name
		if name == "" {
			name = oi.OID
		}
		items = append(items, browseCacheItem{
			Tag:          tag,
			Name:         name,
			Datatype:     oi.Datatype,
			Value:        oi.Value,
			ProtocolType: oi.SnmpType,
		})
	}

	// Convert udts map → array (member shape already matches frontend)
	udts := make([]json.RawMessage, 0, len(scanner.Udts))
	for _, u := range scanner.Udts {
		udts = append(udts, u)
	}

	structTags := scanner.StructTags
	if structTags == nil {
		structTags = make(map[string]string)
	}

	// Include instances for protocols that support auto-discovery (e.g. network)
	instances := scanner.Instances
	if instances == nil {
		instances = make([]json.RawMessage, 0)
	}

	cache := struct {
		DeviceID   string            `json:"deviceId"`
		Protocol   string            `json:"protocol"`
		Items      []browseCacheItem `json:"items"`
		Udts       []json.RawMessage `json:"udts"`
		StructTags map[string]string `json:"structTags"`
		Instances  []json.RawMessage `json:"instances"`
		CachedAt   string            `json:"cachedAt"`
	}{
		DeviceID:   deviceID,
		Protocol:   protocol,
		Items:      items,
		Udts:       udts,
		StructTags: structTags,
		Instances:  instances,
		CachedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	result, err := json.Marshal(cache)
	if err != nil {
		return raw
	}
	return result
}

// handleStreamGatewayBrowseProgress streams gateway browse progress events via SSE.
// GET /api/v1/gateways/{gatewayId}/browse/{browseId}/progress
//
// Resilience: NATS pub/sub doesn't replay, so if the SSE was being torn down /
// re-opened during the brief window when the scanner emits its terminal
// "completed" progress event, the client could hang forever waiting for a
// terminal phase. We harden two ways:
//  1. On connect, if BrowseState is already terminal (the result subject
//     already fired and api marked it completed/failed/cancelled), emit a
//     synthetic terminal event immediately so a late-connecting client
//     transitions instead of staring at the last "browsing" tick.
//  2. Also subscribe to the result subject during the SSE: when the scanner
//     publishes its result, synthesize a terminal so the client doesn't depend
//     on having received the scanner's progress-channel terminal.
func (m *Module) handleStreamGatewayBrowseProgress(w http.ResponseWriter, r *http.Request) {
	browseID := chi.URLParam(r, "browseId")

	sse, ok := newSSEWriter(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Subscribe with wildcard to catch any protocol's browse progress.
	subject := fmt.Sprintf("*.browse.progress.%s", browseID)
	sub, err := m.bus.Subscribe(subject, func(_ string, data []byte, _ bus.ReplyFunc) {
		sse.WriteEvent("progress", json.RawMessage(data))
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to subscribe: "+err.Error())
		return
	}
	defer sub.Unsubscribe()

	emitSynthetic := func(phase, deviceID, message string) {
		msg, _ := json.Marshal(map[string]interface{}{
			"browseId":  browseID,
			"deviceId":  deviceID,
			"phase":     phase,
			"message":   message,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
		sse.WriteEvent("progress", json.RawMessage(msg))
	}

	// (1) If the browse already terminated, synthesize a terminal event right
	// away so the client doesn't sit on a stale "browsing" message.
	m.browseMu.RLock()
	state := m.browseStates[browseID]
	var alreadyTerminal, terminalDevice string
	if state != nil && state.Status != "browsing" {
		alreadyTerminal = state.Status
		terminalDevice = state.DeviceID
	}
	m.browseMu.RUnlock()
	if alreadyTerminal != "" {
		emitSynthetic(alreadyTerminal, terminalDevice, "Browse "+alreadyTerminal)
		return
	}

	// (2) If the scanner publishes the result while we're connected, that's
	// terminal regardless of whether the progress-channel terminal made it.
	resultSubject := fmt.Sprintf("*.browse.result.%s", browseID)
	resSub, err := m.bus.Subscribe(resultSubject, func(_ string, _ []byte, _ bus.ReplyFunc) {
		m.browseMu.RLock()
		s := m.browseStates[browseID]
		var dev string
		if s != nil {
			dev = s.DeviceID
		}
		m.browseMu.RUnlock()
		emitSynthetic("completed", dev, "Browse complete")
	})
	if err == nil {
		defer resSub.Unsubscribe()
	}

	<-r.Context().Done()
}

// handleScannerSubscribe subscribes to scanner data for a protocol.
// POST /api/v1/scanner/{protocol}/subscribe
func (m *Module) handleScannerSubscribe(w http.ResponseWriter, r *http.Request) {
	protocol := chi.URLParam(r, "protocol")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	resp, err := m.bus.Request(topics.ScannerSubscribe(protocol), body, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scanner subscribe failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleScannerUnsubscribe unsubscribes from scanner data for a protocol.
// POST /api/v1/scanner/{protocol}/unsubscribe
func (m *Module) handleScannerUnsubscribe(w http.ResponseWriter, r *http.Request) {
	protocol := chi.URLParam(r, "protocol")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	resp, err := m.bus.Request(topics.ScannerUnsubscribe(protocol), body, busTimeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "scanner unsubscribe failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(resp)
}

// handleCancelGatewayBrowse cancels an in-progress browse operation.
// POST /api/v1/gateways/{gatewayId}/browse/{browseId}/cancel
func (m *Module) handleCancelGatewayBrowse(w http.ResponseWriter, r *http.Request) {
	browseID := chi.URLParam(r, "browseId")
	m.log.Info("api: cancel browse requested", "browseId", browseID)

	m.browseMu.Lock()
	state := m.browseStates[browseID]
	if state == nil {
		m.browseMu.Unlock()
		m.log.Warn("api: cancel browse: not found", "browseId", browseID)
		writeError(w, http.StatusNotFound, "browse not found")
		return
	}
	if state.Status != "browsing" {
		m.browseMu.Unlock()
		m.log.Info("api: cancel browse: already terminal", "browseId", browseID, "status", state.Status)
		writeJSON(w, http.StatusOK, map[string]string{"status": state.Status})
		return
	}
	state.Status = "cancelled"
	protocol := state.Protocol
	cleanup := state.cleanup
	m.browseMu.Unlock()

	// Tell the scanner to stop the browse.
	cancelSubject := topics.BrowseCancel(protocol, browseID)
	m.log.Info("api: publishing browse cancel", "subject", cancelSubject)
	if err := m.bus.Publish(cancelSubject, []byte(`{}`)); err != nil {
		m.log.Error("api: failed to publish browse cancel", "subject", cancelSubject, "error", err)
	}

	if cleanup != nil {
		go cleanup()
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}
