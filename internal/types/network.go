package types

// NetworkAddress represents an IP address on an interface.
type NetworkAddress struct {
	Family    string `json:"family"` // "inet" or "inet6"
	Address   string `json:"address"`
	Prefixlen int    `json:"prefixlen"`
	Scope     string `json:"scope"`
	Broadcast string `json:"broadcast,omitempty"`
}

// NetworkInterfaceStats holds RX/TX counters from sysfs.
type NetworkInterfaceStats struct {
	RxBytes   int64 `json:"rxBytes"`
	TxBytes   int64 `json:"txBytes"`
	RxPackets int64 `json:"rxPackets"`
	TxPackets int64 `json:"txPackets"`
	RxErrors  int64 `json:"rxErrors"`
	TxErrors  int64 `json:"txErrors"`
	RxDropped int64 `json:"rxDropped"`
	TxDropped int64 `json:"txDropped"`
}

// NetworkInterface describes a single network interface.
type NetworkInterface struct {
	Name       string                 `json:"name"`
	Operstate  string                 `json:"operstate"`
	Carrier    bool                   `json:"carrier"`
	Speed      int                    `json:"speed,omitempty"`
	Duplex     string                 `json:"duplex,omitempty"`
	Mac        string                 `json:"mac"`
	Mtu        int                    `json:"mtu"`
	Type       int                    `json:"type"`
	Flags      []string               `json:"flags,omitempty"`
	Addresses  []NetworkAddress       `json:"addresses,omitempty"`
	Statistics *NetworkInterfaceStats `json:"statistics,omitempty"`
}

// NetworkStateMessage is the response to network.state and periodic publish on network.interfaces.
type NetworkStateMessage struct {
	ModuleID   string             `json:"moduleId"`
	Timestamp  int64              `json:"timestamp"`
	Interfaces []NetworkInterface `json:"interfaces"`
}

// NetworkInterfaceConfig is a netplan-style interface configuration.
type NetworkInterfaceConfig struct {
	InterfaceName string   `json:"interfaceName"`
	DHCP4         bool     `json:"dhcp4"`
	Addresses     []string `json:"addresses,omitempty"`  // CIDR format
	Gateway4      string   `json:"gateway4,omitempty"`
	Nameservers   []string `json:"nameservers,omitempty"`
	Mtu           int      `json:"mtu,omitempty"`
}

// NetworkCommandRequest is the request payload for network.command.
type NetworkCommandRequest struct {
	RequestID     string                   `json:"requestId"`
	Action        string                   `json:"action"` // "apply-config", "get-config", "add-address", "remove-address"
	Interfaces    []NetworkInterfaceConfig `json:"interfaces,omitempty"`
	InterfaceName string                   `json:"interfaceName,omitempty"`
	Address       string                   `json:"address,omitempty"`
	Timestamp     int64                    `json:"timestamp"`
}

// NetworkCommandResponse is the reply to network.command.
type NetworkCommandResponse struct {
	RequestID string                   `json:"requestId"`
	Success   bool                     `json:"success"`
	Error     string                   `json:"error,omitempty"`
	Config    []NetworkInterfaceConfig `json:"config,omitempty"`
	Timestamp int64                    `json:"timestamp"`
}
