// ABOUTME: CLI entrypoint for the mammoth-conformance binary that wraps mammoth's engine for AttractorBench.
// ABOUTME: Routes subcommands (parse, validate, run, list-handlers) to their respective implementations.
package main

import (
	"fmt"
	"os"
)

func main() {
	os.Exit(dispatch(os.Args))
}

// dispatch routes CLI arguments to subcommand handlers.
// Returns exit code: 0 for success, 1 for errors.
func dispatch(args []string) int {
	if len(args) < 2 {
		printUsage()
		return 1
	}
	switch args[1] {
	case "parse":
		if len(args) < 3 {
			writeError("parse requires a DOT file argument")
			return 1
		}
		return cmdParse(args[2])
	case "validate":
		if len(args) < 3 {
			writeError("validate requires a DOT file argument")
			return 1
		}
		return cmdValidate(args[2])
	case "run":
		if len(args) < 3 {
			writeError("run requires a DOT file argument")
			return 1
		}
		return cmdRun(args[2])
	case "list-handlers":
		return cmdListHandlers()
	default:
		writeError(fmt.Sprintf("unknown command: %s", args[1]))
		return 1
	}
}

// printUsage writes the usage message to stderr.
func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: mammoth-conformance <command> [args]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  parse <file.dot>       Parse a DOT file and output conformance JSON")
	fmt.Fprintln(os.Stderr, "  validate <file.dot>    Validate a DOT pipeline and output diagnostics")
	fmt.Fprintln(os.Stderr, "  run <file.dot>         Run a DOT pipeline and output results")
	fmt.Fprintln(os.Stderr, "  list-handlers          List available handler types")
}
