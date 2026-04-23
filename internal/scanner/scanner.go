// Package scanner owns the contract between consumers (gateway, PLC) and the
// per-protocol scanner modules that actually talk to devices.
//
// Protocol scanners (ethernetip, opcua, modbus, snmp, profinetcontroller) each
// watch their own KV bucket for subscribe requests and publish values on
// {protocol}.data.{deviceId}.{tag}. Consumers that need device data write
// subscribe requests through this package and subscribe to the data topics
// directly.
//
// Phase 1 of the scanner module extraction: this package is pure helpers —
// wire formats, KV names, and bucket membership are unchanged.
package scanner
