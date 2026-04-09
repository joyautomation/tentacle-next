//go:build snmp || all

package snmp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gosnmp/gosnmp"
	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

// DeviceConnection holds the state for a connected SNMP device.
type DeviceConnection struct {
	DeviceID    string
	Host        string
	Port        int
	Version     gosnmp.SnmpVersion
	Community   string
	V3Auth      *V3Auth
	Client      *gosnmp.GoSNMP
	Variables   map[string]*CachedVar        // OID -> cached value
	Subscribers map[string]map[string]bool    // subscriberID -> set of OIDs
	ScanRate    time.Duration
	UseBulkGet  bool
	stopChan    chan struct{}
	mu          sync.RWMutex
}

// CachedVar holds the cached state of a single OID.
type CachedVar struct {
	OID      string
	SnmpType string      // "Integer32", "OctetString", etc.
	Datatype string      // "number", "string", "boolean"
	Value    interface{}
	Quality  string
	LastRead int64
}

// Scanner manages SNMP device connections and Bus request handling.
type Scanner struct {
	b           bus.Bus
	moduleID    string
	connections map[string]*DeviceConnection
	mibTree     *MibTree
	enabled     atomic.Bool
	mu          sync.RWMutex
	log         *slog.Logger

	subs []bus.Subscription
}

// NewScanner creates a new SNMP scanner.
func NewScanner(b bus.Bus, moduleID string, mibTree *MibTree, log *slog.Logger) *Scanner {
	s := &Scanner{
		b:           b,
		moduleID:    moduleID,
		connections: make(map[string]*DeviceConnection),
		mibTree:     mibTree,
		log:         log,
	}
	s.enabled.Store(true) // enabled by default
	return s
}

// IsEnabled returns whether the scanner is enabled.
func (s *Scanner) IsEnabled() bool {
	return s.enabled.Load()
}

// SetEnabled enables or disables the scanner.
// When disabled, polling loops skip their work but connections are preserved.
func (s *Scanner) SetEnabled(enabled bool) {
	was := s.enabled.Swap(enabled)
	if was != enabled {
		if enabled {
			s.log.Info("snmp: scanner ENABLED — resuming polling")
		} else {
			s.log.Info("snmp: scanner DISABLED — pausing polling (connections preserved)")
		}
	}
}

// Start subscribes to all Bus request subjects.
func (s *Scanner) Start() {
	subscribe := func(subject string, handler bus.MessageHandler) {
		sub, err := s.b.Subscribe(subject, handler)
		if err != nil {
			s.log.Error("snmp: failed to subscribe", "subject", subject, "error", err)
			return
		}
		s.subs = append(s.subs, sub)
	}

	subscribe(topics.Browse("snmp"), s.handleBrowse)
	subscribe(topics.ScannerSubscribe("snmp"), s.handleSubscribe)
	subscribe(topics.ScannerUnsubscribe("snmp"), s.handleUnsubscribe)
	subscribe(topics.ScannerVariables("snmp"), s.handleVariables)
	subscribe(topics.SnmpSet, s.handleSet)

	// Watch scanner config KV bucket for subscription configs.
	bucket := topics.BucketScannerSNMP
	if err := s.b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
		s.log.Warn("snmp: failed to create scanner config bucket", "error", err)
	}
	kvSub, err := s.b.KVWatchAll(bucket, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpPut {
			s.handleSubscribe("", value, nil)
		} else if op == bus.KVOpDelete {
			parts := strings.SplitN(key, ".", 2)
			if len(parts) == 2 {
				unsubReq, _ := json.Marshal(UnsubscribeRequest{SubscriberID: parts[0], DeviceID: parts[1]})
				s.handleUnsubscribe("", unsubReq, nil)
			}
		}
	})
	if err != nil {
		s.log.Error("snmp: failed to watch scanner config bucket", "error", err)
	} else {
		s.subs = append(s.subs, kvSub)
	}

	s.log.Info("snmp: listening for browse/subscribe/unsubscribe/variables/set requests")
}

