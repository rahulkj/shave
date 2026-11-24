package shave

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func repoLabel(r ghRepo) string {
	if r.Owner != "" {
		return r.Owner + "/" + r.Name
	}
	return r.Name
}

func repoRel(r ghRepo, f string) string {
	rel, _ := filepath.Rel(r.Path, f)
	return filepath.ToSlash(rel)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// auditRepos reports secret-like lines and basic hygiene gaps. It never
// prints the raw line: the preview is passed through redactLine first.
func auditRepos(repos []ghRepo, max int) []ghFinding {
	var out []ghFinding
	for _, r := range repos {
		name := repoLabel(r)
		for _, f := range r.allFiles {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			for i, line := range strings.Split(string(b), "\n") {
				if !looksSecret(line) || auditNoise(line) {
					continue
				}
				out = append(out, ghFinding{
					Repo: name, Path: repoRel(r, f), Line: i + 1,
					Kind: "secret", Text: cleanText(line, true, 80),
				})
			}
		}
		checks := []struct {
			kind    string
			present bool
		}{
			{"no readme", r.Description != ""},
			{"no license", r.License != ""},
			{"no CI", r.CI != ""},
			{"no .gitignore", fileExists(filepath.Join(r.Path, ".gitignore"))},
		}
		for _, c := range checks {
			if !c.present {
				out = append(out, ghFinding{Repo: name, Path: ".", Kind: c.kind})
			}
		}
		if fileExists(filepath.Join(r.Path, ".env")) {
			out = append(out, ghFinding{Repo: name, Path: ".env", Kind: "committed .env"})
		}
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// auditNoise drops regex definitions and already-redacted fixture lines so
// the audit reports real leaks instead of the code that detects them.
func auditNoise(line string) bool {
	for _, frag := range []string{"MustCompile", "[REDACTED]", "redactLine(", "reSecret", "reLong", "reHex", "looksSecret("} {
		if strings.Contains(line, frag) {
			return true
		}
	}
	return false
}

// findInRepos is cross-repo "find it before you build it": it searches code,
// declarations and descriptions, returning pointers rather than full dumps.
func findInRepos(repos []ghRepo, query string, max int, redact bool) []ghFinding {
	tokens := strings.Fields(strings.ToLower(query))
	if len(tokens) == 0 {
		return nil
	}
	var out []ghFinding
	for _, r := range repos {
		name := repoLabel(r)
		for _, f := range r.allFiles {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			body := querySnippets(string(b), tokens, 1, 0)
			if body == "" {
				continue
			}
			out = append(out, ghFinding{
				Repo: name, Path: repoRel(r, f), Kind: "match",
				Text: cleanText(firstLine(body), redact, 120),
			})
			if len(out) >= max {
				return out
			}
		}
		low := strings.ToLower(r.Description + " " + strings.Join(r.Topics, " "))
		for _, t := range tokens {
			if strings.Contains(low, t) {
				out = append(out, ghFinding{
					Repo: name, Path: ".", Kind: "description",
					Text: cleanText(r.Description, redact, 120),
				})
				break
			}
		}
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func printFindings(title string, items []ghFinding, terseOut bool) {
	if len(items) == 0 {
		return
	}
	fmt.Println(title)
	top := items
	if len(top) > 20 && !terseOut {
		top = top[:20]
	}
	for _, f := range top {
		where := f.Repo + ":" + f.Path
		if f.Line > 0 {
			where += fmt.Sprintf(":%d", f.Line)
		}
		if terseOut {
			fmt.Printf("%s\t%s\t%s\n", f.Kind, where, f.Text)
			continue
		}
		fmt.Printf("  %-12s %s  %s\n", f.Kind, trim(where, 60), f.Text)
	}
	if len(items) > len(top) {
		fmt.Printf("  ... %d more (use --max)\n", len(items)-len(top))
	}
}
