//go:build all

package main

import (
	"github.com/joyautomation/tentacle/internal/ethernetipserver"
	"github.com/joyautomation/tentacle/internal/gitserver"
	"github.com/joyautomation/tentacle/internal/history"
	"github.com/joyautomation/tentacle/internal/modbus"
	"github.com/joyautomation/tentacle/internal/modbusserver"
	"github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/mqttbroker"
	"github.com/joyautomation/tentacle/internal/nftables"
	"github.com/joyautomation/tentacle/internal/opcua"
	"github.com/joyautomation/tentacle/internal/orchestrator"
	"github.com/joyautomation/tentacle/internal/plc"
	"github.com/joyautomation/tentacle/internal/profinet"
	"github.com/joyautomation/tentacle/internal/profinetcontroller"
	"github.com/joyautomation/tentacle/internal/sparkplughost"
)

func experimentalFactories() map[string]orchestrator.ModuleFactory {
	return map[string]orchestrator.ModuleFactory{
		"opcua":              func(id string) module.Module { return opcua.New(id) },
		"modbus":             func(id string) module.Module { return modbus.New(id) },
		"profinet":           func(id string) module.Module { return profinet.New(id) },
		"profinetcontroller": func(id string) module.Module { return profinetcontroller.New(id) },
		"ethernetip-server":  func(id string) module.Module { return ethernetipserver.New(id) },
		"modbus-server":      func(id string) module.Module { return modbusserver.New(id) },
		"history":            func(id string) module.Module { return history.New(id) },
		"nftables":           func(id string) module.Module { return nftables.New(id) },
		"plc":                func(id string) module.Module { return plc.New(id) },
		"mqtt-broker":        func(id string) module.Module { return mqttbroker.New(id) },
		"sparkplug-host":     func(id string) module.Module { return sparkplughost.New(id) },
		"gitserver":          func(id string) module.Module { return gitserver.New(id) },
	}
}

func experimentalModuleByName(name string) module.Module {
	switch name {
	case "opcua":
		return opcua.New("opcua")
	case "modbus":
		return modbus.New("modbus")
	case "profinet":
		return profinet.New("profinet")
	case "profinetcontroller":
		return profinetcontroller.New("profinetcontroller")
	case "ethernetipserver":
		return ethernetipserver.New("ethernetip-server")
	case "modbusserver":
		return modbusserver.New("modbus-server")
	case "history":
		return history.New("history")
	case "nftables":
		return nftables.New("nftables")
	case "plc":
		return plc.New("plc")
	case "mqtt-broker":
		return mqttbroker.New("mqtt-broker")
	case "sparkplug-host":
		return sparkplughost.New("sparkplug-host")
	case "gitserver":
		return gitserver.New("gitserver")
	default:
		return nil
	}
}