// ActiveDevices returns info about all currently connected devices.
func (s *Scanner) ActiveDevices() []ActiveDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()

	devices := make([]ActiveDevice, 0, len(s.connections))
	for _, conn := range s.connections {
		conn.mu.RLock()
		devices = append(devices, ActiveDevice{
			DeviceID: conn.DeviceID,
			Host:     conn.Host,
			Port:     conn.Port,
			OidCount: len(conn.Variables),
			Version:  versionToString(conn.Version),
		})
		conn.mu.RUnlock()
	}
	return devices
}

// Stop unsubscribes and closes all connections.
func (s *Scanner) Stop() {
	for _, sub := range s.subs {
		_ = sub.Unsubscribe()
	}
	s.subs = nil

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.connections {
		close(conn.stopChan)
		if conn.Client != nil {
			_ = conn.Client.Conn.Close()
		}
	}
	s.connections = make(map[string]*DeviceConnection)
	s.log.Info("snmp: all connections closed")
}

// =========================================================================
// Browse handler
// =========================================================================

func (s *Scanner) handleBrowse(subject string, data []byte, reply bus.ReplyFunc) {
	var req BrowseRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.respondJSON(reply, BrowseResult{OIDs: []OidInfo{}})
		return
	}

	if req.DeviceID == "" || req.Host == "" {
		s.respondJSON(reply, BrowseResult{OIDs: []OidInfo{}})
		return
	}

	if req.Port == 0 {
		req.Port = 161
	}
	if req.RootOID == "" {
		req.RootOID = ".1.3.6.1"
	}
	if req.Version == "" {
		req.Version = "v2c"
	}

	browseID := req.BrowseID
	if browseID == "" {
		browseID = uuid.New().String()
	}

	publishProgress := func(progress types.BrowseProgressMessage) {
		subj := topics.BrowseProgress("snmp", browseID)
		d, _ := json.Marshal(progress)
		_ = s.b.Publish(subj, d)
	}

	version := parseVersion(req.Version)
	client := s.createClient(req.Host, req.Port, version, req.Community, req.V3Auth)

	if req.Async {
		s.respondJSON(reply, map[string]string{"browseId": browseID})
		go func() {
			result, err := s.browseDevice(client, req.DeviceID, req.RootOID, browseID, publishProgress)
			if err != nil {
				s.log.Error("snmp: async browse failed", "device", req.DeviceID, "error", err)
				publishProgress(types.BrowseProgressMessage{
					BrowseID:  browseID,
					ModuleID:  s.moduleID,
					DeviceID:  req.DeviceID,
					Phase:     "failed",
					Message:   fmt.Sprintf("Browse failed: %v", err),
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				})
			} else {
				s.log.Info("snmp: async browse complete", "device", req.DeviceID, "oids", len(result.OIDs))
				resultSubject := fmt.Sprintf("snmp.browse.result.%s", browseID)
				resultData, err := json.Marshal(result)
				if err != nil {
					s.log.Error("snmp: failed to marshal browse result", "error", err)
				} else {
					s.log.Info("snmp: publishing browse result", "subject", resultSubject, "bytes", len(resultData))
					if err := s.b.Publish(resultSubject, resultData); err != nil {
						s.log.Error("snmp: failed to publish browse result", "error", err)
					}
				}
			}
		}()
		return
	}

	result, err := s.browseDevice(client, req.DeviceID, req.RootOID, browseID, publishProgress)
	if err != nil {
		s.log.Error("snmp: browse failed", "device", req.DeviceID, "error", err)
		s.respondJSON(reply, BrowseResult{DeviceID: req.DeviceID, RootOID: req.RootOID, OIDs: []OidInfo{}})
		return
	}

	s.log.Info("snmp: browse complete", "device", req.DeviceID, "oids", len(result.OIDs))
	s.respondJSON(reply, result)
}

