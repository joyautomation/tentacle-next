//go:build !gitserver && !gitopsserver && !mantle && !all

// Stub for builds that don't compile the gitserver module. The package is
// imported unconditionally by code that may run on either edge or mantle
// builds; the stub keeps that compile path open by returning nils. Callers
// must guard against nil before using the server/store.
package gitserver

// In stub builds we don't have the gitops package compiled either, so we
// can't reference its types. Callers gated by a build tag matching the
// real package see the real signatures; nothing in stub-only builds
// reaches Server()/Store(), so opaque return types are sufficient.

func Get() any    { return nil }
func Server() any { return nil }
func Store() any  { return nil }
