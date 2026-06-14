//go:build !windows

package daemon

// resolveAgentViaNpmGlobal is a no-op on non-Windows platforms.
func resolveAgentViaNpmGlobal(_ string) string { return "" }
