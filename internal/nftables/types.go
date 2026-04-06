//go:build nftables || all

package nftables

// managedAliasesState tracks IP aliases that have been added by the nftables
// module so they can be cleaned up when rules change.
type managedAliasesState struct {
	Aliases []managedAlias `json:"aliases"`
}

// managedAlias records a single IP alias that was added to an interface.
type managedAlias struct {
	InterfaceName string `json:"interfaceName"`
	Address       string `json:"address"` // CIDR notation, e.g. "192.168.1.100/24"
}
