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
)

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
	}
}
