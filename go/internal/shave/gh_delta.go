package shave

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func reportCachePath(rep ghReport) string {
	roots := append([]string{}, rep.Roots...)
	sort.Strings(roots)
	key := sha256.Sum256([]byte(strings.Join(roots, "|")))
	return filepath.Join(cacheRoot(), "github", hex.EncodeToString(key[:])[:16]+".json")
}

func saveReport(rep ghReport) {
	p := reportCachePath(rep)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(rep)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, b, 0o644)
}

func loadReport(rep ghReport) *ghReport {
	b, err := os.ReadFile(reportCachePath(rep))
	if err != nil {
		return nil
	}
	var prev ghReport
	if err := json.Unmarshal(b, &prev); err != nil {
		return nil
	}
	return &prev
}

// deltaReport stores the current report and returns only what changed since
// the previous run — ideal for scheduled digests.
func deltaReport(rep ghReport, refresh bool) *ghDelta {
	prev := loadReport(rep)
	saveReport(rep)
	if prev == nil || refresh {
		return nil
	}
	curRepos, oldRepos := map[string]bool{}, map[string]bool{}
	for _, r := range rep.Repos {
		curRepos[repoLabel(r)] = true
	}
	for _, r := range prev.Repos {
		oldRepos[repoLabel(r)] = true
	}
	d := &ghDelta{}
	for k := range curRepos {
		if !oldRepos[k] {
			d.NewRepos = append(d.NewRepos, k)
		}
	}
	for k := range oldRepos {
		if !curRepos[k] {
			d.RemovedRepos = append(d.RemovedRepos, k)
		}
	}
	oldDeps := map[string]float64{}
	for _, c := range prev.Patterns.Deps {
		oldDeps[c.Name] = c.Pct
	}
	for _, c := range rep.Patterns.Deps {
		if c.Pct > oldDeps[c.Name] {
			d.NewDeps = append(d.NewDeps, c.Name)
		}
	}
	oldDups := map[string]bool{}
	for _, x := range prev.Duplicates {
		oldDups[x.Hash] = true
	}
	for _, x := range rep.Duplicates {
		if !oldDups[x.Hash] {
			d.NewDuplicates = append(d.NewDuplicates, x.Hash+": "+strings.Join(x.Paths, ", "))
		}
	}
	sort.Strings(d.NewRepos)
	sort.Strings(d.RemovedRepos)
	sort.Strings(d.NewDeps)
	sort.Strings(d.NewDuplicates)
	return d
}

func printDelta(d *ghDelta) {
	if d == nil {
		fmt.Println("(no previous report; saved this one for next time)")
		return
	}
	fmt.Printf("# delta since last run\n")
	lines := []struct {
		title string
		items []string
	}{
		{"new repos", d.NewRepos},
		{"removed repos", d.RemovedRepos},
		{"new/common deps", d.NewDeps},
		{"new duplicate groups", d.NewDuplicates},
	}
	any := false
	for _, l := range lines {
		if len(l.items) == 0 {
			continue
		}
		any = true
		fmt.Printf("# %s\n", l.title)
		for _, v := range l.items {
			fmt.Println("  " + v)
		}
	}
	if !any {
		fmt.Println("(no changes)")
	}
}
