//go:build snmp || all

// Package snmp implements an SNMP scanner using gosnmp.
// It subscribes to OIDs on SNMP-capable devices and publishes data via the Bus.
package snmp

import "strings"

// BrowseRequest is the JSON payload for snmp.browse requests.
// Performs an SNMP WALK from a root OID.
type BrowseRequest struct {
	DeviceID       string  `json:"deviceId"`
	Host           string  `json:"host"`
	Port           int     `json:"port,omitempty"`
	Version        string  `json:"version"`             // "v1", "v2c", "v3"
	Community      string  `json:"community,omitempty"` // v1/v2c
	V3Auth         *V3Auth `json:"v3Auth,omitempty"`    // v3
	RootOID        string  `json:"rootOid,omitempty"`   // default ".1.3.6.1"
	BrowseID       string  `json:"browseId,omitempty"`
	Async          bool    `json:"async,omitempty"`
	MaxRepetitions int     `json:"maxRepetitions,omitempty"` // for BULKWALK, default 10
}

// V3Auth holds SNMPv3 USM authentication parameters.
type V3Auth struct {
	Username      string `json:"username"`
	AuthProtocol  string `json:"authProtocol,omitempty"`  // "MD5", "SHA", "SHA256"
	AuthPassword  string `json:"authPassword,omitempty"`
	PrivProtocol  string `json:"privProtocol,omitempty"`  // "DES", "AES", "AES256"
	PrivPassword  string `json:"privPassword,omitempty"`
	SecurityLevel string `json:"securityLevel"`           // "noAuthNoPriv", "authNoPriv", "authPriv"
}

// SubscribeRequest is the JSON payload for snmp.subscribe requests.
type SubscribeRequest struct {
	DeviceID     string   `json:"deviceId"`
	Host         string   `json:"host"`
	Port         int      `json:"port,omitempty"`
	Version      string   `json:"version"`
	Community    string   `json:"community,omitempty"`
	V3Auth       *V3Auth  `json:"v3Auth,omitempty"`
	OIDs         []string `json:"oids"`
	ScanRate     int      `json:"scanRate,omitempty"` // ms, default 5000
	SubscriberID string   `json:"subscriberId"`
	UseBulkGet   bool     `json:"useBulkGet,omitempty"`
}

// UnsubscribeRequest is the JSON payload for snmp.unsubscribe requests.
type UnsubscribeRequest struct {
	DeviceID     string   `json:"deviceId"`
	OIDs         []string `json:"oids"`
	SubscriberID string   `json:"subscriberId"`
}

// SetRequest is the JSON payload for snmp.set requests.
type SetRequest struct {
	DeviceID  string   `json:"deviceId"`
	Host      string   `json:"host"`
	Port      int      `json:"port,omitempty"`
	Version   string   `json:"version"`
	Community string   `json:"community,omitempty"`
	V3Auth    *V3Auth  `json:"v3Auth,omitempty"`
	Variables []SetVar `json:"variables"`
}

// SetVar describes a single variable to SET.
type SetVar struct {
	OID   string      `json:"oid"`
	Type  string      `json:"type"` // "integer", "string", "gauge", "counter", "timeticks", "ipAddress", "oid"
	Value interface{} `json:"value"`
}

// SnmpDataMessage is published on snmp.data.{deviceId}.{sanitizedOid}
// when a monitored OID changes value.
type SnmpDataMessage struct {
	ModuleID   string      `json:"moduleId"`
	DeviceID   string      `json:"deviceId"`
	VariableID string      `json:"variableId"` // OID or MIB-resolved name
	OID        string      `json:"oid"`        // always the numeric OID
	Value      interface{} `json:"value"`
	Timestamp  int64       `json:"timestamp"`
	Datatype   string      `json:"datatype"` // "number", "string", "boolean"
	SnmpType   string      `json:"snmpType"` // "Integer32", "OctetString", "Counter32", etc.
}

