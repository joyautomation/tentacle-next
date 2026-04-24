package topics

import (
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
)

// Bucket name constants.
const (
	BucketHeartbeats     = "service_heartbeats"
	BucketServiceEnabled = "service_enabled"
	BucketServiceStatus  = "service_status"
	BucketDesiredServices = "desired_services"
	BucketGatewayConfig  = "gateway_config"
	BucketTentacleConfig = "tentacle_config"
	BucketPlcVariables   = "plc_variables"
	BucketDeviceRegistry = "device_registry"
	BucketBrowseCache    = "browse_cache"

	// BucketDevices holds shared device connection configuration
	// keyed by deviceId. Both gateway and PLC watch this bucket and
	// reference devices by deviceId from their own config.
	BucketDevices = "devices"

	// BucketDevicesLegacy is the pre-rename bucket name. Kept solely so
	// the migration step can drain it into BucketDevices on startup. Do
	// not use for new reads/writes.
	BucketDevicesLegacy = "sources"

	// Per-protocol scanner subscription config buckets.
	// Controllers (gateway, plc) write desired subscriptions here;
	// scanners watch their bucket and act independently.
	BucketScannerEthernetIP = "scanner_config_ethernetip"
	BucketScannerOpcUA      = "scanner_config_opcua"
	BucketScannerModbus     = "scanner_config_modbus"
	BucketScannerSNMP       = "scanner_config_snmp"

	// PLC configuration and programs.
	BucketPlcConfig    = "plc_config"
	BucketPlcPrograms  = "plc_programs"
	BucketPlcTemplates = "plc_templates"
	BucketPlcTests     = "plc_tests"

	// BucketPlcValues persists the last-known runtime value of every
	// PLC variable so values survive restart/redeploy. Keyed by
	// variable ID; value is a JSON snapshot of the RuntimeVariable.
	BucketPlcValues = "plc_values"

	// PROFINET IO Device configuration bucket.
	BucketProfinetConfig = "profinet_config"

	// PROFINET IO Controller scanner config bucket.
	BucketScannerProfinetController = "scanner_config_profinetcontroller"

	// HMI app configuration bucket (apps, screens, components).
	BucketHmiConfig = "hmi_config"

	// Config metadata tracks the source of each config write (gui, cli, gitops)
	// to prevent feedback loops in bidirectional sync.
	BucketConfigMetadata = "config_metadata"
)

// ScannerBucket returns the KV bucket name for a given protocol.
func ScannerBucket(protocol string) string {
	switch protocol {
	case "ethernetip":
		return BucketScannerEthernetIP
	case "opcua":
		return BucketScannerOpcUA
	case "modbus":
		return BucketScannerModbus
	case "snmp":
		return BucketScannerSNMP
	case "profinetcontroller":
		return BucketScannerProfinetController
	default:
		return ""
	}
}

// BucketConfigs returns the default KV bucket configurations.
func BucketConfigs() map[string]bus.KVBucketConfig {
	return map[string]bus.KVBucketConfig{
		BucketHeartbeats:      {TTL: 60 * time.Second, History: 1},
		BucketServiceEnabled:  {History: 1},
		BucketServiceStatus:   {TTL: 120 * time.Second, History: 1},
		BucketDesiredServices: {History: 1},
		BucketGatewayConfig:   {History: 5},
		BucketTentacleConfig:  {History: 5},
		BucketPlcVariables:    {History: 1},
		BucketDeviceRegistry:  {History: 1},
		BucketBrowseCache:     {History: 1},
		BucketDevices:         {History: 5},
		BucketScannerEthernetIP: {History: 1},
		BucketScannerOpcUA:      {History: 1},
		BucketScannerModbus:     {History: 1},
		BucketScannerSNMP:       {History: 1},
		BucketPlcConfig:                 {History: 5},
		BucketPlcPrograms:               {History: 10},
		BucketPlcTemplates:              {History: 10},
		BucketPlcTests:                  {History: 10},
		BucketPlcValues:                 {History: 1},
		BucketProfinetConfig:            {History: 5},
		BucketScannerProfinetController: {History: 1},
		BucketHmiConfig:                 {History: 5},
		BucketConfigMetadata:    {History: 1},
	}
}
