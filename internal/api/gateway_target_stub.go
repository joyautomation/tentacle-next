//go:build (api || all) && !gitopsserver && !mantle && !all

package api

import (
	"errors"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

// errRemoteTargetUnsupported is returned by stub builds when a request
// arrives with ?target=group/node on a tentacle that wasn't built with
// the mantle preset. Edge tentacles never see remote-targeted configurator
// requests in normal operation, so this just guards against misconfigured
// callers.
var errRemoteTargetUnsupported = errors.New("remote targets are only supported on mantle builds")

func (m *Module) loadGatewayConfigForTarget(_ Target, _ string) (*itypes.GatewayConfigKV, error) {
	return nil, errRemoteTargetUnsupported
}

func (m *Module) saveGatewayConfigForTarget(_ Target, _ *itypes.GatewayConfigKV, _ string) error {
	return errRemoteTargetUnsupported
}
