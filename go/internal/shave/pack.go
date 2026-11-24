package shave

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// cmdPack fits the most relevant context into an explicit token budget.
func cmdPack(args []string) int {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	query := fs.String("q", "", "query used to rank lines")
	budget := fs.Int("budget", 4000, "max output tokens")
	jsonOut := fs.Bool("json", false, "json output")
	paths := parseFlagsInterspersed(fs, args)
	if len(paths) == 0 {
		paths = []string{"."}
	}
	if *budget < 1 {
		*budget = 4000
	}
	files := collectFiles(paths)
	tokens := strings.Fields(strings.ToLower(*query))
	var parts []string
	used := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		body := string(b)
		if len(tokens) > 0 {
			body = querySnippets(body, tokens, 2, 0)
			if body == "" {
				continue
			}
		}
		chunk := "## " + f + "\n" + body
		cost := tokenCount(chunk)
		if used+cost > *budget {
			remainChars := (*budget - used) * 4
			if remainChars > 80 && used < *budget {
				parts = append(parts, chunk[:remainChars]+"\n... [budget reached]\n")
			}
			break
		}
		parts = append(parts, chunk)
		used += cost
	}
	out := strings.Join(parts, "\n")
	if out == "" {
		out = "(nothing selected)\n"
	}
	cost := tokenCount(out)
	if *jsonOut {
		writeJSON(map[string]interface{}{
			"query": *query, "budget_tokens": *budget,
			"used_tokens": cost, "files": len(parts), "output": out,
		})
		return 0
	}
	fmt.Print(out)
	fmt.Fprintf(os.Stderr, "shave: ~%d/%d tokens from %d files\n", cost, *budget, len(parts))
	return 0
}
