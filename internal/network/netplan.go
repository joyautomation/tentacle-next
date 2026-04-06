//go:build network || all

package network

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	itypes "github.com/joyautomation/tentacle/internal/types"
	"gopkg.in/yaml.v3"
)

const (
	netplanDir  = "/etc/netplan"
	tentacleCfg = "60-tentacle.yaml"
)

// readConfig reads all netplan YAML files in /etc/netplan and returns a
// merged list of interface configurations.
func readConfig() ([]itypes.NetworkInterfaceConfig, error) {
	entries, err := os.ReadDir(netplanDir)
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", netplanDir, err)
	}

	var configs []itypes.NetworkInterfaceConfig

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(netplanDir, name))
		if err != nil {
			slog.Warn("network: failed to read netplan file", "file", name, "error", err)
			continue
		}

		var cfg netplanConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			slog.Warn("network: failed to parse netplan file", "file", name, "error", err)
			continue
		}

		for ifName, eth := range cfg.Network.Ethernets {
			ic := itypes.NetworkInterfaceConfig{
				InterfaceName: ifName,
				Addresses:     eth.Addresses,
				Gateway4:      eth.Gateway4,
				Mtu:           eth.MTU,
			}
			if eth.DHCP4 != nil {
				ic.DHCP4 = *eth.DHCP4
			}
			if eth.Nameservers != nil {
				ic.Nameservers = eth.Nameservers.Addresses
			}
			configs = append(configs, ic)
		}
	}

	return configs, nil
}

// applyConfig writes the given interface configurations to the tentacle
// netplan file and runs `netplan apply`.
func applyConfig(interfaces []itypes.NetworkInterfaceConfig) error {
	ethernets := make(map[string]netplanEthernet, len(interfaces))
	for _, ic := range interfaces {
		eth := netplanEthernet{
			Addresses: ic.Addresses,
			Gateway4:  ic.Gateway4,
			MTU:       ic.Mtu,
		}
		if ic.DHCP4 {
			t := true
			eth.DHCP4 = &t
		} else {
			f := false
			eth.DHCP4 = &f
		}
		if len(ic.Nameservers) > 0 {
			eth.Nameservers = &netplanNameservers{Addresses: ic.Nameservers}
		}
		ethernets[ic.InterfaceName] = eth
	}

	cfg := netplanConfig{
		Network: netplanNetwork{
			Version:   2,
			Ethernets: ethernets,
		},
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("marshal netplan yaml: %w", err)
	}

	path := filepath.Join(netplanDir, tentacleCfg)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	slog.Info("network: wrote netplan config", "path", path)

	out, err := exec.Command("netplan", "apply").CombinedOutput()
	if err != nil {
		return fmt.Errorf("netplan apply: %s: %w", strings.TrimSpace(string(out)), err)
	}

	slog.Info("network: netplan apply succeeded")
	return nil
}

// addAddress adds an IP address alias to an interface using ip addr add.
func addAddress(interfaceName, address string) error {
	out, err := exec.Command("ip", "addr", "add", address, "dev", interfaceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip addr add %s dev %s: %s: %w", address, interfaceName, strings.TrimSpace(string(out)), err)
	}
	slog.Info("network: added address", "interface", interfaceName, "address", address)
	return nil
}

// removeAddress removes an IP address from an interface using ip addr del.
func removeAddress(interfaceName, address string) error {
	out, err := exec.Command("ip", "addr", "del", address, "dev", interfaceName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip addr del %s dev %s: %s: %w", address, interfaceName, strings.TrimSpace(string(out)), err)
	}
	slog.Info("network: removed address", "interface", interfaceName, "address", address)
	return nil
}
