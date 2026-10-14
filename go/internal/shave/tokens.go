package shave

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// model pricing $/1M tokens (blended input). Conservative defaults.
var modelPrices = map[string]float64{
	"gpt-4o":           2.50,
	"gpt-4o-mini":      0.15,
	"gpt-4.1":          2.00,
	"claude-sonnet-4":  3.00,
	"claude-haiku-4":   0.80,
	"claude-opus-4":    15.00,
	"gemini-2.5-pro":   1.25,
	"gemini-2.5-flash": 0.075,
	"local":            0.00,
}

func cmdTokens(args []string) int {
	fs := flag.NewFlagSet("tokens", flag.ContinueOnError)
	model := fs.String("m", "claude-sonnet-4", "model name for cost estimate")
	jsonOut := fs.Bool("json", false, "json output")
	_ = fs.Parse(args)

	var r io.Reader = os.Stdin
	if fs.NArg() > 0 {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "shave:", err)
			return 1
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shave:", err)
		return 1
	}
	text := string(b)
	chars := len(text)
	// rough token estimate: ~4 chars per token for English code.
	toks := (chars + 3) / 4
	if chars == 0 {
		toks = 0
	}
	price, ok := modelPrices[*model]
	if !ok {
		price = 3.00
	}
	cost := float64(toks) / 1_000_000.0 * price
	// lines / words for context
	lines := strings.Count(text, "\n")
	if len(text) > 0 && !strings.HasSuffix(text, "\n") {
		lines++
	}

	if *jsonOut {
		out := map[string]interface{}{
			"chars":  chars,
			"tokens": toks,
			"lines":  lines,
			"model":  *model,
			"usd":    cost,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return 0
	}
	fmt.Printf("chars:   %d\n", chars)
	fmt.Printf("tokens:  ~%d (chars/4 heuristic)\n", toks)
	fmt.Printf("lines:   %d\n", lines)
	fmt.Printf("model:   %s ($%.2f/M)\n", *model, price)
	fmt.Printf("est usd: $%.4f\n", cost)
	return 0
}
