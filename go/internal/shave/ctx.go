package shave

import (
	"bufio"
	"encoding/json"
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
	jsonOut := fs.Bool("json", false, "json output")
	_ = fs.Parse(args)
	paths := fs.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}

	// collect files
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
			if !ctxExts[ext] {
				return nil
			}
			files = append(files, fp)
			return nil
		})
	}
	sort.Strings(files)

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
		lines := strings.Split(string(b), "\n")
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
			continue
		}
		// build ranges
		type rng struct{ s, e int }
		var ranges []rng
		for _, m := range matches {
			s := m - *contextN
			if s < 0 {
				s = 0
			}
			e := m + *contextN
			if e >= len(lines) {
				e = len(lines) - 1
			}
			ranges = append(ranges, rng{s, e})
		}
		// merge
		var merged []rng
		for _, r := range ranges {
			if len(merged) == 0 || r.s > merged[len(merged)-1].e+1 {
				merged = append(merged, r)
			} else {
				if r.e > merged[len(merged)-1].e {
					merged[len(merged)-1].e = r.e
				}
			}
		}
		header := "## " + f + "\n"
		chunk := header
		for _, r := range merged {
			for i := r.s; i <= r.e; i++ {
				chunk += fmt.Sprintf("%4d: %s\n", i+1, lines[i])
			}
			chunk += "---\n"
		}
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
		enc := json.NewEncoder(os.Stdout)
		// simple object
		enc.SetIndent("", "  ")
		enc.Encode(map[string]interface{}{"query": *query, "budget": *budget, "output": out})
		return 0
	}
	fmt.Print(out)
	return 0
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
		low := strings.ToLower(l)
		if strings.HasPrefix(low, "func ") || strings.HasPrefix(low, "type ") ||
			strings.HasPrefix(low, "class ") || strings.HasPrefix(low, "def ") ||
			strings.HasPrefix(low, "interface ") || strings.HasPrefix(low, "struct ") ||
			strings.HasPrefix(low, "resource ") || strings.HasPrefix(low, "module ") ||
			strings.HasPrefix(low, "const ") {
			if len(l) < 240 {
				out = append(out, l)
			}
		}
	}
	return out
}
