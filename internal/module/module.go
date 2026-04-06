// Package module defines the lifecycle interface that all tentacle modules implement.
package module

import (
	"context"

	"github.com/joyautomation/tentacle/internal/bus"
)

// Module is the interface implemented by all tentacle modules.
// The monolith entrypoint starts multiple modules as goroutines;
// standalone binaries start exactly one.
type Module interface {
	// Start initializes the module and begins processing.
	// It should block until ctx is cancelled or Stop is called.
	Start(ctx context.Context, b bus.Bus) error

	// Stop gracefully shuts down the module.
	Stop() error

	// ModuleID returns the unique identifier for this module instance.
	ModuleID() string

	// ServiceType returns the service type (e.g., "ethernetip", "gateway").
	ServiceType() string
}