func (s *Scanner) browseDevice(client *gosnmp.GoSNMP, deviceID, rootOID, browseID string, publishProgress func(types.BrowseProgressMessage)) (*BrowseResult, error) {
	// Disable OID increasing check — some devices (switches, etc.) return OIDs out of order.
	if client.AppOpts == nil {
		client.AppOpts = make(map[string]interface{})
	}
	client.AppOpts["c"] = true

	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("connect failed: %w", err)
	}
	defer client.Conn.Close()

	publishProgress(types.BrowseProgressMessage{
		BrowseID:  browseID,
		ModuleID:  s.moduleID,
		DeviceID:  deviceID,
		Phase:     "walking",
		Message:   fmt.Sprintf("Walking from %s", rootOID),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	var oids []OidInfo
	var walkErr error

	walkFn := func(pdu gosnmp.SnmpPDU) error {
		snmpType := pduTypeToString(pdu.Type)
		value := pduToValue(pdu)
		datatype := snmpToNatsDatatype(snmpType)
		name := s.mibTree.ResolveOidName(pdu.Name)
		key := SanitizeNameForKey(name)

		oids = append(oids, OidInfo{
			OID:      pdu.Name,
			Name:     name,
			Key:      key,
			Value:    value,
			SnmpType: snmpType,
			Datatype: datatype,
		})

		if len(oids)%100 == 0 {
			publishProgress(types.BrowseProgressMessage{
				BrowseID:      browseID,
				ModuleID:      s.moduleID,
				DeviceID:      deviceID,
				Phase:         "walking",
				TotalCount:     0,
				DiscoveredCount: len(oids),
				Message:       fmt.Sprintf("Discovered %d OIDs so far", len(oids)),
				Timestamp:     time.Now().UTC().Format(time.RFC3339),
			})
		}
		return nil
	}

	if client.Version == gosnmp.Version1 {
		walkErr = client.Walk(rootOID, walkFn)
	} else {
		walkErr = client.BulkWalk(rootOID, walkFn)
	}

	if walkErr != nil {
		return nil, fmt.Errorf("walk failed: %w", walkErr)
	}

	publishProgress(types.BrowseProgressMessage{
		BrowseID:      browseID,
		ModuleID:      s.moduleID,
		DeviceID:      deviceID,
		Phase:         "completed",
		TotalCount:     len(oids),
		DiscoveredCount: len(oids),
		Message:       fmt.Sprintf("Walk complete: %d OIDs", len(oids)),
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	})

	// Detect table structures from walked OIDs using MIB table metadata
	udts, structTags := s.detectTables(oids)

	return &BrowseResult{
		DeviceID:   deviceID,
		RootOID:    rootOID,
		OIDs:       oids,
		Udts:       udts,
		StructTags: structTags,
	}, nil
}

// detectTables groups walked OIDs into SNMP table rows using MIB table metadata.
// Returns UDT definitions and structTag mappings (instance -> type name).
func (s *Scanner) detectTables(oids []OidInfo) (map[string]SnmpTableExport, map[string]string) {
	if s.mibTree == nil || len(s.mibTree.Tables) == 0 {
		return nil, nil
	}

	udts := make(map[string]SnmpTableExport)
	structTags := make(map[string]string)

	// Track which columns actually appear for each table type
	seenColumns := make(map[string]map[int]bool) // typeName -> set of subIds

	for _, oid := range oids {
		normalizedOid := oid.OID
		if !strings.HasPrefix(normalizedOid, ".") {
			normalizedOid = "." + normalizedOid
		}

		// Check each known table entry to see if this OID belongs to it
		for entryOID, table := range s.mibTree.Tables {
			prefix := entryOID + "."
			if !strings.HasPrefix(normalizedOid, prefix) {
				continue
			}

			// OID format: entryOID.columnSubId.instanceSuffix
			remainder := normalizedOid[len(prefix):]
			dotIdx := strings.Index(remainder, ".")
			if dotIdx == -1 {
				continue // no instance suffix
			}

			colSubIdStr := remainder[:dotIdx]
			instanceSuffix := remainder[dotIdx+1:]
			colSubId, err := strconv.Atoi(colSubIdStr)
			if err != nil {
				continue
			}

			// Build instance key: tableName_instanceSuffix
			instanceKey := SanitizeNameForKey(table.TableName + "." + instanceSuffix)
			structTags[instanceKey] = table.TypeName
			// Also map the raw OID so the frontend can filter it from atomic tags.
			structTags[oid.OID] = table.TypeName

			// Track seen columns
			if _, ok := seenColumns[table.TypeName]; !ok {
				seenColumns[table.TypeName] = make(map[int]bool)
			}
			seenColumns[table.TypeName][colSubId] = true
			break // matched a table, no need to check others
		}
	}

	// Build UDT exports using only columns that were actually seen
	for _, table := range s.mibTree.Tables {
		seen, ok := seenColumns[table.TypeName]
		if !ok {
			continue // no instances of this table found
		}
		// Skip if we already added this type (multiple tables can share a type via AUGMENTS)
		if _, exists := udts[table.TypeName]; exists {
			continue
		}

		var members []SnmpTableColumnExport
		for _, col := range table.Columns {
			if !seen[col.SubId] {
				continue
			}
			members = append(members, SnmpTableColumnExport{
				Name:     col.Name,
				Datatype: col.Datatype,
				SubId:    col.SubId,
			})
		}
		if len(members) > 0 {
			udts[table.TypeName] = SnmpTableExport{
				Name:    table.TypeName,
				Members: members,
			}
		}
	}

	if len(udts) == 0 {
		return nil, nil
	}

	return udts, structTags
}

// =========================================================================
// Subscribe handler
// =========================================================================

func (s *Scanner) handleSubscribe(subject string, data []byte, reply bus.ReplyFunc) {
	var req SubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	if req.DeviceID == "" || req.Host == "" || req.SubscriberID == "" || len(req.OIDs) == 0 {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": "missing required fields"})
		return
	}

	if req.Port == 0 {
		req.Port = 161
	}
	if req.Version == "" {
		req.Version = "v2c"
	}

	scanRate := req.ScanRate
	if scanRate <= 0 {
		scanRate = 5000
	}

	version := parseVersion(req.Version)

	s.mu.Lock()
	conn, exists := s.connections[req.DeviceID]
	if !exists {
		client := s.createClient(req.Host, req.Port, version, req.Community, req.V3Auth)
		if err := client.Connect(); err != nil {
			s.mu.Unlock()
			s.log.Error("snmp: failed to connect", "device", req.DeviceID, "host", req.Host, "port", req.Port, "error", err)
			s.respondJSON(reply, map[string]interface{}{"success": false, "error": fmt.Sprintf("connect failed: %v", err)})
			return
		}

		conn = &DeviceConnection{
			DeviceID:    req.DeviceID,
			Host:        req.Host,
			Port:        req.Port,
			Version:     version,
			Community:   req.Community,
			V3Auth:      req.V3Auth,
			Client:      client,
			Variables:   make(map[string]*CachedVar),
			Subscribers: make(map[string]map[string]bool),
			ScanRate:    time.Duration(scanRate) * time.Millisecond,
			UseBulkGet:  req.UseBulkGet,
			stopChan:    make(chan struct{}),
		}
		s.connections[req.DeviceID] = conn
		s.log.Info("snmp: created connection", "device", req.DeviceID, "host", req.Host, "port", req.Port, "version", req.Version)
	}
	s.mu.Unlock()

	conn.mu.Lock()
	if conn.Subscribers[req.SubscriberID] == nil {
		conn.Subscribers[req.SubscriberID] = make(map[string]bool)
	}

	for _, oid := range req.OIDs {
		conn.Subscribers[req.SubscriberID][oid] = true
		if _, exists := conn.Variables[oid]; !exists {
			conn.Variables[oid] = &CachedVar{
				OID:     oid,
				Quality: "unknown",
			}
		}
	}
	conn.mu.Unlock()

	if !exists {
		go s.pollDevice(conn)
	}

	s.log.Info("snmp: subscriber added OIDs", "subscriber", req.SubscriberID, "count", len(req.OIDs), "device", req.DeviceID, "total", len(conn.Variables))

	s.respondJSON(reply, map[string]interface{}{
		"success": true,
		"count":   len(req.OIDs),
	})
}

// =========================================================================
// Unsubscribe handler
// =========================================================================

func (s *Scanner) handleUnsubscribe(subject string, data []byte, reply bus.ReplyFunc) {
	var req UnsubscribeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	s.mu.RLock()
	conn, exists := s.connections[req.DeviceID]
	s.mu.RUnlock()

	if !exists {
		s.respondJSON(reply, map[string]interface{}{"success": true, "count": 0})
		return
	}

	conn.mu.Lock()
	if subs, ok := conn.Subscribers[req.SubscriberID]; ok {
		for _, oid := range req.OIDs {
			delete(subs, oid)
		}
		if len(subs) == 0 {
			delete(conn.Subscribers, req.SubscriberID)
		}
	}

	shouldClose := len(conn.Subscribers) == 0
	conn.mu.Unlock()

	if shouldClose {
		s.mu.Lock()
		close(conn.stopChan)
		if conn.Client != nil {
			_ = conn.Client.Conn.Close()
		}
		delete(s.connections, req.DeviceID)
		s.mu.Unlock()
		s.log.Info("snmp: closed connection (no subscribers)", "device", req.DeviceID)
	}

	s.respondJSON(reply, map[string]interface{}{
		"success": true,
		"count":   len(req.OIDs),
	})
}

// =========================================================================
// Variables handler
// =========================================================================

func (s *Scanner) handleVariables(subject string, data []byte, reply bus.ReplyFunc) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var allVars []VariableInfo
	for _, conn := range s.connections {
		conn.mu.RLock()
		for _, v := range conn.Variables {
			name := s.mibTree.ResolveOidName(v.OID)
			allVars = append(allVars, VariableInfo{
				ModuleID:    s.moduleID,
				DeviceID:    conn.DeviceID,
				VariableID:  name,
				OID:         v.OID,
				Source:      v.OID,
				Value:       v.Value,
				Datatype:    v.Datatype,
				SnmpType:    v.SnmpType,
				Quality:     v.Quality,
				Origin:      "snmp",
				LastUpdated: v.LastRead,
			})
		}
		conn.mu.RUnlock()
	}

	s.respondJSON(reply, allVars)
}

