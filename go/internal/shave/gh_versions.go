package shave

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// extractToolchain records pinned tool/action versions in a compact "k=v"
// form so drift (mixed Go versions, stale actions) is visible across repos.
func extractToolchain(root, path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	base := strings.ToLower(filepath.Base(path))
	var out []string
	switch base {
	case "go.mod":
		if m := reGoVersion.FindStringSubmatch(string(b)); len(m) > 0 {
			out = append(out, "go="+m[1])
		}
	case "package.json":
		if m := reNodeVer.FindStringSubmatch(string(b)); len(m) > 0 {
			out = append(out, "node="+m[1])
		}
	case "dockerfile":
		for _, m := range reFrom.FindAllStringSubmatch(string(b), -1) {
			if m[1] != "" && !strings.EqualFold(m[1], "scratch") {
				out = append(out, "image="+m[1])
			}
		}
	}
	if strings.HasPrefix(rel, ".github/workflows/") {
		for _, m := range reAction.FindAllStringSubmatch(string(b), -1) {
			if m[1] != "" {
				out = append(out, "action="+m[1])
			}
		}
	}
	return out
}

// analyzeSkew finds dependencies pinned at different versions in different
// repos — the highest-signal output of portfolio analysis.
func analyzeSkew(repos []ghRepo, top int) []ghSkew {
	type entry struct {
		versions map[string]bool
		repos    map[string]bool
	}
	entries := map[string]*entry{}
	for _, r := range repos {
		for _, dv := range r.DepVersions {
			i := strings.LastIndex(dv, "@")
			if i < 1 {
				continue
			}
			name, version := dv[:i], dv[i+1:]
			if version == "" {
				continue
			}
			e := entries[name]
			if e == nil {
				e = &entry{versions: map[string]bool{}, repos: map[string]bool{}}
				entries[name] = e
			}
			e.versions[version] = true
			e.repos[r.Path] = true
		}
	}
	var out []ghSkew
	for name, e := range entries {
		if len(e.versions) < 2 {
			continue
		}
		versions := make([]string, 0, len(e.versions))
		for v := range e.versions {
			versions = append(versions, v)
		}
		sort.Strings(versions)
		out = append(out, ghSkew{Name: name, Versions: versions, Repos: len(e.repos)})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Versions) != len(out[j].Versions) {
			return len(out[i].Versions) > len(out[j].Versions)
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > top {
		out = out[:top]
	}
	return out
}

func printSkew(items []ghSkew, terseOut bool) {
	if len(items) == 0 {
		return
	}
	fmt.Println("# version skew")
	top := items
	if len(top) > 10 {
		top = top[:10]
	}
	for _, s := range top {
		if terseOut {
			fmt.Printf("%s\t%d\t%s\n", s.Name, len(s.Versions), strings.Join(s.Versions, ","))
			continue
		}
		fmt.Printf("  %-40s %d versions in %d repos: %s\n", trim(s.Name, 39), len(s.Versions), s.Repos, strings.Join(s.Versions, ", "))
	}
}
