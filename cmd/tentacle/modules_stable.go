//go:build all || stable

package main

import (
	"github.com/joyautomation/tentacle/internal/caddy"
	"github.com/joyautomation/tentacle/internal/ethernetip"
	"github.com/joyautomation/tentacle/internal/gateway"
	"github.com/joyautomation/tentacle/internal/gitops"
	"github.com/joyautomation/tentacle/internal/hmi"
	"github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/mqtt"
	"github.com/joyautomation/tentacle/internal/network"
	"github.com/joyautomation/tentacle/internal/orchestrator"
	"github.com/joyautomation/tentacle/internal/snmp"
	"github.com/joyautomation/tentacle/internal/telemetry"
)

func stableFactories() map[string]orchestrator.ModuleFactory {
	return map[string]orchestrator.ModuleFactory{
		"gateway":    func(id string) module.Module { return gateway.New(id) },
		"ethernetip": func(id string) module.Module { return ethernetip.New(id) },
		"snmp":       func(id string) module.Module { return snmp.New(id) },
		"mqtt":       func(id string) module.Module { return mqtt.New(id) },
		"network":    func(id string) module.Module { return network.New(id) },
		"gitops":     func(id string) module.Module { return gitops.New(id) },
		"caddy":      func(id string) module.Module { return caddy.New(id) },
		"telemetry":  func(id string) module.Module { return telemetry.New(id) },
		"hmi":        func(id string) module.Module { return hmi.New(id) },
	}
}

func stableModuleByName(name string) module.Module {
	switch name {
	case "gateway":
		return gateway.New("gateway")
	case "ethernetip":
		return ethernetip.New("ethernetip")
	case "snmp":
		return snmp.New("snmp")
	case "mqtt":
		return mqtt.New("mqtt")
	case "network":
		return network.New("network")
	case "gitops":
		return gitops.New("gitops")
	case "caddy":
		return caddy.New("caddy")
	case "telemetry":
		return telemetry.New("telemetry")
	case "hmi":
		return hmi.New("hmi")
	case "api":
		return nil // api is a core module, started directly in runMonolith
	case "orchestrator":
		return nil // orchestrator is a core module, started directly in runMonolith
	default:
		return nil
	}
}
