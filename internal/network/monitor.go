//go:build network || all

package network

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	itypes "github.com/joyautomation/tentacle/internal/types"
)

const sysClassNet = "/sys/class/net"

// readInterfaces enumerates all network interfaces from sysfs and enriches
// them with IP addresses from `ip -j addr show`.
func readInterfaces() ([]itypes.NetworkInterface, error) {
	entries, err := os.ReadDir(sysClassNet)
	if err != nil {
		return nil, fmt.Errorf("readdir %s: %w", sysClassNet, err)
	}

	addrMap, flagsMap := ipAddrMap()

	ifaces := make([]itypes.NetworkInterface, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		iface := itypes.NetworkInterface{
			Name:      name,
			Operstate: readSysfsString(name, "operstate"),
			Carrier:   readSysfsBool(name, "carrier"),
			Speed:     readSysfsInt(name, "speed"),
			Duplex:    readSysfsString(name, "duplex"),
			Mac:       readSysfsString(name, "address"),
			Mtu:       readSysfsInt(name, "mtu"),
			Type:      readSysfsInt(name, "type"),
		}

		if addrs, ok := addrMap[name]; ok {
			iface.Addresses = addrs
		}
		if flags, ok := flagsMap[name]; ok {
			iface.Flags = flags
		}

		stats := readStats(name)
		iface.Statistics = &stats

		ifaces = append(ifaces, iface)
	}

	return ifaces, nil
}

// readSysfsString reads a single sysfs attribute and returns the trimmed value.
func readSysfsString(iface, attr string) string {
	data, err := os.ReadFile(filepath.Join(sysClassNet, iface, attr))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readSysfsInt reads a sysfs attribute as an integer. Returns 0 on error.
func readSysfsInt(iface, attr string) int {
	s := readSysfsString(iface, attr)
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

// readSysfsBool reads a sysfs attribute as a boolean (1 = true).
func readSysfsBool(iface, attr string) bool {
	return readSysfsString(iface, attr) == "1"
}

// readSysfsInt64 reads a sysfs attribute as int64. Returns 0 on error.
func readSysfsInt64(iface, stat string) int64 {
	data, err := os.ReadFile(filepath.Join(sysClassNet, iface, "statistics", stat))
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// readStats reads interface statistics from sysfs.
func readStats(iface string) itypes.NetworkInterfaceStats {
	return itypes.NetworkInterfaceStats{
		RxBytes:   readSysfsInt64(iface, "rx_bytes"),
		TxBytes:   readSysfsInt64(iface, "tx_bytes"),
		RxPackets: readSysfsInt64(iface, "rx_packets"),
		TxPackets: readSysfsInt64(iface, "tx_packets"),
		RxErrors:  readSysfsInt64(iface, "rx_errors"),
		TxErrors:  readSysfsInt64(iface, "tx_errors"),
		RxDropped: readSysfsInt64(iface, "rx_dropped"),
		TxDropped: readSysfsInt64(iface, "tx_dropped"),
	}
}

// ipAddrMap runs `ip -j addr show` and returns maps from interface name to
// addresses and flags.
func ipAddrMap() (map[string][]itypes.NetworkAddress, map[string][]string) {
	addrMap := make(map[string][]itypes.NetworkAddress)
	flagsMap := make(map[string][]string)

	out, err := exec.Command("ip", "-j", "addr", "show").Output()
	if err != nil {
		slog.Warn("network: ip addr show failed", "error", err)
		return addrMap, flagsMap
	}

	var entries []ipAddrEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		slog.Warn("network: failed to parse ip addr JSON", "error", err)
		return addrMap, flagsMap
	}

	for _, e := range entries {
		flagsMap[e.IfName] = e.Flags
		for _, ai := range e.AddrInfo {
			addrMap[e.IfName] = append(addrMap[e.IfName], itypes.NetworkAddress{
				Family:    ai.Family,
				Address:   ai.Local,
				Prefixlen: ai.Prefixlen,
				Scope:     ai.Scope,
				Broadcast: ai.Broadcast,
			})
		}
	}

	return addrMap, flagsMap
}
