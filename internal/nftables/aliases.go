//go:build nftables || all

package nftables

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/joyautomation/tentacle/internal/bus"
	"github.com/joyautomation/tentacle/internal/topics"
	itypes "github.com/joyautomation/tentacle/internal/types"
)

const (
	aliasesFile = "managed-aliases.json"
)

// aliasesPath returns the full path to the managed aliases state file.
func aliasesPath() string { return filepath.Join(configDir, aliasesFile) }

// loadManagedAliases reads the current managed aliases state from disk.
func loadManagedAliases() (*managedAliasesState, error) {
	data, err := os.ReadFile(aliasesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &managedAliasesState{}, nil
		}
		return nil, fmt.Errorf("read managed aliases: %w", err)
	}
	var state managedAliasesState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse managed aliases: %w", err)
	}
	return &state, nil
}

// saveManagedAliases writes the managed aliases state to disk.
func saveManagedAliases(state *managedAliasesState) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal managed aliases: %w", err)
	}
	if err := os.WriteFile(aliasesPath(), data, 0644); err != nil {
		return fmt.Errorf("write managed aliases: %w", err)
	}
	return nil
}

// syncAliases compares the required NAT aliases (derived from rule natAddr values)
// against the currently managed aliases, adding/removing via the network module
// as needed.
func syncAliases(log *slog.Logger, b bus.Bus, rules []itypes.NatRule) error {
	current, err := loadManagedAliases()
	if err != nil {
		log.Warn("nftables: failed to load managed aliases, starting fresh", "error", err)
		current = &managedAliasesState{}
	}

	// Build the desired alias set from enabled rules.
	type aliasKey struct{ iface, addr string }
	desired := make(map[aliasKey]bool)
	for _, r := range rules {
		if !r.Enabled || r.NatAddr == "" || r.IncomingInterface == "" {
			continue
		}
		// Use /32 for the alias address since it's a virtual IP.
		addr := r.NatAddr + "/32"
		desired[aliasKey{r.IncomingInterface, addr}] = true
	}

	// Build a set of what we currently manage.
	currentSet := make(map[aliasKey]bool, len(current.Aliases))
	for _, a := range current.Aliases {
		currentSet[aliasKey{a.InterfaceName, a.Address}] = true
	}

	// Remove aliases no longer needed.
	for _, a := range current.Aliases {
		key := aliasKey{a.InterfaceName, a.Address}
		if desired[key] {
			continue
		}
		log.Info("nftables: removing alias", "interface", a.InterfaceName, "address", a.Address)
		if err := requestNetworkCommand(b, "remove-address", a.InterfaceName, a.Address); err != nil {
			log.Error("nftables: failed to remove alias", "interface", a.InterfaceName, "address", a.Address, "error", err)
		}
	}

	// Add aliases that are needed but not yet managed.
	for key := range desired {
		if currentSet[key] {
			continue
		}
		log.Info("nftables: adding alias", "interface", key.iface, "address", key.addr)
		if err := requestNetworkCommand(b, "add-address", key.iface, key.addr); err != nil {
			log.Error("nftables: failed to add alias", "interface", key.iface, "address", key.addr, "error", err)
		}
	}

	// Save updated managed aliases state (what we now desire).
	newState := &managedAliasesState{}
	for key := range desired {
		newState.Aliases = append(newState.Aliases, managedAlias{
			InterfaceName: key.iface,
			Address:       key.addr,
		})
	}
	if err := saveManagedAliases(newState); err != nil {
		log.Error("nftables: failed to save managed aliases", "error", err)
		return err
	}

	return nil
}

// requestNetworkCommand sends an add-address or remove-address request to
// the network module via the Bus.
func requestNetworkCommand(b bus.Bus, action, interfaceName, address string) error {
	req := itypes.NetworkCommandRequest{
		Action:        action,
		InterfaceName: interfaceName,
		Address:       address,
		Timestamp:     time.Now().UnixMilli(),
	}
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal network command: %w", err)
	}
	resp, err := b.Request(topics.NetworkCommand, data, 10*time.Second)
	if err != nil {
		return fmt.Errorf("network command request: %w", err)
	}

	var cmdResp itypes.NetworkCommandResponse
	if err := json.Unmarshal(resp, &cmdResp); err != nil {
		return fmt.Errorf("parse network command response: %w", err)
	}
	if !cmdResp.Success {
		return fmt.Errorf("network command failed: %s", cmdResp.Error)
	}
	return nil
}
