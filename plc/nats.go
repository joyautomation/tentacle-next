package plc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	ttypes "github.com/joyautomation/tentacle/types"
)

// natsManager handles all NATS communication for a PLC instance.
type natsManager struct {
	conn      *nats.Conn
	js        jetstream.JetStream
	projectID string
	vars      *Variables
	configs   map[string]VariableConfig
	rbeStates map[string]*rbeState
	update    UpdateFunc
	log       *slog.Logger
	subs      []*nats.Subscription

	mu   sync.Mutex
	wg   sync.WaitGroup
	done chan struct{}
}

func newNatsManager(projectID string, vars *Variables, configs map[string]VariableConfig, update UpdateFunc, log *slog.Logger) *natsManager {
	return &natsManager{
		projectID: projectID,
		vars:      vars,
		configs:   configs,
		rbeStates: make(map[string]*rbeState),
		update:    update,
		log:       log,
		done:      make(chan struct{}),
	}
}

func (nm *natsManager) connect(cfg Config) error {
	opts := []nats.Option{
		nats.Name("plc-" + cfg.ProjectID),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(5 * time.Second),
	}
	if cfg.NatsUser != "" {
		opts = append(opts, nats.UserInfo(cfg.NatsUser, cfg.NatsPass))
	}
	if cfg.NatsToken != "" {
		opts = append(opts, nats.Token(cfg.NatsToken))
	}

	nc, err := nats.Connect(cfg.NatsURL, opts...)
	if err != nil {
		return fmt.Errorf("plc: nats connect %s: %w", cfg.NatsURL, err)
	}
	nm.conn = nc

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return fmt.Errorf("plc: jetstream: %w", err)
	}
	nm.js = js
	return nil
}

// ─── Subscriptions ──────────────────────────────────────────────────────────

func (nm *natsManager) setupSubscriptions() error {
	// Command handler: {projectId}.command.{variableId}
	sub, err := nm.conn.Subscribe(nm.projectID+".command.*", nm.handleCommand)
	if err != nil {
		return fmt.Errorf("subscribe command: %w", err)
	}
	nm.subs = append(nm.subs, sub)

	// Variables request: {projectId}.variables
	sub, err = nm.conn.Subscribe(nm.projectID+".variables", nm.handleVariablesRequest)
	if err != nil {
		return fmt.Errorf("subscribe variables: %w", err)
	}
	nm.subs = append(nm.subs, sub)

	// Subscribe to data topics for each sourced variable.
	for id, cfg := range nm.configs {
		if cfg.Source == nil {
			continue
		}
		if err := nm.subscribeSource(id, cfg); err != nil {
			nm.log.Warn("failed to subscribe source", "variable", id, "error", err)
		}
	}
	return nil
}

func (nm *natsManager) subscribeSource(variableID string, cfg VariableConfig) error {
	subject := nm.deriveSourceSubject(cfg.Source)
	if subject == "" {
		return nil
	}

	sub, err := nm.conn.Subscribe(subject, func(msg *nats.Msg) {
		nm.handleSourceData(variableID, cfg, msg)
	})
	if err != nil {
		return err
	}
	nm.subs = append(nm.subs, sub)
	return nil
}

func (nm *natsManager) deriveSourceSubject(src *Source) string {
	if src.Subject != "" {
		return src.Subject
	}
	if s := src.EthernetIP; s != nil {
		return "*.data." + s.DeviceID + "." + ttypes.SanitizeForSubject(s.Tag)
	}
	if s := src.OpcUA; s != nil {
		return "*.data." + s.DeviceID + "." + ttypes.SanitizeForSubject(s.NodeID)
	}
	if s := src.Modbus; s != nil {
		return "*.data." + s.DeviceID + "." + ttypes.SanitizeForSubject(s.Tag)
	}
	if s := src.SNMP; s != nil {
		return "*.data." + s.DeviceID + "." + ttypes.SanitizeForSubject(s.OID)
	}
	return ""
}

// ─── Scanner Registration ───────────────────────────────────────────────────

