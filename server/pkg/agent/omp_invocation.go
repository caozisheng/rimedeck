package agent

import "log/slog"

// chooseOmpInvocation selects the actual program (argv[0]) and the full
// argv to spawn an OMP run. OMP is a fork of Pi and ships the same
// npm-style launchers, so the same .cmd/.ps1 rewrite logic applies on
// Windows. See choosePiInvocation for the rationale.
func chooseOmpInvocation(execName, lookedUp string, args []string, logger *slog.Logger) (string, []string) {
	if argv0, full, ok := platformOmpInvocation(lookedUp, args, logger); ok {
		return argv0, full
	}
	return execName, args
}
