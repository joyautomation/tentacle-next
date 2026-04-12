//go:build profinet || profinetcontroller || all

package profinet

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// ApplyInterfaceIP flushes all existing IPv4 addresses from the named interface
// and adds the given IP/mask. If gateway is non-zero, it adds a default route.
func ApplyInterfaceIP(ifaceName string, ip, mask, gateway net.IP) error {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("netlink: interface %s not found: %w", ifaceName, err)
	}

	// Ensure link is UP
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("netlink: failed to bring up %s: %w", ifaceName, err)
	}

	// Flush existing IPv4 addresses
	existing, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("netlink: failed to list addresses on %s: %w", ifaceName, err)
	}
	for _, addr := range existing {
		if err := netlink.AddrDel(link, &addr); err != nil {
			return fmt.Errorf("netlink: failed to remove address %s from %s: %w", addr.IPNet, ifaceName, err)
		}
	}

	// Build the new address
	ipNet := &net.IPNet{
		IP:   ip.To4(),
		Mask: net.IPMask(mask.To4()),
	}
	if err := netlink.AddrAdd(link, &netlink.Addr{IPNet: ipNet}); err != nil {
		return fmt.Errorf("netlink: failed to add address %s to %s: %w", ipNet, ifaceName, err)
	}

	// Add default route via gateway if provided and non-zero
	if gateway != nil && !gateway.Equal(net.IPv4zero) {
		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Gw:        gateway.To4(),
		}
		// Remove existing default route on this interface first (ignore errors)
		_ = netlink.RouteDel(route)
		if err := netlink.RouteAdd(route); err != nil {
			// Gateway route failure is non-fatal — the IP itself was set
			// The caller can log this but shouldn't fail the DCP SET
		}
	}

	return nil
}

// RemoveInterfaceIP removes a specific IPv4 address from the named interface.
func RemoveInterfaceIP(ifaceName string, ip, mask net.IP) error {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("netlink: interface %s not found: %w", ifaceName, err)
	}

	ipNet := &net.IPNet{
		IP:   ip.To4(),
		Mask: net.IPMask(mask.To4()),
	}
	if err := netlink.AddrDel(link, &netlink.Addr{IPNet: ipNet}); err != nil {
		return fmt.Errorf("netlink: failed to remove address %s from %s: %w", ipNet, ifaceName, err)
	}

	return nil
}
