package shave

import (
	"fmt"
	"sort"
	"strings"
)

// mineFixes finds the same class of bug fixed in multiple repos by mining
// local git log subjects. No network, no new dependencies.
func mineFixes(repos []ghRepo, max int) []ghFix {
	type rec struct {
		repos map[string]bool
		count int
	}
	recs := map[string]*rec{}
	for _, r := range repos {
		out, _, rc := runCmd("git", "-C", r.Path, "log", "--format=%s", "-n", "500")
		if rc != 0 {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			msg := normalizeFixMessage(line)
			if len(msg) < 12 {
				continue
			}
			e := recs[msg]
			if e == nil {
				e = &rec{repos: map[string]bool{}}
				recs[msg] = e
			}
			e.repos[repoLabel(r)] = true
			e.count++
		}
	}
	msgs := make([]string, 0, len(recs))
	for m := range recs {
		msgs = append(msgs, m)
	}
	sort.Strings(msgs)
	groups := []map[string]*rec{}
	for _, m := range msgs {
		words := wordSet(m)
		placed := false
		for i := range groups {
			for canonical := range groups[i] {
				if jaccard(words, wordSet(canonical)) >= 0.6 {
					groups[i][m] = recs[m]
					placed = true
					break
				}
			}
			if placed {
				break
			}
		}
		if !placed {
			groups = append(groups, map[string]*rec{m: recs[m]})
		}
	}
	var out []ghFix
	for _, g := range groups {
		fix := ghFix{}
		seenRepos := map[string]bool{}
		for msg, e := range g {
			fix.Count += e.count
			for repo := range e.repos {
				seenRepos[repo] = true
			}
			if fix.Message == "" || len(msg) < len(fix.Message) {
				fix.Message = msg
			}
		}
		for repo := range seenRepos {
			fix.Repos = append(fix.Repos, repo)
		}
		sort.Strings(fix.Repos)
		if len(fix.Repos) < 2 {
			continue
		}
		out = append(out, fix)
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Repos) != len(out[j].Repos) {
			return len(out[i].Repos) > len(out[j].Repos)
		}
		return out[i].Message < out[j].Message
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func normalizeFixMessage(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	drop := map[string]bool{
		"fix": true, "fixed": true, "fixes": true, "bug": true, "bugfix": true,
		"chore": true, "the": true, "a": true, "an": true, "in": true, "of": true,
		"to": true, "for": true, "and": true, "with": true,
	}
	var keep []string
	for _, tok := range strings.Fields(s) {
		t := strings.Trim(tok, "():,.#`\"'")
		if t == "" || drop[t] || len(t) < 3 {
			continue
		}
		if strings.ContainsAny(t, "/@\\") || strings.HasPrefix(t, "#") {
			continue
		}
		allNum := true
		for _, r := range t {
			if r < '0' || r > '9' {
				allNum = false
				break
			}
		}
		if allNum {
			continue
		}
		keep = append(keep, t)
		if len(keep) == 10 {
			break
		}
	}
	return strings.Join(keep, " ")
}

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

func printFixes(items []ghFix, terseOut bool) {
	if len(items) == 0 {
		return
	}
	fmt.Println("# repeated fixes")
	for _, f := range items {
		if terseOut {
			fmt.Printf("%d\t%s\t%s\n", len(f.Repos), f.Message, strings.Join(f.Repos, ","))
			continue
		}
		fmt.Printf("  %d repos: %s\n      %s\n", len(f.Repos), f.Message, strings.Join(f.Repos, ", "))
	}
}
