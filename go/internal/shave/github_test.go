package shave

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFlagsInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "")
	top := fs.Int("top", 1, "")
	paths := parseFlagsInterspersed(fs, []string{"./src", "--json", "--top", "5", "other"})
	if !*jsonOut || *top != 5 || strings.Join(paths, ",") != "./src,other" {
		t.Fatalf("json=%v top=%d paths=%v", *jsonOut, *top, paths)
	}
}

func TestParseGitRemote(t *testing.T) {
	cases := map[string][2]string{
		"git@github.com:rahulkj/shave.git":     {"rahulkj", "shave"}, // sdg:ignore [synthetic remote fixture]
		"https://github.com/rahulkj/shave.git": {"rahulkj", "shave"},
		"ssh://git@github.com/rahulkj/shave":   {"rahulkj", "shave"}, // sdg:ignore [synthetic remote fixture]
		"https://github.com/rahulkj/shave/":    {"rahulkj", "shave"},
		"git@gitlab.com:some/other.git":        {"some", "other"}, // sdg:ignore [synthetic remote fixture]
		"shave":                                {"", ""},
	}
	for in, want := range cases {
		owner, name := parseGitRemote(in)
		if owner != want[0] || name != want[1] {
			t.Errorf("parseGitRemote(%q) = %q, %q; want %q, %q", in, owner, name, want[0], want[1])
		}
	}
}

