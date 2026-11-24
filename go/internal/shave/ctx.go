package shave

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ctxExts = map[string]bool{
	".go": true, ".py": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".java": true, ".rs": true, ".c": true, ".cc": true, ".cpp": true, ".h": true,
	".yaml": true, ".yml": true, ".json": true, ".toml": true, ".md": true,
	".sh": true, ".tf": true, ".sql": true, ".proto": true, ".Dockerfile": true,
}

func cmdCtx(args []string) int {
	fs := flag.NewFlagSet("ctx", flag.ContinueOnError)
	query := fs.String("q", "", "query to focus on")
	budget := fs.Int("budget", 6000, "max output chars")
	contextN := fs.Int("context", 4, "context lines around matches")
	mapOnly := fs.Bool("map", false, "emit a compact outline (file -> declarations)")
	jsonOut := fs.Bool("json", false, "json output")
	paths := parseFlagsInterspersed(fs, args)
	if len(paths) == 0 {
		paths = []string{"."}
	}

	files := collectFiles(paths)

	if *mapOnly {
		fmt.Print(codeMap(files, *budget))
		return 0
	}

	if *query == "" {
		// tree + top-level declarations
		out := strings.Builder{}
		out.WriteString("# file tree\n")
		for _, f := range files {
			out.WriteString(f + "\n")
		}
		out.WriteString("\n# declarations\n")
		for _, f := range files {
			decls := extractDecls(f)
			if len(decls) == 0 {
				continue
			}
			out.WriteString("\n## " + f + "\n")
			for _, d := range decls {
				if out.Len() > *budget {
					break
				}
				out.WriteString(d + "\n")
			}
			if out.Len() > *budget {
				out.WriteString("... [budget reached]\n")
				break
			}
		}
		fmt.Print(out.String())
		return 0
	}

	// query focus: find matching lines + context
	tokens := strings.Fields(strings.ToLower(*query))
	var snippets []string
	used := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		body := querySnippets(string(b), tokens, *contextN, *budget)
		if body == "" {
			continue
		}
		header := "## " + f + "\n"
		chunk := header + body
		if used+len(chunk) > *budget {
			if used < *budget {
				rem := *budget - used
				if rem > len(header)+20 {
					snippets = append(snippets, chunk[:rem]+"\n... [budget reached]\n")
				}
			}
			used = *budget
			break
		}
		snippets = append(snippets, chunk)
		used += len(chunk)
	}
	out := strings.Join(snippets, "\n")
	if out == "" {
		out = "(no matches)\n"
	}
	if *jsonOut {
		writeJSON(map[string]interface{}{"query": *query, "budget": *budget, "output": out})
		return 0
	}
	fmt.Print(out)
	return 0
}

// collectFiles walks paths and returns the sorted list of source/config files
// worth feeding to a model. Shared with the github command so file-selection
// rules exist in exactly one place.
func collectFiles(paths []string, extraExts ...string) []string {
	allowed := make(map[string]bool, len(ctxExts)+len(extraExts))
	for ext, ok := range ctxExts {
		allowed[ext] = ok
	}
	for _, ext := range extraExts {
		allowed[ext] = true
	}
	var files []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, "shave:", err)
			continue
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		filepath.Walk(p, func(fp string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if fi.IsDir() {
				base := fi.Name()
				if base == ".git" || base == "node_modules" || base == "vendor" || base == ".idea" || base == "dist" || base == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			if fi.Size() > 200*1024 {
				return nil
			}
			ext := filepath.Ext(fp)
			if ext == "" {
				// include Dockerfile
				if strings.Contains(fi.Name(), "Dockerfile") {
					ext = ".Dockerfile"
				}
			}
			if !allowed[ext] {
				return nil
			}
			files = append(files, fp)
			return nil
		})
	}
	sort.Strings(files)
	return files
}

// querySnippets returns numbered matching lines plus context for one file.
// Shared by `ctx -q` and `shave github --find`.
func querySnippets(content string, tokens []string, contextN, budget int) string {
	lines := strings.Split(content, "\n")
	var matches []int
	for i, l := range lines {
		lc := strings.ToLower(l)
		for _, t := range tokens {
			if strings.Contains(lc, t) {
				matches = append(matches, i)
				break
			}
		}
	}
	if len(matches) == 0 {
		return ""
	}
	type rng struct{ s, e int }
	var ranges []rng
	for _, m := range matches {
		s, e := m-contextN, m+contextN
		if s < 0 {
			s = 0
		}
		if e >= len(lines) {
			e = len(lines) - 1
		}
		ranges = append(ranges, rng{s, e})
	}
	var merged []rng
	for _, r := range ranges {
		if len(merged) == 0 || r.s > merged[len(merged)-1].e+1 {
			merged = append(merged, r)
		} else if r.e > merged[len(merged)-1].e {
			merged[len(merged)-1].e = r.e
		}
	}
	var b strings.Builder
	for _, r := range merged {
		for i := r.s; i <= r.e; i++ {
			fmt.Fprintf(&b, "%4d: %s\n", i+1, lines[i])
		}
		b.WriteString("---\n")
	}
	out := b.String()
	if budget > 0 && len(out) > budget {
		out = out[:budget] + "\n... [budget reached]\n"
	}
	return out
}

// codeMap is progressive disclosure: a file -> declarations outline that lets
// an agent ask for specific lines instead of pasting whole files.
func codeMap(files []string, budget int) string {
	var b strings.Builder
	for _, f := range files {
		decls := extractDecls(f)
		line := f
		if len(decls) > 0 {
			names := decls
			if len(names) > 5 {
				names = names[:5]
			}
			for i, n := range names {
				names[i] = trunc(n, 60)
			}
			line += fmt.Sprintf(" (%d decls): %s", len(decls), strings.Join(names, " | "))
		}
		line = trunc(line, 300)
		if b.Len()+len(line) > budget && budget > 0 {
			b.WriteString("... [budget reached]\n")
			break
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func isDeclaration(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	for _, prefix := range []string{"func ", "type ", "class ", "def ", "interface ", "struct ", "resource ", "module ", "const "} {
		if strings.HasPrefix(low, prefix) {
			return true
		}
	}
	return false
}

func extractDecls(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if isDeclaration(l) && len(l) < 240 {
			out = append(out, l)
		}
	}
	return out
}
