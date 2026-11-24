package shave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeSkew(t *testing.T) {
	repos := []ghRepo{
		{Path: "/a", Name: "a", DepVersions: []string{"github.com/google/uuid@1.6.0", "react@18"}},
		{Path: "/b", Name: "b", DepVersions: []string{"github.com/google/uuid@1.7.0", "react@19"}},
	}
	got := analyzeSkew(repos, 10)
	if len(got) != 2 || got[0].Name != "github.com/google/uuid" || len(got[0].Versions) != 2 {
		t.Fatalf("skew = %+v", got)
	}
}

func TestExtractToolchain(t *testing.T) {
	dir := t.TempDir()
	gomod := filepath.Join(dir, "go.mod")
	writeTestFile(t, gomod, "module x\n\ngo 1.27\n")
	got := extractToolchain(dir, gomod)
	if !contains(got, "go=1.27") {
		t.Fatalf("toolchain = %v", got)
	}
}

func TestNearDuplicates(t *testing.T) {
	root := t.TempDir()
	var repos []ghRepo
	base := `#!/usr/bin/env bash
set -euo pipefail
export REGION=us-east-1
NAMESPACE=default
APP=api
kubectl config use-context prod
kubectl apply -f deployment.yml
kubectl apply -f service.yml
kubectl rollout status deployment/api
kubectl get pods -n default
echo "deployed to ${REGION}"
echo "namespace ${NAMESPACE}"
echo "app ${APP}"
echo "step one complete"
echo "step two complete"
echo "step three complete"
echo "step four complete"
echo "step five complete"
echo "step six complete"
echo "step seven complete"
echo "step eight complete"
`
	other := strings.Replace(base, "REGION=us-east-1", "REGION=us-west-2", 1)
	for i, body := range []string{base, other} {
		dir := filepath.Join(root, "repo"+string(rune('a'+i)))
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "deploy.sh")
		writeTestFile(t, p, body)
		repos = append(repos, ghRepo{Path: dir, Name: filepath.Base(dir), allFiles: []string{p}})
	}
	got := nearDuplicates(repos, 30, 5, true)
	if len(got) != 1 || got[0].Score < 0.6 {
		t.Fatalf("near duplicates = %+v", got)
	}
	if len(got[0].Paths) != 2 {
		t.Fatalf("paths = %v", got[0].Paths)
	}
}

func TestAuditReposRedactsAndFindsGaps(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "app.sh")
	fakeLine := "export " + "API_KEY" + "=" + strings.Repeat("a", 32) + "\n"
	writeTestFile(t, p, fakeLine)
	r := ghRepo{Path: dir, Name: "demo", allFiles: []string{p}}
	got := auditRepos([]ghRepo{r}, 20)
	foundSecret := false
	for _, f := range got {
		if f.Kind == "secret" {
			foundSecret = true
			if strings.Contains(f.Text, strings.Repeat("a", 12)) {
				t.Fatalf("audit leaked secret: %q", f.Text)
			}
		}
	}
	if !foundSecret {
		t.Fatalf("no secret finding: %+v", got)
	}
}

func TestFindInRepos(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "retry.go")
	writeTestFile(t, p, "func withRetry() {}\n")
	r := ghRepo{Path: dir, Name: "demo", allFiles: []string{p}}
	got := findInRepos([]ghRepo{r}, "retry", 5, true)
	if len(got) != 1 || !strings.Contains(got[0].Path, "retry.go") {
		t.Fatalf("find = %+v", got)
	}
}

func TestCompressDiffReviewRedacts(t *testing.T) {
	lines := []string{
		"+++ b/x.go", "@@ -1 +1 @@",
		"+func bigger() {}", "+var password = " + "hunter2secret", "+// comment only",
	}
	got := compressDiff(lines, true, true, 20)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "func bigger") || strings.Contains(joined, "comment only") {
		t.Fatalf("review output = %q", joined)
	}
	if strings.Contains(joined, "hunter2secret") || !strings.Contains(joined, "[REDACTED]") {
		t.Fatalf("review output leaked secret: %q", joined)
	}
}

func TestCollapsePasses(t *testing.T) {
	kept, n := collapsePasses([]string{"ok pkg/a", "--- FAIL: TestX", "FAIL"})
	if n != 1 || len(kept) != 2 {
		t.Fatalf("kept=%v n=%d", kept, n)
	}
}

func TestRenderProfileAndDelta(t *testing.T) {
	rep := ghReport{
		Repos: []ghRepo{{Name: "demo"}},
		Patterns: ghPatterns{
			Languages: []ghCount{{Name: "Go", Repos: 1, Pct: 100}},
		},
	}
	profile := renderProfile(rep)
	if !strings.Contains(profile, "house style") || !strings.Contains(profile, "Go (100%)") {
		t.Fatalf("profile = %q", profile)
	}

	t.Setenv("HOME", t.TempDir())
	rep.Roots = []string{"/tmp/demo"}
	deltaReport(rep, false) // baseline
	rep.Patterns.Deps = []ghCount{{Name: "newdep", Repos: 1, Pct: 100}}
	d := deltaReport(rep, false)
	if d == nil || len(d.NewDeps) != 1 || d.NewDeps[0] != "newdep" {
		t.Fatalf("delta = %+v", d)
	}
}