// =========================================================================
// SET handler
// =========================================================================

func (s *Scanner) handleSet(subject string, data []byte, reply bus.ReplyFunc) {
	var req SetRequest
	if err := json.Unmarshal(data, &req); err != nil {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	if req.DeviceID == "" || req.Host == "" || len(req.Variables) == 0 {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": "missing required fields"})
		return
	}

	if req.Port == 0 {
		req.Port = 161
	}
	if req.Version == "" {
		req.Version = "v2c"
	}

	version := parseVersion(req.Version)
	client := s.createClient(req.Host, req.Port, version, req.Community, req.V3Auth)
	if err := client.Connect(); err != nil {
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": fmt.Sprintf("connect failed: %v", err)})
		return
	}
	defer client.Conn.Close()

	pdus := make([]gosnmp.SnmpPDU, 0, len(req.Variables))
	for _, v := range req.Variables {
		pdu, err := buildSetPDU(v)
		if err != nil {
			s.respondJSON(reply, map[string]interface{}{"success": false, "error": fmt.Sprintf("invalid variable %s: %v", v.OID, err)})
			return
		}
		pdus = append(pdus, pdu)
	}

	result, err := client.Set(pdus)
	if err != nil {
		s.log.Error("snmp: SET failed", "device", req.DeviceID, "error", err)
		s.respondJSON(reply, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}

	s.log.Info("snmp: SET success", "device", req.DeviceID, "count", len(pdus), "errorStatus", result.Error)
	s.respondJSON(reply, map[string]interface{}{
		"success":     result.Error == 0,
		"errorStatus": result.Error,
		"errorIndex":  result.ErrorIndex,
	})
}

// =========================================================================
// Polling loop
// =========================================================================

func (s *Scanner) pollDevice(conn *DeviceConnection) {
	s.log.Info("snmp: starting poll loop", "device", conn.DeviceID, "scanRate", conn.ScanRate)

	ticker := time.NewTicker(conn.ScanRate)
	defer ticker.Stop()

	for {
		select {
		case <-conn.stopChan:
			s.log.Info("snmp: poll loop stopped", "device", conn.DeviceID)
			return
		case <-ticker.C:
			if !s.enabled.Load() {
				continue // skip polling when disabled
			}
			s.pollOnce(conn)
		}
	}
}

func (s *Scanner) pollOnce(conn *DeviceConnection) {
	conn.mu.RLock()
	oids := make([]string, 0, len(conn.Variables))
	for oid := range conn.Variables {
		oids = append(oids, oid)
	}
	conn.mu.RUnlock()

	if len(oids) == 0 {
		return
	}

	// SNMP GET can handle multiple OIDs per request, but there's a practical limit.
	// Batch into groups of 20 to avoid exceeding device limits.
	const batchSize = 20
	for i := 0; i < len(oids); i += batchSize {
		end := i + batchSize
		if end > len(oids) {
			end = len(oids)
		}
		batch := oids[i:end]

		var result *gosnmp.SnmpPacket
		var err error

		if conn.UseBulkGet && conn.Version != gosnmp.Version1 {
			result, err = conn.Client.GetBulk(batch, 0, 1)
		} else {
			result, err = conn.Client.Get(batch)
		}

		if err != nil {
			s.log.Warn("snmp: poll failed", "device", conn.DeviceID, "error", err)
			// Try to reconnect
			if reconnErr := s.reconnect(conn); reconnErr != nil {
				s.log.Warn("snmp: reconnect failed", "device", conn.DeviceID, "error", reconnErr)
			}
			conn.mu.Lock()
			for _, oid := range batch {
				if v, ok := conn.Variables[oid]; ok {
					v.Quality = "bad"
				}
			}
			conn.mu.Unlock()
			continue
		}

		now := time.Now().UnixMilli()
		for _, pdu := range result.Variables {
			snmpType := pduTypeToString(pdu.Type)
			value := pduToValue(pdu)
			datatype := snmpToNatsDatatype(snmpType)

			// Skip error types
			if pdu.Type == gosnmp.NoSuchObject || pdu.Type == gosnmp.NoSuchInstance || pdu.Type == gosnmp.EndOfMibView {
				conn.mu.Lock()
				if v, ok := conn.Variables[pdu.Name]; ok {
					v.Quality = "bad"
					v.SnmpType = snmpType
				}
				conn.mu.Unlock()
				continue
			}

			conn.mu.Lock()
			v, ok := conn.Variables[pdu.Name]
			if !ok {
				conn.mu.Unlock()
				continue
			}

			changed := v.Value != value
			s.log.Debug("snmp: poll result", "device", conn.DeviceID, "oid", pdu.Name, "value", value, "prev", v.Value, "type", snmpType, "changed", changed)
			v.Value = value
			v.Datatype = datatype
			v.SnmpType = snmpType
			v.Quality = "good"
			v.LastRead = now
			conn.mu.Unlock()

			if changed {
				dataMsg := SnmpDataMessage{
					ModuleID:   s.moduleID,
					DeviceID:   conn.DeviceID,
					VariableID: s.mibTree.ResolveOidName(pdu.Name),
					OID:        pdu.Name,
					Value:      value,
					Timestamp:  now,
					Datatype:   datatype,
					SnmpType:   snmpType,
				}
				d, _ := json.Marshal(dataMsg)
				subj := topics.Data(s.moduleID, types.SanitizeForSubject(conn.DeviceID), sanitizeOidForSubject(pdu.Name))
				_ = s.b.Publish(subj, d)
			}
		}
	}
}

// reconnect attempts to re-establish the SNMP connection.
func (s *Scanner) reconnect(conn *DeviceConnection) error {
	if conn.Client != nil && conn.Client.Conn != nil {
		_ = conn.Client.Conn.Close()
	}
	conn.Client = s.createClient(conn.Host, conn.Port, conn.Version, conn.Community, conn.V3Auth)
	return conn.Client.Connect()
}

// =========================================================================
// SNMP client helpers
// =========================================================================

func (s *Scanner) createClient(host string, port int, version gosnmp.SnmpVersion, community string, v3Auth *V3Auth) *gosnmp.GoSNMP {
	client := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(port),
		Version:   version,
		Community: community,
		Timeout:   5 * time.Second,
		Retries:   2,
	}

	if version == gosnmp.Version3 && v3Auth != nil {
		client.SecurityModel = gosnmp.UserSecurityModel
		client.MsgFlags = parseSecurityLevel(v3Auth.SecurityLevel)
		client.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 v3Auth.Username,
			AuthenticationProtocol:   parseAuthProtocol(v3Auth.AuthProtocol),
			AuthenticationPassphrase: v3Auth.AuthPassword,
			PrivacyProtocol:          parsePrivProtocol(v3Auth.PrivProtocol),
			PrivacyPassphrase:        v3Auth.PrivPassword,
		}
	}

	return client
}

