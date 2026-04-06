//go:build opcua || all

package opcua

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/monitor"
	"github.com/gopcua/opcua/ua"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/types"
)

// OpcUaConnection holds a connection to a single OPC UA device.
type OpcUaConnection struct {
	DeviceID            string
	EndpointURL         string
	SecurityPolicy      string
	SecurityMode        string
	Auth                OpcUaAuth
	Client              *opcua.Client
	NodeMonitor         *monitor.NodeMonitor
	MonitorSub          *monitor.Subscription // active subscription on the NodeMonitor
	Variables           map[string]*CachedVariable
	ConnectionState     string // "disconnected", "connecting", "connected"
	ConsecutiveFailures int
	LastConnectAttempt  time.Time
	cancel              context.CancelFunc
}

// DeviceSubscription tracks a subscriber's interest in specific nodes.
type DeviceSubscription struct {
	SubscriberID string
	NodeIDs      map[string]bool
	ScanRate     int
}

// Scanner manages OPC UA connections and bus request handlers.
type Scanner struct {
	b               bus.Bus
	moduleID        string
	connections     map[string]*OpcUaConnection               // deviceID -> connection
	subscribers     map[string]map[string]*DeviceSubscription  // deviceID -> subscriberID -> sub
	certFile        string
	keyFile         string
	pkiDir          string
	autoAcceptCerts bool
	enabled         atomic.Bool
	mu              sync.RWMutex
	subs            []bus.Subscription
}

// NewScanner creates a new scanner instance.
func NewScanner(b bus.Bus, moduleID, certFile, keyFile, pkiDir string, autoAcceptCerts bool) *Scanner {
	s := &Scanner{
		b:               b,
		moduleID:        moduleID,
		connections:     make(map[string]*OpcUaConnection),
		subscribers:     make(map[string]map[string]*DeviceSubscription),
		certFile:        certFile,
		keyFile:         keyFile,
		pkiDir:          pkiDir,
		autoAcceptCerts: autoAcceptCerts,
	}
	s.enabled.Store(true)
	return s
}

// IsEnabled returns whether the scanner is enabled.
func (s *Scanner) IsEnabled() bool {
	return s.enabled.Load()
}

// SetEnabled enables or disables the scanner.
// When disabled, data change callbacks skip publishing but connections are preserved.
func (s *Scanner) SetEnabled(enabled bool) {
	was := s.enabled.Swap(enabled)
	if was != enabled {
		if enabled {
			slog.Info("opcua: scanner ENABLED — resuming publishing")
		} else {
			slog.Info("opcua: scanner DISABLED — pausing publishing (connections preserved)")
		}
	}
}

// Start begins listening for bus requests.
func (s *Scanner) Start() {
	slog.Info("opcua: starting scanner (stateless, subscriber-driven)")
	s.startRequestHandlers()
	slog.Info("opcua: scanner started, waiting for subscribe/browse requests")
}

// Stop shuts down all connections and bus subscriptions.
func (s *Scanner) Stop() {
	slog.Info("opcua: stopping scanner")

	for _, sub := range s.subs {
		_ = sub.Unsubscribe()
	}
	s.subs = nil

	s.mu.Lock()
	for deviceID, conn := range s.connections {
		s.disconnectDevice(conn)
		slog.Info("opcua: disconnected device", "deviceId", deviceID)
	}
	s.connections = make(map[string]*OpcUaConnection)
	s.subscribers = make(map[string]map[string]*DeviceSubscription)
	s.mu.Unlock()

	slog.Info("opcua: scanner stopped")
}

