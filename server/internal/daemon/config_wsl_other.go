//go:build !windows

package daemon

// resolveAgentsViaWSL is a no-op on non-Windows platforms.
func resolveAgentsViaWSL(_ []string) map[string]string { return nil }

// wslPathExists always returns false on non-Windows platforms.
func wslPathExists(_ string) bool { return false }
