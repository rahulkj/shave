package shave

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// parseFlagsInterspersed accepts flags before or after positional arguments.
// Go's flag package stops at the first positional argument, so a natural call
// like `shave github ~/src --json` would otherwise ignore the trailing flags.
func parseFlagsInterspersed(fs *flag.FlagSet, args []string) []string {
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, "shave:", err)
		return fs.Args()
	}
	var positional []string
	rest := fs.Args()
	for len(rest) > 0 {
		if !strings.HasPrefix(rest[0], "-") {
			positional = append(positional, rest[0])
			rest = rest[1:]
			continue
		}
		if err := fs.Parse(rest); err != nil {
			fmt.Fprintln(os.Stderr, "shave:", err)
			break
		}
		rest = fs.Args()
	}
	return positional
}
