package types

// NatRule defines a single NAT port-forwarding rule.
type NatRule struct {
	ID                string `json:"id"`
	Enabled           bool   `json:"enabled"`
	Protocol          string `json:"protocol"`          // "tcp", "udp", "icmp", "all"
	ConnectingDevices string `json:"connectingDevices"` // "any" or IP/CIDR
	IncomingInterface string `json:"incomingInterface"`
	OutgoingInterface string `json:"outgoingInterface"`
	NatAddr           string `json:"natAddr"`
	OriginalPort      string `json:"originalPort"`
	TranslatedPort    string `json:"translatedPort"`
	DeviceAddr        string `json:"deviceAddr"`
	DeviceName        string `json:"deviceName"`
	DoubleNat         bool   `json:"doubleNat"`
	DoubleNatAddr     string `json:"doubleNatAddr,omitempty"`
	Comment           string `json:"comment,omitempty"`
}

// NftablesConfig holds the full nftables NAT configuration.
type NftablesConfig struct {
	NatRules []NatRule `json:"natRules"`
}

// NftablesStateMessage is the response to nftables.state and periodic publish on nftables.rules.
type NftablesStateMessage struct {
	ModuleID   string `json:"moduleId"`
	Timestamp  int64  `json:"timestamp"`
	RawRuleset string `json:"rawRuleset"` // Raw JSON from nft -j list ruleset
}

// NftablesCommandRequest is the request payload for nftables.command.
type NftablesCommandRequest struct {
	RequestID string    `json:"requestId"`
	Action    string    `json:"action"` // "get-config", "apply-config"
	NatRules  []NatRule `json:"natRules,omitempty"`
	Timestamp int64     `json:"timestamp"`
}

// NftablesCommandResponse is the reply to nftables.command.
type NftablesCommandResponse struct {
	RequestID string          `json:"requestId"`
	Success   bool            `json:"success"`
	Error     string          `json:"error,omitempty"`
	Config    *NftablesConfig `json:"config,omitempty"`
	Timestamp int64           `json:"timestamp"`
}