// connectDevice establishes or reuses a connection to an OPC UA server.
func (s *Scanner) connectDevice(
	deviceID, endpointURL string,
	auth *OpcUaAuth,
	securityPolicy, securityMode string,
) (*OpcUaConnection, error) {
	s.mu.Lock()
	conn, exists := s.connections[deviceID]
	if exists && conn.Client != nil && conn.ConnectionState == "connected" {
		s.mu.Unlock()
		return conn, nil
	}

	if !exists {
		authVal := OpcUaAuth{Type: "anonymous"}
		if auth != nil {
			authVal = *auth
		}
		conn = &OpcUaConnection{
			DeviceID:       deviceID,
			EndpointURL:    endpointURL,
			SecurityPolicy: securityPolicy,
			SecurityMode:   securityMode,
			Auth:           authVal,
			Variables:      make(map[string]*CachedVariable),
		}
		s.connections[deviceID] = conn
	}
	conn.ConnectionState = "connecting"
	conn.LastConnectAttempt = time.Now()
	s.mu.Unlock()

	slog.Info("opcua: connecting to device", "deviceId", deviceID, "endpointUrl", endpointURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Discover endpoints
	endpoints, err := opcua.GetEndpoints(ctx, endpointURL)
	if err != nil {
		s.mu.Lock()
		conn.ConnectionState = "disconnected"
		conn.ConsecutiveFailures++
		s.mu.Unlock()
		return nil, fmt.Errorf("get endpoints: %w", err)
	}

	// Select endpoint based on security policy
	ep := selectEndpoint(endpoints, securityPolicy, securityMode)
	if ep == nil {
		s.mu.Lock()
		conn.ConnectionState = "disconnected"
		conn.ConsecutiveFailures++
		s.mu.Unlock()
		return nil, fmt.Errorf("no matching endpoint for policy=%s mode=%s", securityPolicy, securityMode)
	}

	slog.Info("opcua: selected endpoint",
		"deviceId", deviceID,
		"securityPolicy", ep.SecurityPolicyURI,
		"securityMode", securityModeStr(ep.SecurityMode),
		"endpointUrl", ep.EndpointURL,
	)

	// Auth options
	authType := "anonymous"
	if auth != nil {
		authType = auth.Type
	}

	var userTokenType ua.UserTokenType
	switch authType {
	case "username":
		userTokenType = ua.UserTokenTypeUserName
	case "certificate":
		userTokenType = ua.UserTokenTypeCertificate
	default:
		userTokenType = ua.UserTokenTypeAnonymous
	}

	// Build client options
	opts := []opcua.Option{
		opcua.SecurityFromEndpoint(ep, userTokenType),
		opcua.ApplicationURI("urn:tentacle-opcua"),
		opcua.AutoReconnect(true),
		opcua.ReconnectInterval(10 * time.Second),
	}

	if authType == "username" {
		opts = append(opts, opcua.AuthUsername(auth.Username, auth.Password))
	}

	// Add certificate if security is not None
	if ep.SecurityPolicyURI != ua.SecurityPolicyURINone {
		opts = append(opts,
			opcua.CertificateFile(s.certFile),
			opcua.PrivateKeyFile(s.keyFile),
		)
	}

	// Use the original endpointUrl if the server's advertised URL differs
	// (server may advertise an internal hostname we can't reach)
	connectURL := ep.EndpointURL
	if ep.EndpointURL != endpointURL {
		slog.Info("opcua: server advertised different URL, using original",
			"deviceId", deviceID, "advertised", ep.EndpointURL, "using", endpointURL)
		connectURL = endpointURL
	}

	client, err := opcua.NewClient(connectURL, opts...)
	if err != nil {
		s.mu.Lock()
		conn.ConnectionState = "disconnected"
		conn.ConsecutiveFailures++
		s.mu.Unlock()
		return nil, fmt.Errorf("create client: %w", err)
	}

	connectCtx, connectCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer connectCancel()

	if err := client.Connect(connectCtx); err != nil {
		s.mu.Lock()
		conn.ConnectionState = "disconnected"
		conn.ConsecutiveFailures++
		s.mu.Unlock()
		return nil, fmt.Errorf("connect: %w", err)
	}

	s.mu.Lock()
	conn.Client = client
	conn.ConnectionState = "connected"
	conn.ConsecutiveFailures = 0
	s.mu.Unlock()

	slog.Info("opcua: connected", "deviceId", deviceID)
	return conn, nil
}

func (s *Scanner) disconnectDevice(conn *OpcUaConnection) {
	if conn.cancel != nil {
		conn.cancel()
	}
	if conn.MonitorSub != nil {
		_ = conn.MonitorSub.Unsubscribe(context.Background())
		conn.MonitorSub = nil
	}
	conn.NodeMonitor = nil
	if conn.Client != nil {
		if err := conn.Client.Close(context.Background()); err != nil {
			slog.Debug("opcua: close error", "deviceId", conn.DeviceID, "error", err)
		}
		conn.Client = nil
	}
	conn.ConnectionState = "disconnected"
}

func (s *Scanner) removeConnection(deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	conn, ok := s.connections[deviceID]
	if !ok {
		return
	}
	s.disconnectDevice(conn)
	delete(s.connections, deviceID)
	slog.Info("opcua: removed connection", "deviceId", deviceID)
}

func selectEndpoint(endpoints []*ua.EndpointDescription, policy, mode string) *ua.EndpointDescription {
	policyLower := strings.ToLower(policy)

	policyURIs := map[string]string{
		"none":           ua.SecurityPolicyURINone,
		"basic128rsa15":  ua.SecurityPolicyURIBasic128Rsa15,
		"basic256":       ua.SecurityPolicyURIBasic256,
		"basic256sha256": ua.SecurityPolicyURIBasic256Sha256,
		"":               "", // any
	}

	targetURI := policyURIs[policyLower]

	modeLower := strings.ToLower(mode)
	var targetMode ua.MessageSecurityMode
	switch modeLower {
	case "sign":
		targetMode = ua.MessageSecurityModeSign
	case "signandencrypt":
		targetMode = ua.MessageSecurityModeSignAndEncrypt
	case "none", "":
		targetMode = ua.MessageSecurityModeNone
	}

	// If no policy specified, prefer the most secure endpoint
	if targetURI == "" {
		var best *ua.EndpointDescription
		for _, ep := range endpoints {
			if best == nil || ep.SecurityMode > best.SecurityMode {
				best = ep
			}
		}
		return best
	}

	// Find matching endpoint
	for _, ep := range endpoints {
		if ep.SecurityPolicyURI == targetURI {
			if targetMode == 0 || ep.SecurityMode == targetMode {
				return ep
			}
		}
	}

	// Fallback: any endpoint with the requested policy
	for _, ep := range endpoints {
		if ep.SecurityPolicyURI == targetURI {
			return ep
		}
	}

	return nil
}

func securityModeStr(mode ua.MessageSecurityMode) string {
	switch mode {
	case ua.MessageSecurityModeNone:
		return "None"
	case ua.MessageSecurityModeSign:
		return "Sign"
	case ua.MessageSecurityModeSignAndEncrypt:
		return "SignAndEncrypt"
	default:
		return fmt.Sprintf("Unknown(%d)", mode)
	}
}

func getBackoffDelay(failures int) time.Duration {
	base := 2 * time.Second
	maxDelay := 60 * time.Second
	delay := time.Duration(float64(base) * math.Pow(2, float64(failures)))
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

// getDeviceSubscribedNodeIDs returns the union of all subscribed node IDs for a device.
func (s *Scanner) getDeviceSubscribedNodeIDs(deviceID string) map[string]bool {
	nodeIDs := make(map[string]bool)
	subs, ok := s.subscribers[deviceID]
	if !ok {
		return nodeIDs
	}
	for _, sub := range subs {
		for nodeID := range sub.NodeIDs {
			nodeIDs[nodeID] = true
		}
	}
	return nodeIDs
}

func (s *Scanner) addSubscriber(deviceID, subscriberID string, nodeIDs []string, scanRate int) {
	if _, ok := s.subscribers[deviceID]; !ok {
		s.subscribers[deviceID] = make(map[string]*DeviceSubscription)
	}
	subs := s.subscribers[deviceID]

	existing, ok := subs[subscriberID]
	if ok {
		for _, nodeID := range nodeIDs {
			existing.NodeIDs[nodeID] = true
		}
		existing.ScanRate = scanRate
	} else {
		ids := make(map[string]bool)
		for _, nodeID := range nodeIDs {
			ids[nodeID] = true
		}
		subs[subscriberID] = &DeviceSubscription{
			SubscriberID: subscriberID,
			NodeIDs:      ids,
			ScanRate:     scanRate,
		}
	}

	slog.Info("opcua: subscribed nodeIds",
		"count", len(nodeIDs), "subscriber", subscriberID,
		"device", deviceID, "totalSubscribers", len(subs))
}

// removeSubscriber removes nodeIDs for a subscriber. Returns true if zero subscribers remain.
func (s *Scanner) removeSubscriber(deviceID, subscriberID string, nodeIDs []string) bool {
	subs, ok := s.subscribers[deviceID]
	if !ok {
		return true
	}

	existing, ok := subs[subscriberID]
	if ok {
		for _, nodeID := range nodeIDs {
			delete(existing.NodeIDs, nodeID)
		}
		if len(existing.NodeIDs) == 0 {
			delete(subs, subscriberID)
		}
	}

	if len(subs) == 0 {
		delete(s.subscribers, deviceID)
		return true
	}
	return false
}

// publishValue publishes a data change to the bus.
func (s *Scanner) publishValue(conn *OpcUaConnection, nodeID string, value interface{}, datatype, quality string) {
	if value == nil {
		return
	}

	now := time.Now().UnixMilli()

	// Update cached variable regardless of enabled state
	if cached, ok := conn.Variables[nodeID]; ok {
		cached.Value = value
		cached.Quality = quality
		cached.LastUpdated = now
	}

	// Skip publishing when disabled
	if !s.enabled.Load() {
		return
	}

	description := nodeID
	if cached, ok := conn.Variables[nodeID]; ok && cached.DisplayName != "" {
		description = cached.DisplayName
	}

	msg := types.PlcDataMessage{
		ModuleID:    s.moduleID,
		DeviceID:    conn.DeviceID,
		VariableID:  nodeID,
		Value:       value,
		Timestamp:   now,
		Datatype:    datatype,
		Description: description,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("opcua: failed to marshal data message", "error", err)
		return
	}

	sanitizedNodeID := sanitizeNodeIDForSubject(nodeID)
	subject := fmt.Sprintf("%s.data.%s.%s", s.moduleID, types.SanitizeForSubject(conn.DeviceID), sanitizedNodeID)
	_ = s.b.Publish(subject, data)
}

func (s *Scanner) publishBrowseProgress(browseID, deviceID, phase string, totalTags, completedTags, errorCount int, message string) {
	msg := types.BrowseProgressMessage{
		BrowseID:      browseID,
		ModuleID:      s.moduleID,
		DeviceID:      deviceID,
		Phase:         phase,
		TotalTags:     totalTags,
		CompletedTags: completedTags,
		ErrorCount:    errorCount,
		Message:       message,
		Timestamp:     time.Now().UnixMilli(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	subject := fmt.Sprintf("%s.browse.progress.%s", s.moduleID, browseID)
	_ = s.b.Publish(subject, data)
}

// browseDevice connects (if necessary), browses the address space, and returns
// the discovered variables.
func (s *Scanner) browseDevice(
	deviceID, endpointURL string,
	auth *OpcUaAuth,
	securityPolicy, securityMode string,
	startNodeID, browseID string,
	maxDepth int,
) []VariableInfo {
	s.mu.RLock()
	conn, exists := s.connections[deviceID]
	hasSession := exists && conn.Client != nil && conn.ConnectionState == "connected"
	s.mu.RUnlock()

	tempConnection := false

	if !hasSession {
		if browseID != "" {
			s.publishBrowseProgress(browseID, deviceID, "discovering", 0, 0, 0, "Connecting to OPC UA server...")
		}

		var err error
		conn, err = s.connectDevice(deviceID, endpointURL, auth, securityPolicy, securityMode)
		if err != nil {
			slog.Error("opcua: failed to connect for browse", "deviceId", deviceID, "error", err)
			if browseID != "" {
				s.publishBrowseProgress(browseID, deviceID, "failed", 0, 0, 1,
					fmt.Sprintf("Connection failed: %v", err))
			}
			return nil
		}

		s.mu.RLock()
		_, hasSubs := s.subscribers[deviceID]
		s.mu.RUnlock()
		tempConnection = !hasSubs
	}

	if conn == nil || conn.Client == nil {
		slog.Error("opcua: no client for device", "deviceId", deviceID)
		return nil
	}

	if browseID != "" {
		s.publishBrowseProgress(browseID, deviceID, "discovering", 0, 0, 0, "Browsing address space...")
	}

	if startNodeID == "" {
		startNodeID = "i=85" // Objects folder
	}
	if maxDepth <= 0 {
		maxDepth = 10
	}

	var progressFn BrowseProgressFunc
	if browseID != "" {
		progressFn = func(total int, nodeID string, message string) {
			s.publishBrowseProgress(browseID, deviceID, "discovering", total, total, 0, message)
		}
	}

	ctx := context.Background()
	browseResults, err := browseAddressSpace(ctx, conn.Client, startNodeID, maxDepth, progressFn)
	if err != nil {
		slog.Error("opcua: browse failed", "deviceId", deviceID, "error", err)
		if browseID != "" {
			s.publishBrowseProgress(browseID, deviceID, "failed", 0, 0, 1,
				fmt.Sprintf("Browse failed: %v", err))
		}
		return nil
	}

	// Cache discovered variables
	s.mu.Lock()
	for _, v := range browseResults {
		if _, exists := conn.Variables[v.NodeID]; !exists {
			conn.Variables[v.NodeID] = &CachedVariable{
				NodeID:        v.NodeID,
				DisplayName:   v.DisplayName,
				Datatype:      v.Datatype,
				OpcuaDatatype: v.OpcuaDatatype,
				Value:         nil,
				Quality:       "unknown",
				LastUpdated:   0,
			}
		}
	}
	s.mu.Unlock()

	// Disconnect temp connection but keep cached variables
	if tempConnection && conn.Client != nil {
		s.mu.Lock()
		if conn.cancel != nil {
			conn.cancel()
		}
		if conn.MonitorSub != nil {
			_ = conn.MonitorSub.Unsubscribe(context.Background())
			conn.MonitorSub = nil
		}
		conn.NodeMonitor = nil
		if conn.Client != nil {
			_ = conn.Client.Close(context.Background())
			conn.Client = nil
		}
		conn.ConnectionState = "disconnected"
		s.mu.Unlock()
	}

	if browseID != "" {
		s.publishBrowseProgress(browseID, deviceID, "completed", len(browseResults), len(browseResults), 0,
			fmt.Sprintf("Browse complete: %d variables", len(browseResults)))
	}

	// Build response
	results := make([]VariableInfo, len(browseResults))
	for i, v := range browseResults {
		results[i] = VariableInfo{
			ModuleID:      s.moduleID,
			DeviceID:      deviceID,
			VariableID:    v.NodeID,
			DisplayName:   v.DisplayName,
			Value:         nil,
			Datatype:      v.Datatype,
			OpcuaDatatype: v.OpcuaDatatype,
			Quality:       "unknown",
			Origin:        "opcua",
			LastUpdated:   0,
		}
	}

	return results
}

// startRequestHandlers subscribes to all bus request subjects.
func (s *Scanner) startRequestHandlers() {
	subscribe := func(subject string, handler bus.MessageHandler) {
		sub, err := s.b.Subscribe(subject, handler)
		if err != nil {
			slog.Error("opcua: failed to subscribe", "subject", subject, "error", err)
			return
		}
		s.subs = append(s.subs, sub)
		slog.Info("opcua: listening", "subject", subject)
	}

	subscribe(s.moduleID+".variables", s.handleVariables)
	subscribe(s.moduleID+".browse", s.handleBrowse)
	subscribe(s.moduleID+".subscribe", s.handleSubscribe)
	subscribe(s.moduleID+".unsubscribe", s.handleUnsubscribe)
	subscribe(s.moduleID+".command.>", s.handleWriteCommand)
}

func (s *Scanner) handleVariables(subject string, data []byte, reply bus.ReplyFunc) {
	s.mu.RLock()
	var allVars []VariableInfo
	for deviceID, conn := range s.connections {
		for _, cached := range conn.Variables {
			allVars = append(allVars, VariableInfo{
				ModuleID:      s.moduleID,
				DeviceID:      deviceID,
				VariableID:    cached.NodeID,
				DisplayName:   cached.DisplayName,
				Value:         cached.Value,
				Datatype:      cached.Datatype,
				OpcuaDatatype: cached.OpcuaDatatype,
				Quality:       cached.Quality,
				Origin:        "opcua",
				LastUpdated:   cached.LastUpdated,
			})
		}
	}
	s.mu.RUnlock()

	slog.Info("opcua: variables request", "count", len(allVars))
	s.respondJSON(reply, allVars)
}

func (s *Scanner) handleBrowse(subject string, data []byte, reply bus.ReplyFunc) {
	var req BrowseRequest
	if len(data) > 0 {
		if err := json.Unmarshal(data, &req); err != nil {
			slog.Error("opcua: invalid browse request", "error", err)
			s.respondJSON(reply, []VariableInfo{})
			return
		}
	}

	if req.DeviceID == "" || req.EndpointURL == "" {
		s.respondJSON(reply, map[string]string{
			"error": "Browse requires deviceId and endpointUrl",
		})
		return
	}

	browseID := req.BrowseID
	if browseID == "" && req.Async {
		browseID = uuid.New().String()
	}

	if req.Async && browseID != "" {
		slog.Info("opcua: browse request (async)",
			"deviceId", req.DeviceID, "endpointUrl", req.EndpointURL, "browseId", browseID)

		// Reply immediately with browseId
		s.respondJSON(reply, map[string]string{"browseId": browseID})

		// Run browse in background
		go func() {
			results := s.browseDevice(
				req.DeviceID, req.EndpointURL,
				req.Auth, req.SecurityPolicy, req.SecurityMode,
				req.StartNodeID, browseID, req.MaxDepth,
			)
			s.publishBrowseProgress(browseID, "_all", "completed", len(results), len(results), 0,
				fmt.Sprintf("Browse complete: %d total variables", len(results)))
		}()
		return
	}

	// Synchronous browse
	results := s.browseDevice(
		req.DeviceID, req.EndpointURL,
		req.Auth, req.SecurityPolicy, req.SecurityMode,
		req.StartNodeID, browseID, req.MaxDepth,
	)

	slog.Info("opcua: browse request complete", "count", len(results))
	s.respondJSON(reply, results)
}

func (s *Scanner) handleSubscribe(subject string, data []byte, reply bus.ReplyFunc) {
	var req SubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		slog.Error("opcua: invalid subscribe request", "error", err)
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	if req.DeviceID == "" || req.EndpointURL == "" || len(req.NodeIDs) == 0 || req.SubscriberID == "" {
		s.respondJSON(reply, map[string]interface{}{
			"success": false,
			"error":   "Subscribe requires deviceId, endpointUrl, nodeIds, and subscriberId",
		})
		return
	}

	scanRate := req.ScanRate
	if scanRate <= 0 {
		scanRate = 1000
	}

	// Track subscriber
	s.mu.Lock()
	s.addSubscriber(req.DeviceID, req.SubscriberID, req.NodeIDs, scanRate)
	s.mu.Unlock()

	// Connect if needed
	conn, err := s.connectDevice(req.DeviceID, req.EndpointURL, req.Auth, req.SecurityPolicy, req.SecurityMode)
	if err != nil {
		s.respondJSON(reply, map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Connection failed: %v", err),
		})
		return
	}

	// Set up monitoring via gopcua's monitor package
	s.mu.Lock()
	if conn.NodeMonitor == nil && conn.Client != nil {
		ctx, cancel := context.WithCancel(context.Background())
		conn.cancel = cancel

		nm, err := monitor.NewNodeMonitor(conn.Client)
		if err != nil {
			s.mu.Unlock()
			slog.Error("opcua: failed to create node monitor", "error", err)
			s.respondJSON(reply, map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Monitor creation failed: %v", err),
			})
			return
		}
		conn.NodeMonitor = nm

		// Start the subscription channel reader
		ch := make(chan *monitor.DataChangeMessage, 256)
		monSub, err := nm.ChanSubscribe(
			ctx,
			&opcua.SubscriptionParameters{
				Interval: time.Duration(scanRate) * time.Millisecond,
			},
			ch,
			req.NodeIDs...,
		)
		if err != nil {
			s.mu.Unlock()
			slog.Error("opcua: failed to create subscription", "error", err)
			s.respondJSON(reply, map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Subscription failed: %v", err),
			})
			return
		}
		conn.MonitorSub = monSub

		// Ensure variable entries exist for all subscribed nodes
		for _, nodeID := range req.NodeIDs {
			if _, exists := conn.Variables[nodeID]; !exists {
				conn.Variables[nodeID] = &CachedVariable{
					NodeID:        nodeID,
					DisplayName:   nodeID,
					Datatype:      "string",
					OpcuaDatatype: "String",
					Value:         nil,
					Quality:       "unknown",
					LastUpdated:   0,
				}
			}
		}
		s.mu.Unlock()

		// Read data changes in a goroutine
		go func() {
			for dcm := range ch {
				if dcm.Error != nil {
					slog.Debug("opcua: data change error", "error", dcm.Error)
					continue
				}

				nodeID := dcm.NodeID.String()
				value := extractValue(dcm.Value)

				quality := "good"
				if dcm.Status != ua.StatusOK {
					quality = "bad"
				}

				s.mu.RLock()
				cached, ok := conn.Variables[nodeID]
				datatype := "string"
				if ok {
					datatype = cached.Datatype
					// Infer datatype from first value
					if datatype == "string" {
						switch value.(type) {
						case int64, uint64, float64:
							datatype = "number"
							cached.Datatype = "number"
						case bool:
							datatype = "boolean"
							cached.Datatype = "boolean"
						}
					}
				}
				s.mu.RUnlock()

				s.publishValue(conn, nodeID, value, datatype, quality)
			}
		}()
	} else if conn.NodeMonitor != nil {
		// Already have a monitor -- add new nodes to existing subscription
		for _, nodeID := range req.NodeIDs {
			if _, exists := conn.Variables[nodeID]; !exists {
				conn.Variables[nodeID] = &CachedVariable{
					NodeID:        nodeID,
					DisplayName:   nodeID,
					Datatype:      "string",
					OpcuaDatatype: "String",
					Value:         nil,
					Quality:       "unknown",
					LastUpdated:   0,
				}
			}
		}
		s.mu.Unlock()
	} else {
		s.mu.Unlock()
	}

	slog.Info("opcua: subscribe complete", "nodeIds", len(req.NodeIDs), "device", req.DeviceID)
	s.respondJSON(reply, map[string]interface{}{"success": true, "count": len(req.NodeIDs)})
}

func (s *Scanner) handleUnsubscribe(subject string, data []byte, reply bus.ReplyFunc) {
	var req UnsubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		slog.Error("opcua: invalid unsubscribe request", "error", err)
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	s.mu.Lock()
	zeroSubscribers := s.removeSubscriber(req.DeviceID, req.SubscriberID, req.NodeIDs)
	s.mu.Unlock()

	if zeroSubscribers {
		slog.Info("opcua: no subscribers remaining, closing connection", "device", req.DeviceID)
		s.removeConnection(req.DeviceID)
	}

	s.respondJSON(reply, map[string]interface{}{"success": true, "count": len(req.NodeIDs)})
}

func (s *Scanner) handleWriteCommand(subj string, data []byte, reply bus.ReplyFunc) {
	commandPrefix := s.moduleID + ".command."
	if !strings.HasPrefix(subj, commandPrefix) {
		return
	}

	variableID := subj[len(commandPrefix):]
	if variableID == "" {
		slog.Warn("opcua: write command with empty variableId", "subject", subj)
		return
	}

	valueStr := string(data)
	slog.Info("opcua: write command received", "variableId", variableID, "value", valueStr)

	// Find connection with this variable
	s.mu.RLock()
	var conn *OpcUaConnection
	for _, c := range s.connections {
		if _, ok := c.Variables[variableID]; ok {
			conn = c
			break
		}
	}
	s.mu.RUnlock()

	if conn == nil {
		slog.Warn("opcua: write failed, variable not found", "variableId", variableID)
		return
	}

	if conn.Client == nil || conn.ConnectionState != "connected" {
		slog.Warn("opcua: write failed, device not connected", "deviceId", conn.DeviceID)
		return
	}

	s.mu.RLock()
	cached := conn.Variables[variableID]
	var datatype string
	if cached != nil {
		datatype = cached.Datatype
	}
	s.mu.RUnlock()

	// Parse value based on datatype
	var writeValue interface{} = valueStr
	switch datatype {
	case "number":
		if v, err := strconv.ParseFloat(valueStr, 64); err == nil {
			writeValue = v
		}
	case "boolean":
		lower := strings.ToLower(valueStr)
		writeValue = lower == "true" || lower == "1" || lower == "on" || lower == "yes"
	}

	// Write to OPC UA
	parsedID, err := ua.ParseNodeID(variableID)
	if err != nil {
		slog.Error("opcua: invalid NodeID for write", "variableId", variableID, "error", err)
		return
	}

	variant, err := ua.NewVariant(writeValue)
	if err != nil {
		slog.Error("opcua: failed to create variant for write", "error", err)
		return
	}

	writeReq := &ua.WriteRequest{
		NodesToWrite: []*ua.WriteValue{
			{
				NodeID:      parsedID,
				AttributeID: ua.AttributeIDValue,
				Value: &ua.DataValue{
					EncodingMask: ua.DataValueValue,
					Value:        variant,
				},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := conn.Client.Write(ctx, writeReq)
	if err != nil {
		slog.Error("opcua: write error", "variableId", variableID, "error", err)
		return
	}

	if len(resp.Results) > 0 && resp.Results[0] != ua.StatusOK {
		slog.Error("opcua: write failed", "variableId", variableID, "status", resp.Results[0])
	} else {
		slog.Info("opcua: write successful", "variableId", variableID, "value", writeValue)
	}
}

// respondJSON marshals v and calls the reply function.
func (s *Scanner) respondJSON(reply bus.ReplyFunc, v interface{}) {
	if reply == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("opcua: failed to marshal response", "error", err)
		return
	}
	if err := reply(data); err != nil {
		slog.Error("opcua: failed to respond", "error", err)
	}
}
