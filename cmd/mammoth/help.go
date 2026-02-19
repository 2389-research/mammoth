// ABOUTME: Help display for the mammoth CLI with grouped flags, examples, and environment status.
// ABOUTME: Provides printHelp for polished usage output and envStatus for API key detection.
package main

import (
	"fmt"
	"io"
	"os"
)

const mammothASCII = `
                             _.-----.._____,-~~~~-._...__
                          ,-'            /         ` + "`" + `....
                        ,'             ,'      .  .  \::.
                      ,'        . ''    :     . \  ` + "`" + `./::..
                    ,'    ..   .     .      .  . : ;':::.
                   /     :go. :       . :    \ : ;'.::.
                   |     ' .o8)     .  :|    : ,'. .
                  /     :   ~:'  . '   :/  . :/. .
                 /       ,  '          |   : /. .
                /       ,              |   ./.
                L._    .       ,' .:.  /  ,'.
               /-.     :.--._,-'~~~~~~| ,'|:
              ,--.    /   .:/         |/::| ` + "`" + `.
              |-.    /   .;'      .-__)::/    \
 ...._____...-|-.  ,'  .;'      .' '.'|;'      |
   ~--..._____\-_-'  .:'      .'   /  '
    ___....--~~   _.-' ` + "`" + `.___.'   ./
      ~~------+~~_. .    ~~    .,'
                  ~:_.' . . ._:'
                     ~~-+-+~~
`

// printHelp writes a formatted help message to w, including usage patterns,
// grouped flags, examples, environment status, and a docs link.
func printHelp(w io.Writer, ver string) {
	fmt.Fprint(w, mammothASCII)
	fmt.Fprintf(w, "mammoth %s — DOT-based AI pipeline runner\n", ver)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  mammoth [run] <pipeline.dot>        Run a pipeline")
	fmt.Fprintln(w, "  mammoth -validate <pipeline.dot>    Validate without executing")
	fmt.Fprintln(w, "  mammoth -server [-port 2389]        Start HTTP API server")
	fmt.Fprintln(w, "  mammoth serve              Start web UI (local mode: CWD is project root)")
	fmt.Fprintln(w, "  mammoth serve --global     Start web UI (global mode: ~/.local/share/mammoth)")
	fmt.Fprintln(w, "  mammoth setup                       Interactive setup wizard")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Pipeline Flags:")
	fmt.Fprintln(w, "  -retry <policy>       none, standard, aggressive, linear, patient (default: none)")
	fmt.Fprintln(w, "  -checkpoint-dir <dir> Directory for checkpoint files")
	fmt.Fprintln(w, "  -artifact-dir <dir>   Directory for artifact storage (default: current directory)")
	fmt.Fprintln(w, "  -data-dir <dir>       Persistent state directory (default: .mammoth/ in CWD)")
	fmt.Fprintln(w, "  -base-url <url>       Custom API base URL for the LLM provider")
	fmt.Fprintln(w, "  -tui                  Run with interactive terminal UI")
	fmt.Fprintln(w, "  -verbose              Verbose output")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Server Flags:")
	fmt.Fprintln(w, "  -server               Start HTTP server mode")
	fmt.Fprintln(w, "  -port <port>          Server port (default: 2389)")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Other:")
	fmt.Fprintln(w, "  -validate             Validate pipeline without executing")
	fmt.Fprintln(w, "  -version              Print version and exit")
	fmt.Fprintln(w, "  -help                 Show this help")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  mammoth examples/simple.dot")
	fmt.Fprintln(w, "  mammoth -validate my_pipeline.dot")
	fmt.Fprintln(w, "  mammoth -tui examples/build_pong.dot")
	fmt.Fprintln(w, "  mammoth -server -port 8080")
	fmt.Fprintln(w, "  mammoth -retry aggressive examples/full_pipeline.dot")
	fmt.Fprintln(w, "  mammoth serve --port 3000")
	fmt.Fprintln(w, "  mammoth serve --global --port 3000")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Environment:")
	fmt.Fprintf(w, "  ANTHROPIC_API_KEY     %s\n", envStatus("ANTHROPIC_API_KEY"))
	fmt.Fprintf(w, "  OPENAI_API_KEY        %s\n", envStatus("OPENAI_API_KEY"))
	fmt.Fprintf(w, "  GEMINI_API_KEY        %s\n", envStatus("GEMINI_API_KEY"))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  At least one API key is required for pipeline execution.")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "Docs: https://github.com/2389-research/mammoth")
}

// envStatus returns "[set]" if the named environment variable is non-empty,
// or "[not set]" otherwise.
func envStatus(key string) string {
	if os.Getenv(key) != "" {
		return "[set]"
	}
	return "[not set]"
}
