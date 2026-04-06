// Package topics defines NATS subject patterns and helpers used across all modules.
package topics

import "fmt"

// Scanner subjects (per protocol: ethernetip, opcua, snmp, modbus).
func Browse(protocol string) string                    { return protocol + ".browse" }
func BrowseProgress(protocol, browseID string) string  { return fmt.Sprintf("%s.browse.progress.%s", protocol, browseID) }
func ScannerSubscribe(protocol string) string          { return protocol + ".subscribe" }
func ScannerUnsubscribe(protocol string) string        { return protocol + ".unsubscribe" }
func ScannerVariables(protocol string) string          { return protocol + ".variables" }

// Data subjects.
func Data(moduleID, deviceID, variableID string) string { return fmt.Sprintf("%s.data.%s.%s", moduleID, deviceID, variableID) }
func DataWildcard(moduleID string) string               { return moduleID + ".data.>" }
func AllData() string                                   { return "*.data.>" }
func Command(moduleID, variableID string) string        { return fmt.Sprintf("%s.command.%s", moduleID, variableID) }
func CommandWildcard(moduleID string) string             { return moduleID + ".command.>" }
func Variables(moduleID string) string                  { return moduleID + ".variables" }
func Shutdown(moduleID string) string                   { return moduleID + ".shutdown" }

// Service management.
func ServiceLogs(serviceType, moduleID string) string { return fmt.Sprintf("service.logs.%s.%s", serviceType, moduleID) }

// Orchestrator.
const OrchestratorCommand = "orchestrator.command"

// MQTT Bridge.
const (
	MqttMetrics      = "mqtt.metrics"
	MqttStoreForward = "mqtt.store-forward"
)

// SNMP specific.
const SnmpSet = "snmp.set"
func SnmpTrap(deviceID string) string { return "snmp.trap." + deviceID }

// EtherNet/IP Server.
const (
	EIPServerSubscribe   = "ethernetip-server.subscribe"
	EIPServerUnsubscribe = "ethernetip-server.unsubscribe"
	EIPServerVariables   = "ethernetip-server.variables"
	EIPServerBrowse      = "ethernetip-server.browse"
)

// Network (build-tag gated).
const (
	NetworkInterfaces = "network.interfaces"
	NetworkState      = "network.state"
	NetworkCommand    = "network.command"
)

// Nftables (build-tag gated).
const (
	NftablesRules   = "nftables.rules"
	NftablesState   = "nftables.state"
	NftablesCommand = "nftables.command"
)

// Modbus Server.
const (
	ModbusServerSubscribe   = "modbus-server.subscribe"
	ModbusServerUnsubscribe = "modbus-server.unsubscribe"
	ModbusServerVariables   = "modbus-server.variables"
)

// History (request/reply).
const (
	HistoryQuery   = "history.query"
	HistoryUsage   = "history.usage"
	HistoryEnabled = "history.enabled"
)

// Network data subjects.
func NetworkData(interfaceName string) string { return "network.data." + interfaceName }

// Nftables data subjects.
func NftablesData(ruleKey string) string { return "nftables.data." + ruleKey }
