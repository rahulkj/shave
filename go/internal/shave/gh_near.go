package shave

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type nearCandidate struct {
	path   string
	rel    string
	repo   string
	base   string
	lines  []string
	shing  map[string]bool
	hash   string
	redact bool
}

// nearDuplicates finds files that are mostly the same but not byte-identical
// (exact copies are reported by findDuplicates) using 3-line shingles and a
// 60% Jaccard threshold. Grouping by basename keeps
// the comparison cheap without external dependencies.
func nearDuplicates(repos []ghRepo, minBytes, max int, redact bool) []ghNear {
	byBase := map[string][]nearCandidate{}
	for _, r := range repos {
		for _, f := range r.allFiles {
			fi, err := os.Stat(f)
			if err != nil || fi.Size() < int64(minBytes) || fi.Size() > ghScanMaxBytes {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			norm := normalizeForHash(b, redact)
			if len(norm) < minBytes {
				continue
			}
			lines := strings.Split(norm, "\n")
			h := sha256.Sum256([]byte(norm))
			c := nearCandidate{
				path: f, rel: repoRel(r, f), repo: repoLabel(r), base: filepath.Base(f), lines: lines,
				shing: shingleLines(lines, 3), hash: hex.EncodeToString(h[:])[:12], redact: redact,
			}
			byBase[c.base] = append(byBase[c.base], c)
		}
	}
	var out []ghNear
	for _, group := range byBase {
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if a.repo == b.repo || a.hash == b.hash {
					continue
				}
				score := jaccard(a.shing, b.shing)
				if score < 0.6 {
					continue
				}
				out = append(out, ghNear{
					Score:  score,
					Target: "shared/" + a.base,
					Paths:  []string{a.repo + ":" + a.rel, b.repo + ":" + b.rel},
					Diffs:  differingLines(a.lines, b.lines, redact),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Paths[0] < out[j].Paths[0]
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func shingleLines(lines []string, n int) map[string]bool {
	out := map[string]bool{}
	if len(lines) == 0 {
		return out
	}
	if len(lines) <= n {
		out[strings.Join(lines, "\n")] = true
		return out
	}
	for i := 0; i+n <= len(lines); i++ {
		out[strings.Join(lines[i:i+n], "\n")] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func differingLines(a, b []string, redact bool) []string {
	other := map[string]bool{}
	for _, l := range b {
		other[l] = true
	}
	var out []string
	for _, l := range a {
		if other[l] {
			continue
		}
		out = append(out, cleanText(l, redact, 120))
		if len(out) == 3 {
			break
		}
	}
	return out
}

func printNear(items []ghNear, terseOut bool) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("# near-duplicates: %d pairs\n", len(items))
	for _, n := range items {
		if terseOut {
			fmt.Printf("%.0f%%\t%s\n", n.Score*100, strings.Join(n.Paths, " | "))
			continue
		}
		fmt.Printf("  %.0f%%  %s\n", n.Score*100, strings.Join(n.Paths, " | "))
		fmt.Printf("      extract to: %s\n", n.Target)
		for _, d := range n.Diffs {
			fmt.Printf("      only in first: %s\n", d)
		}
	}
}
