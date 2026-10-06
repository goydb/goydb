//go:build !sqlite

package storage

// registerOptionalEngines is a no-op when built without `-tags sqlite` —
// bbolt (engine_bbolt.go) is the only engine available, and "engine":
// "sqlite" in CreateDatabase's args fails with ErrUnknownEngine.
func registerOptionalEngines(s *Storage) {}
