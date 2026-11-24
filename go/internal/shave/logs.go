package shave

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

var (
	reSecret = regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|api[_-]?key|authorization|bearer)\s*[:=]\s*\S+`)
	reLong   = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
	reHex    = regexp.MustCompile(`\b[0-9a-fA-F]{33,}\b`)
)

func redactLine(s string) string {
	s = reSecret.ReplaceAllString(s, "$1: [REDACTED]")
	s = reLong.ReplaceAllString(s, "[REDACTED]")
	s = reHex.ReplaceAllString(s, "[REDACTED]")
	return s
}

func isInteresting(line string) bool {
	l := strings.ToLower(line)
	for _, k := range []string{"error", "warn", "fatal", "panic", "exception", "fail", "traceback", "crash", "timeout", "refused", "denied", "not found", "oom", "killed"} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…[truncated]"
}

func cmdLogs(args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	errorsOnly := fs.Bool("errors", false, "keep only error/warn-relevant lines (+context)")
	contextN := fs.Int("context", 3, "context lines around matches (errors mode)")
	maxLines := fs.Int("max", 300, "max output lines")
	noRedact := fs.Bool("no-redact", false, "disable secret redaction")
	jsonOut := fs.Bool("json", false, "json output")
	file := fs.String("f", "", "input file (default stdin)")
	_ = fs.Parse(args)

	var r io.Reader = os.Stdin
	if *file != "" {
		f, err := os.Open(*file)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shave:", err)
			return 1
		}
		defer f.Close()
		r = f
	}
	// also allow positional file
	if fs.NArg() > 0 && *file == "" {
		f, err := os.Open(fs.Arg(0))
		if err != nil {
			fmt.Fprintln(os.Stderr, "shave:", err)
			return 1
		}
		defer f.Close()
		r = f
	}

	var lines []string
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	final, linesIn, omitted := processLogs(lines, *errorsOnly, *contextN, *maxLines, !*noRedact)

	if *jsonOut {
		out := map[string]interface{}{
			"lines_in":  linesIn,
			"lines_out": len(final),
			"omitted":   omitted,
			"output":    final,
		}
		writeJSON(out)
		return 0
	}
	for _, l := range final {
		fmt.Println(l)
	}
	if omitted > 0 {
		fmt.Fprintf(os.Stderr, "shave: %d more lines omitted (use --max)\n", omitted)
	}
	if linesIn > 0 {
		ratio := float64(len(final)) / float64(linesIn) * 100
		fmt.Fprintf(os.Stderr, "shave: %d -> %d lines (%.0f%%)\n", linesIn, len(final), ratio)
	}
	return 0
}

// processLogs is the shared compression pipeline: error-focused filtering,
// redaction, truncation, consecutive-dupe collapsing and a hard line cap.
// Every filter command (logs, test, diff) calls this rather than copying it.
func processLogs(lines []string, errorsOnly bool, contextN, max int, redact bool) (final []string, linesIn, omitted int) {
	linesIn = len(lines)
	outLines := []string{}
	if errorsOnly {
		keep := make([]bool, len(lines))
		for i, l := range lines {
			if isInteresting(l) {
				for j := i - contextN; j <= i+contextN; j++ {
					if j >= 0 && j < len(lines) {
						keep[j] = true
					}
				}
			}
		}
		for i, l := range lines {
			if keep[i] {
				outLines = append(outLines, l)
			}
		}
	} else {
		outLines = lines
	}
	type runItem struct {
		text  string
		count int
	}
	var collapsed []runItem
	for _, l := range outLines {
		t := l
		if redact {
			t = redactLine(t)
		}
		t = trunc(strings.TrimRight(t, " \t"), 500)
		if len(collapsed) > 0 && collapsed[len(collapsed)-1].text == t {
			collapsed[len(collapsed)-1].count++
		} else {
			collapsed = append(collapsed, runItem{text: t, count: 1})
		}
	}
	for _, it := range collapsed {
		if it.count > 1 {
			final = append(final, fmt.Sprintf("%s   (x%d)", it.text, it.count))
		} else {
			final = append(final, it.text)
		}
	}
	if len(final) > max {
		omitted = len(final) - max
		final = final[:max]
	}
	return final, linesIn, omitted
}
