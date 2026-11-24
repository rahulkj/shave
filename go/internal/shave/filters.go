package shave

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// readInputLines is the shared stdin/file reader for filter commands.
func readInputLines(path string) ([]string, error) {
	var r io.Reader = os.Stdin
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

// cmdTest collapses passing test output and keeps only failures + context.
func cmdTest(args []string) int {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	max := fs.Int("max", 60, "max output lines")
	contextN := fs.Int("context", 2, "context lines around failures")
	noRedact := fs.Bool("no-redact", false, "disable secret redaction")
	jsonOut := fs.Bool("json", false, "json output")
	file := fs.String("f", "", "input file (default stdin)")
	paths := parseFlagsInterspersed(fs, args)
	path := *file
	if path == "" && len(paths) > 0 {
		path = paths[0]
	}
	lines, err := readInputLines(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shave:", err)
		return 1
	}
	kept, passes := collapsePasses(lines)
	final, in, omitted := processLogs(kept, true, *contextN, *max, !*noRedact)
	if *jsonOut {
		writeJSON(map[string]interface{}{
			"lines_in": in, "pass_lines_hidden": passes,
			"lines_out": len(final), "omitted": omitted, "output": final,
		})
		return 0
	}
	fmt.Printf("[test: %d pass lines hidden, %d -> %d lines]\n", passes, in, len(final))
	for _, l := range final {
		fmt.Println(l)
	}
	if omitted > 0 {
		fmt.Fprintf(os.Stderr, "shave: %d more lines omitted (use --max)\n", omitted)
	}
	return 0
}

func collapsePasses(lines []string) ([]string, int) {
	var out []string
	passes := 0
	for _, l := range lines {
		low := strings.ToLower(strings.TrimSpace(l))
		if strings.HasPrefix(low, "ok ") || strings.HasPrefix(low, "--- pass") ||
			strings.HasPrefix(low, "=== run") || strings.HasPrefix(low, "=== name") ||
			low == "pass" || low == "pass!" {
			passes++
			continue
		}
		out = append(out, l)
	}
	return out, passes
}

// cmdDiff compresses a unified diff. --review keeps only declaration changes,
// secret-looking additions and TODOs, which is what a reviewer must see.
func cmdDiff(args []string) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	review := fs.Bool("review", false, "review mode: only declarations, secrets and TODOs")
	max := fs.Int("max", 80, "max output lines")
	noRedact := fs.Bool("no-redact", false, "disable secret redaction")
	jsonOut := fs.Bool("json", false, "json output")
	file := fs.String("f", "", "input file (default stdin)")
	paths := parseFlagsInterspersed(fs, args)
	path := *file
	if path == "" && len(paths) > 0 {
		path = paths[0]
	}
	lines, err := readInputLines(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shave:", err)
		return 1
	}
	out := compressDiff(lines, *review, !*noRedact, *max)
	if *jsonOut {
		writeJSON(map[string]interface{}{"lines_in": len(lines), "lines_out": len(out), "output": out})
		return 0
	}
	for _, l := range out {
		fmt.Println(l)
	}
	fmt.Fprintf(os.Stderr, "shave: %d -> %d lines\n", len(lines), len(out))
	return 0
}

func compressDiff(lines []string, review, redact bool, max int) []string {
	var out []string
	file := ""
	added, removed := 0, 0
	flush := func() {
		if file != "" {
			out = append(out, fmt.Sprintf("[%s] +%d -%d", file, added, removed))
		}
	}
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "+++ "):
			flush()
			file = strings.TrimPrefix(l, "+++ ")
			added, removed = 0, 0
		case strings.HasPrefix(l, "@@") && file != "" && !skipDiffFile(file):
			out = append(out, file+" "+l)
		case strings.HasPrefix(l, "+") && !strings.HasPrefix(l, "+++"):
			added++
			if file == "" || skipDiffFile(file) {
				continue
			}
			body := strings.TrimPrefix(l, "+")
			if review && !isDeclaration(body) && !looksSecret(body) &&
				!strings.Contains(strings.ToUpper(body), "TODO") && !strings.Contains(strings.ToUpper(body), "FIXME") {
				continue
			}
			out = append(out, file+": +"+cleanText(body, redact, 200))
		case strings.HasPrefix(l, "-") && !strings.HasPrefix(l, "---"):
			removed++
			if file == "" || skipDiffFile(file) || review {
				continue
			}
			out = append(out, file+": -"+cleanText(strings.TrimPrefix(l, "-"), redact, 200))
		}
	}
	flush()
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func skipDiffFile(path string) bool {
	p := strings.ToLower(path)
	for _, frag := range []string{"lock.", ".lock", "vendor/", "node_modules/", "dist/", ".min.js", ".pb.go", "package-lock.json"} {
		if strings.Contains(p, frag) {
			return true
		}
	}
	return strings.HasSuffix(p, ".sum")
}

func looksSecret(s string) bool {
	return redactLine(s) != s
}
