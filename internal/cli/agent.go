package cli

import (
	"flag"
	"os"
	"strings"

	"github.com/ttak0422/track/internal/track/agent"
)

// cmdAgent routes `track agent <sub>`.
func cmdAgent(args []string) int {
	if len(args) == 0 {
		return fail("usage: track agent ls | track agent log <sessionId> [--tail N]")
	}
	switch args[0] {
	case "ls":
		return cmdAgentLs(args[1:])
	case "log":
		return cmdAgentLog(args[1:])
	default:
		return fail("unknown agent subcommand %q (expected: ls, log)", args[0])
	}
}

// cmdAgentLs lists the live Claude Code sessions. It never touches the vault: agent state lives
// under $HOME/.claude, so the command works with no vault configured (ADR 0072).
func cmdAgentLs(args []string) int {
	if len(args) != 0 {
		return fail("agent ls: takes no arguments")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail("agent ls: %v", err)
	}
	sessions, err := agent.List(home)
	if err != nil {
		return fail("agent ls: %v", err)
	}
	return emit(map[string]any{"sessions": sessions})
}

// cmdAgentLog prints the tail of one session's transcript together with the session's latest
// ai-title and the PR it created. The transcript lives under $HOME/.claude/projects/, so like
// agent ls this command needs no vault.
func cmdAgentLog(args []string) int {
	fs := flag.NewFlagSet("agent log", flag.ContinueOnError)
	tail := fs.Int("tail", 50, "number of trailing user/assistant messages to return")
	// The contract is `track agent log <sessionId> [--tail N]`: the id is positional and may
	// precede flags, which the flag package otherwise stops parsing at. Reorder first.
	if code, ok := parseArgs(fs, reorderFlags(args), "usage: track agent log <sessionId> [--tail N]"); !ok {
		return code
	}
	if *tail <= 0 {
		return fail("agent log: --tail must be positive")
	}
	if fs.NArg() != 1 {
		return fail("agent log: want exactly one sessionId")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail("agent log: %v", err)
	}
	tr, err := agent.Log(home, fs.Arg(0), *tail)
	if err != nil {
		return fail("agent log: %v", err)
	}
	return emit(tr)
}

// reorderFlags moves positional arguments after the flags, keeping flag/value pairs intact, so a
// flag package that stops at the first positional can still parse `cmd <pos> --flag v`.
func reorderFlags(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1]) // the flag's value
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	return append(flags, pos...)
}