// TrapMessage is published on snmp.trap.{deviceId}
// when an SNMP trap/notification is received.
type TrapMessage struct {
	ModuleID  string         `json:"moduleId"`
	DeviceID  string         `json:"deviceId"`  // source IP or matched device
	TrapOID   string         `json:"trapOid"`
	Variables []TrapVariable `json:"variables"`
	Timestamp int64          `json:"timestamp"`
	Version   string         `json:"version"`            // "v1", "v2c", "v3"
	Community string         `json:"community,omitempty"`
}

// TrapVariable describes a single varbind in a trap.
type TrapVariable struct {
	OID      string      `json:"oid"`
	Value    interface{} `json:"value"`
	SnmpType string      `json:"snmpType"`
}

// OidInfo describes a discovered OID from an SNMP walk.
type OidInfo struct {
	OID      string      `json:"oid"`
	Name     string      `json:"name,omitempty"`     // MIB-resolved name (e.g., "sysDescr.0")
	Key      string      `json:"key,omitempty"`      // Sanitized key for codegen (e.g., "sysDescr_0")
	Value    interface{} `json:"value"`
	SnmpType string      `json:"snmpType"` // "Integer32", "OctetString", etc.
	Datatype string      `json:"datatype"` // "number", "string", "boolean"
}

// SnmpTableColumnExport describes a column in an SNMP table for codegen.
type SnmpTableColumnExport struct {
	Name     string `json:"name"`
	Datatype string `json:"datatype"`
	SubId    int    `json:"subId"`
}

// SnmpTableExport describes an SNMP table type for codegen.
type SnmpTableExport struct {
	Name    string                  `json:"name"`
	Members []SnmpTableColumnExport `json:"members"`
}

// BrowseResult is the full browse response.
type BrowseResult struct {
	DeviceID   string                     `json:"deviceId"`
	RootOID    string                     `json:"rootOid"`
	OIDs       []OidInfo                  `json:"oids"`
	Udts       map[string]SnmpTableExport `json:"udts,omitempty"`
	StructTags map[string]string          `json:"structTags,omitempty"`
}

// VariableInfo is the JSON structure returned for individual monitored OIDs.
type VariableInfo struct {
	ModuleID    string      `json:"moduleId"`
	DeviceID    string      `json:"deviceId"`
	VariableID  string      `json:"variableId"`        // MIB-resolved name or OID
	OID         string      `json:"oid,omitempty"`     // numeric OID
	Source      string      `json:"source,omitempty"`  // numeric OID (for GraphQL source field)
	Value       interface{} `json:"value"`
	Datatype    string      `json:"datatype"`
	SnmpType    string      `json:"snmpType"`
	Quality     string      `json:"quality"`
	Origin      string      `json:"origin"`
	LastUpdated int64       `json:"lastUpdated"`
}

// ActiveDevice describes a currently connected SNMP device for heartbeat metadata.
type ActiveDevice struct {
	DeviceID string `json:"deviceId"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	OidCount int    `json:"oidCount"`
	Version  string `json:"version"`
}

// snmpToNatsDatatype normalizes SNMP types to "number", "boolean", or "string".
func snmpToNatsDatatype(snmpType string) string {
	switch snmpType {
	case "Integer32", "Counter32", "Counter64", "Gauge32", "Unsigned32", "TimeTicks":
		return "number"
	case "OctetString":
		return "string"
	case "ObjectIdentifier", "IpAddress":
		return "string"
	case "Null", "NoSuchObject", "NoSuchInstance", "EndOfMibView":
		return "string"
	default:
		return "string"
	}
}

// sanitizeOidForSubject converts an OID to a valid NATS subject segment.
// ".1.3.6.1.2.1.1.1.0" -> "1_3_6_1_2_1_1_1_0"
func sanitizeOidForSubject(oid string) string {
	s := strings.TrimPrefix(oid, ".")
	r := strings.NewReplacer(".", "_")
	return r.Replace(s)
}
