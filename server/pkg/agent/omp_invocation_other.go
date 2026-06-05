//go:build !windows

package agent

import "log/slog"

// platformOmpInvocation is a no-op on non-Windows platforms.
func platformOmpInvocation(_ string, _ []string, _ *slog.Logger) (string, []string, bool) {
	return "", nil, false
}