// registerScannerSubscriptions tells protocol scanners what to poll.
func (nm *natsManager) registerScannerSubscriptions() {
	eip := map[string]*eipSubReq{}
	opcua := map[string]*opcuaSubReq{}
	modbus := map[string]*modbusSubReq{}
	snmp := map[string]*snmpSubReq{}

	subscriberID := "plc-" + nm.projectID

	for _, cfg := range nm.configs {
		if cfg.Source == nil {
			continue
		}
		if s := cfg.Source.EthernetIP; s != nil {
			key := s.DeviceID
			if eip[key] == nil {
				eip[key] = &eipSubReq{
					SubscriberID: subscriberID,
					DeviceID:     s.DeviceID,
					Host:         s.Host,
					Port:         s.Port,
					ScanRate:     s.ScanRate,
					CipTypes:     map[string]string{},
					Deadbands:    map[string]dbCfg{},
					DisableRBE:   map[string]bool{},
				}
			}
			eip[key].Tags = append(eip[key].Tags, s.Tag)
			if s.CipType != "" {
				eip[key].CipTypes[s.Tag] = s.CipType
			}
			if cfg.Deadband != nil {
				eip[key].Deadbands[s.Tag] = dbCfg{
					Value:   cfg.Deadband.Value,
					MinTime: cfg.Deadband.MinTime,
					MaxTime: cfg.Deadband.MaxTime,
				}
			}
			if cfg.DisableRBE {
				eip[key].DisableRBE[s.Tag] = true
			}
		}
		if s := cfg.Source.OpcUA; s != nil {
			key := s.DeviceID
			if opcua[key] == nil {
				opcua[key] = &opcuaSubReq{
					SubscriberID: subscriberID,
					DeviceID:     s.DeviceID,
					EndpointURL:  s.EndpointURL,
					ScanRate:     s.ScanRate,
				}
			}
			opcua[key].NodeIDs = append(opcua[key].NodeIDs, s.NodeID)
		}
		if s := cfg.Source.Modbus; s != nil {
			key := s.DeviceID
			if modbus[key] == nil {
				modbus[key] = &modbusSubReq{
					SubscriberID: subscriberID,
					DeviceID:     s.DeviceID,
					Host:         s.Host,
					Port:         s.Port,
					UnitID:       s.UnitID,
					ScanRate:     s.ScanRate,
				}
			}
			modbus[key].Registers = append(modbus[key].Registers, modbusReg{
				Tag:            s.Tag,
				Address:        s.Address,
				FunctionCode:   s.FunctionCode,
				ModbusDatatype: s.ModbusDatatype,
				ByteOrder:      s.ByteOrder,
			})
		}
		if s := cfg.Source.SNMP; s != nil {
			key := s.DeviceID
			if snmp[key] == nil {
				snmp[key] = &snmpSubReq{
					SubscriberID: subscriberID,
					DeviceID:     s.DeviceID,
					Host:         s.Host,
					Port:         s.Port,
					Version:      s.Version,
					Community:    s.Community,
					ScanRate:     s.ScanRate,
				}
				if s.V3Auth != nil {
					snmp[key].V3Auth = &v3AuthJSON{
						Username:      s.V3Auth.Username,
						SecurityLevel: s.V3Auth.SecurityLevel,
						AuthProtocol:  s.V3Auth.AuthProtocol,
						AuthPassword:  s.V3Auth.AuthPassword,
						PrivProtocol:  s.V3Auth.PrivProtocol,
						PrivPassword:  s.V3Auth.PrivPassword,
					}
				}
			}
			snmp[key].OIDs = append(snmp[key].OIDs, s.OID)
		}
	}

	timeout := 5 * time.Second
	for _, req := range eip {
		data, _ := json.Marshal(req)
		if _, err := nm.conn.Request("ethernetip.subscribe", data, timeout); err != nil {
			nm.log.Warn("ethernetip subscribe failed", "device", req.DeviceID, "error", err)
		}
	}
	for _, req := range opcua {
		data, _ := json.Marshal(req)
		if _, err := nm.conn.Request("opcua.subscribe", data, timeout); err != nil {
			nm.log.Warn("opcua subscribe failed", "device", req.DeviceID, "error", err)
		}
	}
	for _, req := range modbus {
		data, _ := json.Marshal(req)
		if _, err := nm.conn.Request("modbus.subscribe", data, timeout); err != nil {
			nm.log.Warn("modbus subscribe failed", "device", req.DeviceID, "error", err)
		}
	}
	for _, req := range snmp {
		data, _ := json.Marshal(req)
		if _, err := nm.conn.Request("snmp.subscribe", data, timeout); err != nil {
			nm.log.Warn("snmp subscribe failed", "device", req.DeviceID, "error", err)
		}
	}
}

