package shave

import (
	"fmt"
	"os"
)

const helpText = `shave — spend fewer AI tokens on coding, k8s and logs

Compress, filter and summarise the inputs you hand to an AI before you hand
them over. Stdin-friendly: pipe kubectl/get/logs/code into it and paste what
comes out.

  version                      Build version
  help [cmd]                   Show help

  tokens [file] [-m model]      Count tokens (approx chars/4) and est. cost
  logs [file]                   Compress a log stream (dedupe, redact, focus)
  test [file]                   Test output: hide passes, keep failures + context
  diff [file] [--review]        Compress a unified diff (--review: changes that matter)
  pack <paths...> [-q query]    Fit the most relevant context into a token budget
  k8s pods [-n ns]             Compact pod status table
  k8s events [-n ns]            Dedup + compact events
  k8s diagnose <kind/name>      Gather status/events/logs into one report
  ctx <paths...> [-q query]     Prune code context to relevant snippets (--map: outline)
  cache get <key>               Read a cached entry
  cache set <key>              Write a cached entry from stdin
  cache stats                   Cache size + hit counts
  cache clear                  Clear cache

  run -- <cmd>                 Run cmd, cache output, print diff on repeat
  github [paths...]            Analyze local git clones: inventory, shared patterns, duplicates
  mcp                          Run as an MCP server (stdio JSON-RPC)

Flags (most commands): --json, --max, --terse, --redact/--no-redact
Environment: SHAVE_TERSE=1 for agent-sized tab output

Examples:
  kubectl logs deploy/api -n prod | shave logs --errors
  kubectl get pods -A -o json | shave k8s pods --json
  shave ctx ./src -q "retry logic" --budget 2000
  git diff | shave tokens
  go test ./... | shave test
  git diff | shave diff --review
  shave pack ./src -q "retry logic" --budget 3000
  shave github ~/src --depth 2 --top 10
  shave github ~/src --find "retry logic"
  shave github ~/src --profile
`

func Main(args []string) int {
	if len(args) == 0 {
		fmt.Print(helpText)
		return 0
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("shave %s\n", Version)
		return 0
	case "help", "-h", "--help":
		fmt.Print(helpText)
		return 0
	case "tokens":
		return cmdTokens(args[1:])
	case "logs":
		return cmdLogs(args[1:])
	case "test":
		return cmdTest(args[1:])
	case "diff":
		return cmdDiff(args[1:])
	case "pack":
		return cmdPack(args[1:])
	case "k8s":
		return cmdK8s(args[1:])
	case "ctx":
		return cmdCtx(args[1:])
	case "cache":
		return cmdCache(args[1:])
	case "run":
		return cmdRun(args[1:])
	case "github":
		return cmdGithub(args[1:])
	case "mcp":
		return cmdMCP(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "shave: unknown command %q\n%s\n", args[0], helpText)
		return 2
	}
}

var Version = "0.2.0"
var Commit = "dev"
var Date = "unknown"
