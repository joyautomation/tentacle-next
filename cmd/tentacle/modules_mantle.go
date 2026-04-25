//go:build mantle && !all

package main

import (
	"github.com/joyautomation/tentacle/internal/history"
	"github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/mqttbroker"
	"github.com/joyautomation/tentacle/internal/orchestrator"
	"github.com/joyautomation/tentacle/internal/sparkplughost"
)

func experimentalFactories() map[string]orchestrator.ModuleFactory {
	return map[string]orchestrator.ModuleFactory{
		"history":        func(id string) module.Module { return history.New(id) },
		"mqtt-broker":    func(id string) module.Module { return mqttbroker.New(id) },
		"sparkplug-host": func(id string) module.Module { return sparkplughost.New(id) },
	}
}

func experimentalModuleByName(name string) module.Module {
	switch name {
	case "history":
		return history.New("history")
	case "mqtt-broker":
		return mqttbroker.New("mqtt-broker")
	case "sparkplug-host":
		return sparkplughost.New("sparkplug-host")
	default:
		return nil
	}
}