// ─── Handlers ───────────────────────────────────────────────────────────────

func (nm *natsManager) handleCommand(msg *nats.Msg) {
	parts := strings.SplitN(msg.Subject, ".", 3)
	if len(parts) < 3 {
		return
	}
	variableID := parts[2]

	cfg, ok := nm.configs[variableID]
	if !ok {
		return
	}

	var cmd struct {
		Value interface{} `json:"value"`
	}
	if json.Unmarshal(msg.Data, &cmd) != nil {
		return
	}

	val := parseValue(cmd.Value, cfg.Datatype)
	nm.update(variableID, val)

	// Route to scanner if sourced.
	nm.routeCommand(variableID, cfg, val)

	if msg.Reply != "" {
		msg.Respond([]byte(`{"success":true}`))
	}
}

func (nm *natsManager) routeCommand(variableID string, cfg VariableConfig, val interface{}) {
	src := cfg.Source
	if src == nil || !src.Bidirectional {
		return
	}
	if src.OnSend != nil {
		val = src.OnSend(val)
	}

	if s := src.EthernetIP; s != nil {
		data, _ := json.Marshal(map[string]interface{}{"tag": s.Tag, "value": val})
		nm.conn.Publish("ethernetip.module.command."+ttypes.SanitizeForSubject(s.Tag), data)
	} else if s := src.OpcUA; s != nil {
		data, _ := json.Marshal(map[string]interface{}{"nodeId": s.NodeID, "value": val})
		nm.conn.Publish("opcua.module.command."+ttypes.SanitizeForSubject(s.NodeID), data)
	} else if s := src.Modbus; s != nil {
		data, _ := json.Marshal(map[string]interface{}{"tag": s.Tag, "value": val})
		nm.conn.Publish("modbus.module.command."+ttypes.SanitizeForSubject(s.Tag), data)
	} else if s := src.SNMP; s != nil {
		data, _ := json.Marshal(map[string]interface{}{"oid": s.OID, "value": val})
		nm.conn.Publish("snmp.set", data)
	}
}

func (nm *natsManager) handleSourceData(variableID string, cfg VariableConfig, msg *nats.Msg) {
	var data struct {
		Value interface{} `json:"value"`
	}
	if json.Unmarshal(msg.Data, &data) != nil {
		return
	}
	val := data.Value
	if cfg.Source != nil && cfg.Source.OnResponse != nil {
		val = cfg.Source.OnResponse(val)
	}
	nm.update(variableID, val)
}

func (nm *natsManager) handleVariablesRequest(msg *nats.Msg) {
	result := make([]ttypes.VariableInfo, 0, len(nm.vars.vars))
	for id, v := range nm.vars.vars {
		cfg := nm.configs[id]
		result = append(result, ttypes.VariableInfo{
			ModuleID:    nm.projectID,
			DeviceID:    nm.projectID,
			VariableID:  id,
			Value:       v.Value(),
			Datatype:    cfg.Datatype,
			Description: cfg.Description,
			Deadband:    cfg.Deadband,
			DisableRBE:  cfg.DisableRBE,
			UdtTemplate: cfg.UdtTemplate,
		})
	}
	out, _ := json.Marshal(result)
	msg.Respond(out)
}

// ─── Publishing ─────────────────────────────────────────────────────────────

func (nm *natsManager) publishVariable(variableID string, value interface{}) {
	cfg, ok := nm.configs[variableID]
	if !ok {
		return
	}

	// RBE check.
	nm.mu.Lock()
	state, exists := nm.rbeStates[variableID]
	if !exists {
		state = &rbeState{config: cfg.Deadband}
		nm.rbeStates[variableID] = state
	}
	nm.mu.Unlock()

	if !cfg.DisableRBE && !state.shouldPublish(value) {
		return
	}

	msg := ttypes.PlcDataMessage{
		ModuleID:   nm.projectID,
		DeviceID:   nm.projectID,
		VariableID: variableID,
		Value:      value,
		Timestamp:  time.Now().UnixMilli(),
		Datatype:   cfg.Datatype,
	}
	if cfg.UdtTemplate != nil {
		msg.UdtTemplate = cfg.UdtTemplate
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	subject := nm.projectID + ".data." + nm.projectID + "." + ttypes.SanitizeForSubject(variableID)
	nm.conn.Publish(subject, data)
}

// ─── Heartbeat ──────────────────────────────────────────────────────────────

func (nm *natsManager) startHeartbeat(startedAt int64) {
	kv, err := nm.js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:  "service_heartbeats",
		TTL:     60 * time.Second,
		History: 1,
	})
	if err != nil {
		nm.log.Warn("failed to access heartbeat KV", "error", err)
		return
	}

	nm.wg.Add(1)
	go func() {
		defer nm.wg.Done()
		publishHB := func() {
			hb := ttypes.ServiceHeartbeat{
				ServiceType: "plc",
				ModuleID:    nm.projectID,
				LastSeen:    time.Now().UnixMilli(),
				StartedAt:   startedAt,
			}
			data, _ := json.Marshal(hb)
			kv.Put(context.Background(), nm.projectID, data)
		}
		publishHB()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-nm.done:
				return
			case <-ticker.C:
				publishHB()
			}
		}
	}()
}