func parseVersion(v string) gosnmp.SnmpVersion {
	switch v {
	case "v1":
		return gosnmp.Version1
	case "v3":
		return gosnmp.Version3
	default:
		return gosnmp.Version2c
	}
}

func versionToString(v gosnmp.SnmpVersion) string {
	switch v {
	case gosnmp.Version1:
		return "v1"
	case gosnmp.Version3:
		return "v3"
	default:
		return "v2c"
	}
}

func parseSecurityLevel(level string) gosnmp.SnmpV3MsgFlags {
	switch level {
	case "authNoPriv":
		return gosnmp.AuthNoPriv
	case "authPriv":
		return gosnmp.AuthPriv
	default:
		return gosnmp.NoAuthNoPriv
	}
}

func parseAuthProtocol(proto string) gosnmp.SnmpV3AuthProtocol {
	switch proto {
	case "SHA":
		return gosnmp.SHA
	case "SHA256":
		return gosnmp.SHA256
	case "SHA512":
		return gosnmp.SHA512
	default:
		return gosnmp.MD5
	}
}

func parsePrivProtocol(proto string) gosnmp.SnmpV3PrivProtocol {
	switch proto {
	case "AES":
		return gosnmp.AES
	case "AES192":
		return gosnmp.AES192
	case "AES256":
		return gosnmp.AES256
	default:
		return gosnmp.DES
	}
}

