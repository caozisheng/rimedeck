//go:build windows

package agent

import "log/slog"

// platformOmpInvocation rewrites omp.cmd → PowerShell -File omp.ps1 on
// Windows to avoid cmd.exe %* re-tokenisation (same issue as pi, see #3306).
func platformOmpInvocation(lookedUp string, args []string, logger *slog.Logger) (string, []string, bool) {
	return rewriteCmdToPS1("omp", lookedUp, args, logger)
}