// ─── Cleanup ────────────────────────────────────────────────────────────────

func (nm *natsManager) close() {
	close(nm.done)
	nm.wg.Wait()
	for _, sub := range nm.subs {
		sub.Unsubscribe()
	}
	if nm.conn != nil {
		nm.conn.Close()
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func parseValue(raw interface{}, datatype string) interface{} {
	switch datatype {
	case Number:
		switch v := raw.(type) {
		case float64:
			return v
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		case json.Number:
			if f, err := v.Float64(); err == nil {
				return f
			}
		}
		return 0.0
	case Boolean:
		switch v := raw.(type) {
		case bool:
			return v
		case string:
			return v == "true" || v == "1"
		case float64:
			return v != 0
		}
		return false
	case String:
		if s, ok := raw.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", raw)
	default:
		return raw
	}
}

// ─── Wire-format types for scanner subscribe requests ───────────────────────

type dbCfg struct {
	Value   float64 `json:"value"`
	MinTime int64   `json:"minTime,omitempty"`
	MaxTime int64   `json:"maxTime,omitempty"`
}

type eipSubReq struct {
	SubscriberID string            `json:"subscriberId"`
	DeviceID     string            `json:"deviceId"`
	Host         string            `json:"host"`
	Port         int               `json:"port,omitempty"`
	Tags         []string          `json:"tags"`
	ScanRate     int               `json:"scanRate,omitempty"`
	CipTypes     map[string]string `json:"cipTypes,omitempty"`
	Deadbands    map[string]dbCfg  `json:"deadbands,omitempty"`
	DisableRBE   map[string]bool   `json:"disableRBE,omitempty"`
}

type opcuaSubReq struct {
	SubscriberID string   `json:"subscriberId"`
	DeviceID     string   `json:"deviceId"`
	EndpointURL  string   `json:"endpointUrl"`
	NodeIDs      []string `json:"nodeIds"`
	ScanRate     int      `json:"scanRate,omitempty"`
}

type modbusReg struct {
	Tag            string `json:"tag"`
	Address        int    `json:"address"`
	FunctionCode   int    `json:"functionCode"`
	ModbusDatatype string `json:"modbusDatatype"`
	ByteOrder      string `json:"byteOrder,omitempty"`
}

type modbusSubReq struct {
	SubscriberID string      `json:"subscriberId"`
	DeviceID     string      `json:"deviceId"`
	Host         string      `json:"host"`
	Port         int         `json:"port,omitempty"`
	UnitID       int         `json:"unitId,omitempty"`
	Registers    []modbusReg `json:"registers"`
	ScanRate     int         `json:"scanRate,omitempty"`
}

type v3AuthJSON struct {
	Username      string `json:"username"`
	SecurityLevel string `json:"securityLevel"`
	AuthProtocol  string `json:"authProtocol,omitempty"`
	AuthPassword  string `json:"authPassword,omitempty"`
	PrivProtocol  string `json:"privProtocol,omitempty"`
	PrivPassword  string `json:"privPassword,omitempty"`
}

type snmpSubReq struct {
	SubscriberID string      `json:"subscriberId"`
	DeviceID     string      `json:"deviceId"`
	Host         string      `json:"host"`
	Port         int         `json:"port,omitempty"`
	Version      string      `json:"version"`
	Community    string      `json:"community,omitempty"`
	V3Auth       *v3AuthJSON `json:"v3Auth,omitempty"`
	OIDs         []string    `json:"oids"`
	ScanRate     int         `json:"scanRate,omitempty"`
}