// pduTypeToString converts a gosnmp PDU type to a human-readable string.
func pduTypeToString(t gosnmp.Asn1BER) string {
	switch t {
	case gosnmp.Integer:
		return "Integer32"
	case gosnmp.OctetString:
		return "OctetString"
	case gosnmp.Null:
		return "Null"
	case gosnmp.ObjectIdentifier:
		return "ObjectIdentifier"
	case gosnmp.IPAddress:
		return "IpAddress"
	case gosnmp.Counter32:
		return "Counter32"
	case gosnmp.Gauge32:
		return "Gauge32"
	case gosnmp.TimeTicks:
		return "TimeTicks"
	case gosnmp.Opaque:
		return "Opaque"
	case gosnmp.Counter64:
		return "Counter64"
	case gosnmp.NoSuchObject:
		return "NoSuchObject"
	case gosnmp.NoSuchInstance:
		return "NoSuchInstance"
	case gosnmp.EndOfMibView:
		return "EndOfMibView"
	default:
		return fmt.Sprintf("Unknown(%d)", t)
	}
}

// pduToValue extracts a Go value from an SNMP PDU.
func pduToValue(pdu gosnmp.SnmpPDU) interface{} {
	switch pdu.Type {
	case gosnmp.Integer:
		return gosnmp.ToBigInt(pdu.Value).Int64()
	case gosnmp.OctetString:
		b, ok := pdu.Value.([]byte)
		if !ok {
			return fmt.Sprintf("%v", pdu.Value)
		}
		if isPrintable(b) {
			return string(b)
		}
		return fmt.Sprintf("%x", b)
	case gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks:
		return gosnmp.ToBigInt(pdu.Value).Uint64()
	case gosnmp.Counter64:
		return gosnmp.ToBigInt(pdu.Value).Uint64()
	case gosnmp.ObjectIdentifier:
		if s, ok := pdu.Value.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", pdu.Value)
	case gosnmp.IPAddress:
		if s, ok := pdu.Value.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", pdu.Value)
	case gosnmp.Null, gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView:
		return nil
	default:
		return fmt.Sprintf("%v", pdu.Value)
	}
}

