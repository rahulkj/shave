package shave

import (
	"fmt"
	"strings"
)

// renderProfile turns the portfolio analysis into a small, reusable
// "house style" block that can be pasted into AGENTS.md or project docs.
func renderProfile(rep ghReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# house style — %d local projects\n\n", len(rep.Repos))
	writeProfileLine(&b, "languages", rep.Patterns.Languages, 30)
	writeProfileLine(&b, "ci", rep.Patterns.CI, 30)
	writeProfileLine(&b, "license", rep.Patterns.Licenses, 20)
	writeProfileLine(&b, "configs", rep.Patterns.Configs, 40)
	writeProfileLine(&b, "deps", rep.Patterns.Deps, 25)
	writeProfileLine(&b, "toolchain", rep.Patterns.Toolchain, 30)
	writeProfileLine(&b, "scripts", rep.Patterns.Scripts, 25)
	if len(rep.Skew) > 0 {
		b.WriteString("# version skew to resolve\n")
		for _, s := range rep.Skew {
			if len(s.Versions) > 6 {
				s.Versions = s.Versions[:6]
			}
			fmt.Fprintf(&b, "- %s: %s\n", s.Name, strings.Join(s.Versions, ", "))
		}
		b.WriteString("\n")
	}
	if len(rep.Duplicates) > 0 {
		b.WriteString("# duplication candidates\n")
		for _, d := range rep.Duplicates {
			if len(d.Paths) > 5 {
				d.Paths = d.Paths[:5]
			}
			fmt.Fprintf(&b, "- (%d repos) %s\n", d.Repos, strings.Join(d.Paths, ", "))
		}
	}
	return b.String()
}

func writeProfileLine(b *strings.Builder, title string, items []ghCount, minPct float64) {
	var kept []ghCount
	for _, c := range items {
		if c.Pct >= minPct {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		return
	}
	fmt.Fprintf(b, "# %s\n", title)
	for _, c := range kept {
		fmt.Fprintf(b, "- %s (%.0f%%)\n", c.Name, c.Pct)
	}
	b.WriteString("\n")
}
