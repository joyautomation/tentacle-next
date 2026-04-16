//go:build snmp || all

// Package snmp implements an SNMP scanner using gosnmp.
// It subscribes to OIDs on SNMP-capable devices and publishes data via the Bus.
package snmp

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/heartbeat"
	"github.com/joyautomation/tentacle/internal/topics"
	"github.com/joyautomation/tentacle/types"
)

const defaultServiceType = "snmp"

// Module implements the module.Module interface for SNMP scanning.
type Module struct {
	moduleID     string
	scanner      *Scanner
	trapListener *TrapListener
	stopHB       func()
	subs         []bus.Subscription
	b            bus.Bus
	trapPort     int
	log          *slog.Logger
}

// New creates a new SNMP module.
func New(moduleID string) *Module {
	if moduleID == "" {
		moduleID = "snmp"
	}
	return &Module{
		moduleID: moduleID,
		trapPort: 1162,
	}
}

func (m *Module) ModuleID() string    { return m.moduleID }
func (m *Module) ServiceType() string { return defaultServiceType }

// Start initializes the scanner, trap listener, heartbeat, and enabled state watcher.
func (m *Module) Start(ctx context.Context, b bus.Bus) error {
	m.b = b
	m.log = slog.Default().With("serviceType", m.ServiceType(), "moduleID", m.ModuleID())

	// Read trap port from environment
	if p := os.Getenv("SNMP_TRAP_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			m.trapPort = v
		}
	}

	// Ensure KV buckets exist
	for _, bucket := range []string{topics.BucketHeartbeats, topics.BucketServiceEnabled} {
		if err := b.KVCreate(bucket, topics.BucketConfigs()[bucket]); err != nil {
			m.log.Warn("snmp: failed to create bucket", "bucket", bucket, "error", err)
		}
	}

	// Load MIB files for OID name resolution
	mibDirs := os.Getenv("SNMP_MIB_DIRS")
	if mibDirs == "" {
		// Auto-detect standard MIB paths
		defaultPaths := []string{
			"/usr/share/snmp/mibs/ietf",
			"/usr/share/snmp/mibs/iana",
			"/usr/share/snmp/mibs",
		}
		var found []string
		for _, p := range defaultPaths {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				found = append(found, p)
			}
		}
		mibDirs = strings.Join(found, ":")
	}

	var mibTree *MibTree
	if mibDirs != "" {
		m.log.Info("snmp: loading MIBs", "dirs", mibDirs)
		mibTree = LoadMibDirs(mibDirs)
		m.log.Info("snmp: loaded OID definitions from MIBs", "count", len(mibTree.ByOID))
	} else {
		m.log.Info("snmp: no MIB directories found — OIDs will not be resolved to names")
		mibTree = EmptyMibTree()
	}

	// Create and start scanner
	m.scanner = NewScanner(b, m.moduleID, mibTree, m.log)
	m.scanner.Start()

	// Create and start trap listener
	m.trapListener = NewTrapListener(b, m.moduleID, m.trapPort, m.log)
	go m.trapListener.Start()

	// Start heartbeat
	m.stopHB = heartbeat.Start(b, m.moduleID, defaultServiceType, func() map[string]interface{} {
		devices := m.scanner.ActiveDevices()
		devicesJSON, _ := json.Marshal(devices)
		return map[string]interface{}{
			"devices":        string(devicesJSON),
			"deviceStatuses": m.scanner.DeviceStatuses(),
			"trapPort":       m.trapPort,
			"enabled":        m.scanner.IsEnabled(),
		}
	})

	// Watch enabled state
	if data, _, err := b.KVGet(topics.BucketServiceEnabled, m.moduleID); err == nil {
		var state types.ServiceEnabledKV
		if json.Unmarshal(data, &state) == nil {
			m.scanner.SetEnabled(state.Enabled)
			m.log.Info("snmp: initial enabled state", "enabled", state.Enabled)
		}
	}

	enabledSub, err := b.KVWatch(topics.BucketServiceEnabled, m.moduleID, func(key string, value []byte, op bus.KVOperation) {
		if op == bus.KVOpDelete {
			m.scanner.SetEnabled(true) // default to enabled
			return
		}
		var state types.ServiceEnabledKV
		if json.Unmarshal(value, &state) == nil {
			m.scanner.SetEnabled(state.Enabled)
		}
	})
	if err != nil {
		m.log.Warn("snmp: failed to watch service_enabled KV", "error", err)
	} else {
		m.subs = append(m.subs, enabledSub)
	}

	// Listen for shutdown via Bus
	shutdownSub, _ := b.Subscribe(topics.Shutdown(m.moduleID), func(subject string, data []byte, reply bus.ReplyFunc) {
		m.log.Info("snmp: received shutdown command via Bus")
		m.Stop()
		os.Exit(0)
	})
	m.subs = append(m.subs, shutdownSub)

	m.log.Info("snmp: service running", "moduleId", m.moduleID, "trapPort", m.trapPort)

	// Block until context cancelled or signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-sigChan:
	}
	return nil
}

// Stop tears down scanner, trap listener, heartbeat, and subscriptions.
func (m *Module) Stop() error {
	if m.trapListener != nil {
		m.trapListener.Stop()
	}
	if m.scanner != nil {
		m.scanner.Stop()
	}
	if m.stopHB != nil {
		m.stopHB()
	}
	for _, sub := range m.subs {
		_ = sub.Unsubscribe()
	}
	m.subs = nil
	m.log.Info("snmp: shutdown complete")
	return nil
}