func TestDiscoverAndLoadRepos(t *testing.T) {
	root := t.TempDir()
	alpha := filepath.Join(root, "alpha")
	beta := filepath.Join(root, "beta")
	for _, d := range []string{alpha, beta} {
		if err := os.MkdirAll(filepath.Join(d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	sharedGo := `package main

import "fmt"

// this comment is stripped when hashing
func main() {
    fmt.Println("hello from a deliberately duplicated source file")
    fmt.Println("line two of the duplicated source file")
    fmt.Println("line three, enough bytes to pass the duplicate threshold")
}
`
	writeTestFile(t, filepath.Join(alpha, "go.mod"), "module alpha\n\ngo 1.27\n\nrequire github.com/google/uuid v1.6.0\n")
	writeTestFile(t, filepath.Join(alpha, "README.md"), "# Alpha\npassword=hunter2 is used only in this demo service.\n")
	writeTestFile(t, filepath.Join(alpha, "main.go"), sharedGo)
	writeTestFile(t, filepath.Join(beta, "package.json"), `{"name":"beta","dependencies":{"react":"^19.0.0"}}`)
	writeTestFile(t, filepath.Join(beta, "README.md"), "# Beta\na small web application with a verbose enough description.\n")
	writeTestFile(t, filepath.Join(beta, "main.go"), sharedGo)

	repos := discoverRepos([]string{root}, 1, "")
	if len(repos) != 2 {
		t.Fatalf("discovered %d repos, want 2: %+v", len(repos), repos)
	}
	for i := range repos {
		loadRepo(&repos[i], true)
	}
	byName := map[string]ghRepo{}
	for _, r := range repos {
		byName[r.Name] = r
	}
	a, b := byName["alpha"], byName["beta"]
	if a.Language != "Go" || b.Language != "JavaScript" {
		t.Errorf("languages = %q, %q; want Go, JavaScript", a.Language, b.Language)
	}
	if strings.Contains(a.Description, "hunter2") || !strings.Contains(a.Description, "[REDACTED]") {
		t.Errorf("description was not redacted: %q", a.Description)
	}
	if !contains(a.Deps, "github.com/google/uuid") {
		t.Errorf("go.mod deps = %v", a.Deps)
	}
	if !contains(b.Deps, "react") {
		t.Errorf("package.json deps = %v", b.Deps)
	}

	dups := findDuplicates(repos, 60, 10, true)
	if len(dups) != 1 || dups[0].Repos != 2 {
		t.Fatalf("duplicates = %+v; want one group spanning two repos", dups)
	}
	if !strings.Contains(strings.Join(dups[0].Paths, ","), "main.go") {
		t.Errorf("duplicate paths = %v", dups[0].Paths)
	}
}

func TestFindDuplicatesRedactsBeforeHashing(t *testing.T) {
	root := t.TempDir()
	var repos []ghRepo
	body := func(secret string) string {
		return `#!/usr/bin/env bash
set -euo pipefail
export DEPLOY_TOKEN=` + secret + `
echo "deploying the application to the target cluster"
kubectl apply -f deployment.yml
kubectl rollout status deployment/app
`
	}
	for i, secret := range []string{"supersecretvalue111", "completelydifferent222"} {
		dir := filepath.Join(root, "repo"+string(rune('a'+i)))
		if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, "deploy.sh")
		writeTestFile(t, p, body(secret))
		repos = append(repos, ghRepo{Name: filepath.Base(dir), Path: dir, allFiles: []string{p}})
	}

	withRedact := findDuplicates(repos, 60, 10, true)
	if len(withRedact) != 1 || withRedact[0].Repos != 2 {
		t.Fatalf("redacted duplicates = %+v; want one shared group", withRedact)
	}
	if strings.Contains(withRedact[0].Preview, "supersecret") || strings.Contains(withRedact[0].Preview, "completelydifferent") {
		t.Errorf("preview leaked a secret: %q", withRedact[0].Preview)
	}
	if got := findDuplicates(repos, 60, 10, false); len(got) != 0 {
		t.Errorf("without redaction differing secrets should not match, got %+v", got)
	}
}

func TestNormalizeForHashIgnoresFormatting(t *testing.T) {
	a := normalizeForHash([]byte("foo    bar\n\n# comment\n baz \n"), false)
	b := normalizeForHash([]byte("foo bar\nbaz\n"), false)
	if a != b {
		t.Errorf("normalization differs: %q vs %q", a, b)
	}
}

func TestAnalyzePatterns(t *testing.T) {
	repos := []ghRepo{
		{Language: "Go", CI: "GitHub Actions", Deps: []string{"uuid", "pgx"}},
		{Language: "Go", CI: "GitHub Actions", Deps: []string{"uuid"}},
	}
	p := analyzePatterns(repos, 10)
	if len(p.Languages) != 1 || p.Languages[0].Repos != 2 || p.Languages[0].Pct != 100 {
		t.Errorf("languages = %+v", p.Languages)
	}
	if len(p.Deps) != 2 || p.Deps[0].Name != "uuid" || p.Deps[0].Repos != 2 {
		t.Errorf("deps = %+v", p.Deps)
	}
}

func TestDepsFromManifest(t *testing.T) {
	dir := t.TempDir()
	gomod := filepath.Join(dir, "go.mod")
	writeTestFile(t, gomod, "module x\n\ngo 1.27\n\nrequire github.com/google/uuid v1.6.0\n\nrequire (\n\tgithub.com/jackc/pgx/v5 v5.7.0\n\tgolang.org/x/sys v0.30.0 // indirect\n)\n")
	got := depNames(depsFromManifest(gomod))
	for _, want := range []string{"github.com/google/uuid", "github.com/jackc/pgx", "golang.org/x/sys"} {
		if !contains(got, want) {
			t.Errorf("go.mod deps = %v; missing %q", got, want)
		}
	}
	if !contains(depPairs(depsFromManifest(gomod)), "github.com/google/uuid@1.6.0") {
		t.Errorf("go.mod versions lost: %v", depPairs(depsFromManifest(gomod)))
	}

	pkg := filepath.Join(dir, "package.json")
	writeTestFile(t, pkg, `{"dependencies": {"react": "^19"}, "devDependencies": {"vitest": "^3"}}`)
	got = depNames(depsFromManifest(pkg))
	if !contains(got, "react") || !contains(got, "vitest") {
		t.Errorf("package.json deps = %v", got)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
