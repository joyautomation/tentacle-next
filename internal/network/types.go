//go:build network || all

package network

// ipAddrEntry mirrors one element of `ip -j addr show` output.
type ipAddrEntry struct {
	IfName   string         `json:"ifname"`
	Flags    []string       `json:"flags"`
	AddrInfo []ipAddrInfo   `json:"addr_info"`
}

// ipAddrInfo mirrors one address inside an ip-addr JSON entry.
type ipAddrInfo struct {
	Family    string `json:"family"`
	Local     string `json:"local"`
	Prefixlen int    `json:"prefixlen"`
	Scope     string `json:"scope"`
	Broadcast string `json:"broadcast,omitempty"`
}

// netplanConfig is the top-level structure of a netplan YAML file.
type netplanConfig struct {
	Network netplanNetwork `yaml:"network"`
}

// netplanNetwork holds the network key within netplan YAML.
type netplanNetwork struct {
	Version   int                          `yaml:"version"`
	Ethernets map[string]netplanEthernet   `yaml:"ethernets,omitempty"`
}

// netplanEthernet holds per-interface netplan configuration.
type netplanEthernet struct {
	DHCP4       *bool                 `yaml:"dhcp4,omitempty"`
	Addresses   []string              `yaml:"addresses,omitempty"`
	Gateway4    string                `yaml:"gateway4,omitempty"`
	Nameservers *netplanNameservers   `yaml:"nameservers,omitempty"`
	MTU         int                   `yaml:"mtu,omitempty"`
}

// netplanNameservers holds DNS server configuration.
type netplanNameservers struct {
	Addresses []string `yaml:"addresses,omitempty"`
}
