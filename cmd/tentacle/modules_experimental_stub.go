//go:build stable && !all && !mantle

package main

import (
	"github.com/joyautomation/tentacle/internal/module"
	"github.com/joyautomation/tentacle/internal/orchestrator"
)

func experimentalFactories() map[string]orchestrator.ModuleFactory {
	return nil
}

func experimentalModuleByName(_ string) module.Module {
	return nil
}