// isPrintable checks if a byte slice contains printable ASCII characters
// (including common whitespace like newlines and tabs).
func isPrintable(b []byte) bool {
	for _, c := range b {
		if c == '\n' || c == '\r' || c == '\t' {
			continue
		}
		if c < 32 || c > 126 {
			return false
		}
	}
	return len(b) > 0
}

// buildSetPDU creates an SNMP PDU for a SET operation.
func buildSetPDU(v SetVar) (gosnmp.SnmpPDU, error) {
	pdu := gosnmp.SnmpPDU{Name: v.OID}

	switch v.Type {
	case "integer":
		val, ok := v.Value.(float64) // JSON numbers come as float64
		if !ok {
			return pdu, fmt.Errorf("expected number for integer type")
		}
		pdu.Type = gosnmp.Integer
		pdu.Value = int(val)
	case "string":
		val, ok := v.Value.(string)
		if !ok {
			return pdu, fmt.Errorf("expected string for string type")
		}
		pdu.Type = gosnmp.OctetString
		pdu.Value = val
	case "gauge":
		val, ok := v.Value.(float64)
		if !ok {
			return pdu, fmt.Errorf("expected number for gauge type")
		}
		pdu.Type = gosnmp.Gauge32
		pdu.Value = uint(val)
	case "counter":
		val, ok := v.Value.(float64)
		if !ok {
			return pdu, fmt.Errorf("expected number for counter type")
		}
		pdu.Type = gosnmp.Counter32
		pdu.Value = uint(val)
	case "timeticks":
		val, ok := v.Value.(float64)
		if !ok {
			return pdu, fmt.Errorf("expected number for timeticks type")
		}
		pdu.Type = gosnmp.TimeTicks
		pdu.Value = uint(val)
	case "ipAddress":
		val, ok := v.Value.(string)
		if !ok {
			return pdu, fmt.Errorf("expected string for ipAddress type")
		}
		if ip := net.ParseIP(val); ip == nil {
			return pdu, fmt.Errorf("invalid IP address: %s", val)
		}
		pdu.Type = gosnmp.IPAddress
		pdu.Value = val
	case "oid":
		val, ok := v.Value.(string)
		if !ok {
			return pdu, fmt.Errorf("expected string for oid type")
		}
		pdu.Type = gosnmp.ObjectIdentifier
		pdu.Value = val
	default:
		return pdu, fmt.Errorf("unsupported type: %s", v.Type)
	}

	return pdu, nil
}

// respondJSON marshals v and calls the reply function.
func (s *Scanner) respondJSON(reply bus.ReplyFunc, v interface{}) {
	if reply == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		s.log.Error("snmp: failed to marshal response", "error", err)
		return
	}
	if err := reply(data); err != nil {
		s.log.Error("snmp: failed to respond", "error", err)
	}
}
