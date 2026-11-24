package shave

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// github.go analyzes local git clones: a compact inventory of each project,
// the patterns they share, and files/scripts that are duplicated across them.
// It is deliberately local-only (no network, no gh/API calls).

const (
	ghScanMaxBytes = 128 << 10 // ignore huge generated files when hashing
	ghListCap      = 50        // per-repo list cap, keeps JSON/report bounded
)

var (
	reHeading   = regexp.MustCompile(`^#{1,3}\s+(.+)$`)
	reSpace     = regexp.MustCompile(`\s+`)
	reMajor     = regexp.MustCompile(`/v[0-9]+$`)
	reDepBlock  = regexp.MustCompile(`"(?:dependencies|devDependencies|peerDependencies)"\s*:\s*\{([^}]*)\}`)
	reJSONDep   = regexp.MustCompile(`"([^"]+)"\s*:\s*"([^"]+)"`)
	rePyReq     = regexp.MustCompile(`^([A-Za-z0-9_.-]+)([=<>~!\[].*)?$`)
	reTomlDep   = regexp.MustCompile(`^([A-Za-z0-9_.-]+)\s*=\s*(.+)$`)
	reGradleDep = regexp.MustCompile(`["']([A-Za-z0-9_.-]+):([A-Za-z0-9_.-]+):[A-Za-z0-9_.-]+["']`)
	reMavenDep  = regexp.MustCompile(`<artifactId>([^<]+)</artifactId>`)
	reGoVersion = regexp.MustCompile(`(?m)^go\s+([0-9.]+)`)
	reNodeVer   = regexp.MustCompile(`"node"\s*:\s*"([^"]+)"`)
	reFrom      = regexp.MustCompile(`(?im)^FROM\s+(\S+)`)
	reAction    = regexp.MustCompile(`uses:\s*([^\s#]+)`)
)

// extra extensions that ctx does not need but dependency/license discovery does
var ghExtraExts = []string{".mod", ".txt", ".xml", ".gradle", ".lock", ".sum", ".properties"}

var ghLang = map[string]string{
	".go": "Go", ".py": "Python", ".js": "JavaScript", ".jsx": "JavaScript",
	".ts": "TypeScript", ".tsx": "TypeScript", ".java": "Java", ".rs": "Rust",
	".c": "C", ".cc": "C++", ".cpp": "C++", ".h": "C/C++", ".sh": "Shell",
	".tf": "HCL", ".hcl": "HCL", ".rb": "Ruby", ".php": "PHP", ".swift": "Swift",
	".kt": "Kotlin", ".scala": "Scala", ".sql": "SQL", ".proto": "Protobuf",
}

var ghNonCodeExt = map[string]bool{
	".md": true, ".json": true, ".yaml": true, ".yml": true, ".toml": true,
	".txt": true, ".mod": true, ".sum": true, ".lock": true, ".xml": true,
	".gradle": true, ".properties": true, ".Dockerfile": true,
}

var ghConfigNames = map[string]bool{
	"go.mod": true, "package.json": true, "pyproject.toml": true,
	"requirements.txt": true, "cargo.toml": true, "pom.xml": true,
	"build.gradle": true, "build.gradle.kts": true, "makefile": true,
	"justfile": true, "taskfile.yml": true, "dockerfile": true,
	"docker-compose.yml": true, ".goreleaser.yml": true, "npm-shrinkwrap.json": true,
}

type ghRepo struct {
	Name        string   `json:"name"`
	Owner       string   `json:"owner,omitempty"`
	Path        string   `json:"path"`
	Description string   `json:"description,omitempty"`
	Language    string   `json:"language,omitempty"`
	License     string   `json:"license,omitempty"`
	Updated     string   `json:"updated,omitempty"`
	CI          string   `json:"ci,omitempty"`
	Files       []string `json:"files"`
	Configs     []string `json:"configs"`
	Deps        []string `json:"deps"`
	DepVersions []string `json:"dep_versions,omitempty"`
	Toolchain   []string `json:"toolchain,omitempty"`
	Topics      []string `json:"topics,omitempty"`
	Scripts     []string `json:"scripts"`
	allFiles    []string // full paths, used only for duplicate detection
}

type ghSkew struct {
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
	Repos    int      `json:"repos"`
}

type ghNear struct {
	Score  float64  `json:"score"`
	Target string   `json:"target,omitempty"`
	Paths  []string `json:"paths"`
	Diffs  []string `json:"diffs,omitempty"`
}

type ghFinding struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

type ghFix struct {
	Message string   `json:"message"`
	Repos   []string `json:"repos"`
	Count   int      `json:"count"`
}

type ghDelta struct {
	NewRepos      []string `json:"new_repos,omitempty"`
	RemovedRepos  []string `json:"removed_repos,omitempty"`
	NewDeps       []string `json:"new_deps,omitempty"`
	NewDuplicates []string `json:"new_duplicates,omitempty"`
}

type ghCount struct {
	Name  string  `json:"name"`
	Repos int     `json:"repos"`
	Pct   float64 `json:"pct"`
}

type ghPatterns struct {
	Languages []ghCount `json:"languages"`
	CI        []ghCount `json:"ci"`
	Licenses  []ghCount `json:"licenses"`
	Configs   []ghCount `json:"configs"`
	Deps      []ghCount `json:"deps"`
	Scripts   []ghCount `json:"scripts"`
	Toolchain []ghCount `json:"toolchain,omitempty"`
	Topics    []ghCount `json:"topics,omitempty"`
}

type ghDup struct {
	Hash    string   `json:"hash"`
	Repos   int      `json:"repos"`
	Files   int      `json:"files"`
	Bytes   int      `json:"bytes"`
	Paths   []string `json:"paths"`
	Preview string   `json:"preview"`
}

type ghReport struct {
	Roots      []string    `json:"roots"`
	Repos      []ghRepo    `json:"repos"`
	Patterns   ghPatterns  `json:"patterns"`
	Duplicates []ghDup     `json:"duplicates"`
	Skew       []ghSkew    `json:"skew,omitempty"`
	Near       []ghNear    `json:"near,omitempty"`
	Audit      []ghFinding `json:"audit,omitempty"`
	Find       []ghFinding `json:"find,omitempty"`
	Fixes      []ghFix     `json:"fixes,omitempty"`
	Delta      *ghDelta    `json:"delta,omitempty"`
}

func cmdGithub(args []string) int {
	fs := flag.NewFlagSet("github", flag.ContinueOnError)
	depth := fs.Int("depth", 2, "how deep below each path to look for git repos")
	top := fs.Int("top", 10, "rows per commonality section")
	max := fs.Int("max", 20, "max repos/duplicates to print")
	owner := fs.String("owner", "", "only include repos whose origin owner matches")
	minBytes := fs.Int("min-bytes", 120, "min normalized size for duplicate candidates")
	find := fs.String("find", "", "search declarations/docs across all repos")
	profile := fs.Bool("profile", false, "emit a compact reusable house-style profile")
	audit := fs.Bool("audit", false, "report secret/hygiene findings (paths only)")
	near := fs.Bool("near", false, "cluster near-duplicate files (>=60% similar)")
	fixes := fs.Bool("fixes", false, "mine git log for fixes repeated across repos")
	since := fs.Bool("since", false, "show only what changed since the last run")
	fromStdin := fs.Bool("from-stdin", false, "read gh API JSON from stdin instead of scanning")
	refresh := fs.Bool("refresh", false, "ignore the cached previous report")
	noRedact := fs.Bool("no-redact", false, "disable secret redaction")
	jsonOut := fs.Bool("json", false, "json output")
	paths := parseFlagsInterspersed(fs, args)
	if len(paths) == 0 {
		paths = []string{"."}
	}
	if *top < 1 {
		*top = 10
	}
	if *max < 1 {
		*max = 20
	}

	redact := !*noRedact
	remote := *fromStdin
	var repos []ghRepo
	if remote {
		repos = reposFromStdin(redact)
	} else {
		repos = discoverRepos(paths, *depth, *owner)
		for i := range repos {
			loadRepo(&repos[i], redact)
		}
	}
	rep := ghReport{
		Roots:    paths,
		Repos:    repos,
		Patterns: analyzePatterns(repos, *top),
	}
	// Deep scans (hashing/reading every file) are skipped for fast modes.
	deep := !remote && *find == "" && !*profile
	rep.Skew = analyzeSkew(repos, *top)
	if deep {
		rep.Duplicates = findDuplicates(repos, *minBytes, *max, redact)
		if *near {
			rep.Near = nearDuplicates(repos, *minBytes, *max, redact)
		}
		if *audit {
			rep.Audit = auditRepos(repos, *max)
		}
		if *fixes {
			rep.Fixes = mineFixes(repos, *max)
		}
	}
	if *find != "" {
		rep.Find = findInRepos(repos, *find, *max, redact)
	}
	if *since && !remote {
		rep.Delta = deltaReport(rep, *refresh)
	}

	if *profile {
		if *jsonOut {
			writeJSON(rep)
		} else {
			fmt.Print(renderProfile(rep))
		}
		return 0
	}
	if *jsonOut {
		writeJSON(rep)
		return 0
	}
	if *find != "" {
		printFindings("# cross-repo matches for "+*find, rep.Find, terse())
		return 0
	}
	if *since && rep.Delta != nil {
		printDelta(rep.Delta)
		return 0
	}
	terseOut := terse()
	printGithubReport(rep, terseOut, *max)
	printSkew(rep.Skew, terseOut)
	printNear(rep.Near, terseOut)
	printFindings("# cross-repo matches", rep.Find, terseOut)
	printFindings("# audit findings", rep.Audit, terseOut)
	printFixes(rep.Fixes, terseOut)
	return 0
}

// discoverRepos finds git work trees under roots, at most depth levels down.
func discoverRepos(roots []string, depth int, owner string) []ghRepo {
	if depth < 0 {
		depth = 0
	}
	seen := map[string]bool{}
	var repos []ghRepo
	for _, root := range roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			abs = root
		}
		walkRepos(abs, 0, depth, seen, &repos)
	}
	if owner != "" {
		filtered := repos[:0]
		for _, r := range repos {
			if strings.EqualFold(r.Owner, owner) {
				filtered = append(filtered, r)
			}
		}
		repos = filtered
	}
	sort.Slice(repos, func(i, j int) bool {
		return strings.ToLower(repos[i].Name+repos[i].Owner) < strings.ToLower(repos[j].Name+repos[j].Owner)
	})
	return repos
}

func walkRepos(dir string, level, maxDepth int, seen map[string]bool, out *[]ghRepo) {
	if seen[dir] {
		return
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		return
	}
	if isGitRepo(dir) {
		seen[dir] = true
		*out = append(*out, ghRepo{Name: filepath.Base(dir), Path: dir})
		return
	}
	seen[dir] = true
	if level >= maxDepth {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || skipRepoDir(e.Name()) {
			continue
		}
		walkRepos(filepath.Join(dir, e.Name()), level+1, maxDepth, seen, out)
	}
}

func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func skipRepoDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "dist", "build", "bin", "testdata":
		return true
	}
	return false
}

// parseGitRemote extracts owner/name from common GitHub remote URL forms.
func parseGitRemote(raw string) (owner, name string) {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	if i := strings.Index(s, "github.com"); i >= 0 {
		s = s[i+len("github.com"):]
		s = strings.TrimLeft(s, ":/")
		if strings.HasPrefix(s, "git@") {
			s = strings.TrimPrefix(s, "git@")
		}
	} else if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[i+1:]
	}
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2], parts[len(parts)-1]
	}
	return "", ""
}

func loadRepo(r *ghRepo, redact bool) {
	if out, _, rc := runCmd("git", "-C", r.Path, "config", "--get", "remote.origin.url"); rc == 0 {
		owner, name := parseGitRemote(out)
		if owner != "" {
			r.Owner = owner
		}
		if name != "" {
			r.Name = name
		}
	}
	if out, _, rc := runCmd("git", "-C", r.Path, "log", "-1", "--format=%cs"); rc == 0 {
		r.Updated = strings.TrimSpace(out)
	}

	r.allFiles = collectFiles([]string{r.Path}, ghExtraExts...)
	langCount := map[string]int{}
	var deps []ghDep
	for _, f := range r.allFiles {
		base := filepath.Base(f)
		rel, _ := filepath.Rel(r.Path, f)
		ext := filepath.Ext(f)
		low := strings.ToLower(base)
		isConfig := ghConfigNames[low] || ghConfigNames[base]
		if isConfig {
			r.Configs = append(r.Configs, low)
		}
		if isScript(base, rel) && !isConfig {
			r.Scripts = append(r.Scripts, base)
		}
		if !filepath.IsLocal(rel) || strings.Contains(rel, string(os.PathSeparator)) {
			// top-level files only in the Files inventory
			continue
		}
		r.Files = append(r.Files, base)
		if !ghNonCodeExt[ext] {
			if lang := ghLang[ext]; lang != "" {
				langCount[lang]++
			}
		}
		deps = append(deps, depsFromManifest(f)...)
		r.Toolchain = append(r.Toolchain, extractToolchain(r.Path, f)...)
	}
	r.Files = uniqueSorted(r.Files, ghListCap)
	r.Configs = uniqueSorted(r.Configs, ghListCap)
	r.Scripts = uniqueSorted(r.Scripts, ghListCap)
	r.Deps = uniqueSorted(depNames(deps), ghListCap)
	r.DepVersions = uniqueSorted(depPairs(deps), ghListCap)
	r.Toolchain = uniqueSorted(r.Toolchain, ghListCap)
	r.Language = dominantLang(langCount, r.Configs)
	r.CI = detectCI(r.allFiles, r.Path)
	r.License = detectLicense(r.Path)
	r.Description = readmeSummary(r.Path, redact)
}

func isScript(base, rel string) bool {
	low := strings.ToLower(base)
	if strings.HasSuffix(low, ".sh") || strings.HasSuffix(low, ".bash") || strings.HasSuffix(low, ".ps1") {
		return true
	}
	switch low {
	case "makefile", "justfile", "jenkinsfile", "taskfile.yml", "dockerfile":
		return true
	}
	return strings.HasPrefix(rel, "scripts"+string(os.PathSeparator)) ||
		strings.HasPrefix(rel, "hack"+string(os.PathSeparator))
}

func uniqueSorted(in []string, capN int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	if len(out) > capN {
		out = out[:capN]
	}
	return out
}

func dominantLang(counts map[string]int, configs []string) string {
	cfg := map[string]bool{}
	for _, c := range configs {
		cfg[c] = true
	}
	// A dependency manifest is a stronger primary-language signal than a count
	// of small shell/CI files, which otherwise labels every repo "Shell".
	switch {
	case cfg["go.mod"]:
		return "Go"
	case cfg["pom.xml"], cfg["build.gradle"], cfg["build.gradle.kts"]:
		return "Java"
	case cfg["cargo.toml"]:
		return "Rust"
	case cfg["pyproject.toml"], cfg["requirements.txt"]:
		return "Python"
	case cfg["package.json"]:
		if counts["TypeScript"] > counts["JavaScript"] {
			return "TypeScript"
		}
		return "JavaScript"
	}
	best, bestN := "", 0
	for lang, n := range counts {
		if n > bestN || (n == bestN && lang < best) {
			best, bestN = lang, n
		}
	}
	return best
}

func detectCI(files []string, root string) string {
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		rel = filepath.ToSlash(rel)
		base := strings.ToLower(filepath.Base(f))
		switch {
		case strings.HasPrefix(rel, ".github/workflows/"):
			return "GitHub Actions"
		case base == "jenkinsfile":
			return "Jenkins"
		case base == ".gitlab-ci.yml":
			return "GitLab CI"
		case base == ".travis.yml":
			return "Travis CI"
		case strings.HasPrefix(rel, ".circleci/"):
			return "CircleCI"
		}
	}
	return ""
}

func detectLicense(dir string) string {
	for _, name := range []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING", "COPYING.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return "present"
		}
	}
	return ""
}

func readmeSummary(dir string, redact bool) string {
	for _, name := range []string{"README.md", "README.rst", "README.txt", "README", "readme.md"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		heading := ""
		for _, l := range lines {
			if m := reHeading.FindStringSubmatch(strings.TrimSpace(l)); len(m) > 0 {
				heading = cleanText(m[1], redact, 100)
				break
			}
		}
		for _, l := range lines {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "[![") ||
				strings.HasPrefix(t, "![") || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "---") {
				continue
			}
			if len(t) < 12 {
				continue
			}
			return cleanText(t, redact, 160)
		}
		return heading
	}
	return ""
}

func cleanText(s string, redact bool, limit int) string {
	s = strings.TrimSpace(s)
	if redact {
		s = redactLine(s)
	}
	return trunc(s, limit)
}

// ghDep keeps the version so the report can spot version skew across repos.
type ghDep struct {
	Name    string
	Version string
}

func normVersion(v string) string {
	v = strings.TrimLeft(strings.TrimSpace(v), "^~>=<")
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimSuffix(v, ",")
	return v
}

// depsFromManifest reads dependency names and versions from known manifests.
func depsFromManifest(path string) []ghDep {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	switch strings.ToLower(filepath.Base(path)) {
	case "go.mod":
		return depsGoMod(string(b))
	case "package.json":
		return depsPackageJSON(string(b))
	case "requirements.txt":
		return depsRequirements(string(b))
	case "pyproject.toml":
		return depsPyProject(string(b))
	case "cargo.toml":
		return depsCargo(string(b))
	case "pom.xml":
		var out []ghDep
		for _, m := range reMavenDep.FindAllStringSubmatch(string(b), -1) {
			out = append(out, ghDep{Name: m[1]})
		}
		return out
	case "build.gradle", "build.gradle.kts":
		var out []ghDep
		for _, m := range reGradleDep.FindAllStringSubmatch(string(b), -1) {
			out = append(out, ghDep{Name: m[1] + ":" + m[2]})
		}
		return out
	}
	return nil
}

func depsGoMod(s string) []ghDep {
	var out []ghDep
	inBlock := false
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "//", 2)[0])
		if line == "" {
			continue
		}
		if inBlock {
			if line == ")" {
				inBlock = false
				continue
			}
			f := strings.Fields(line)
			if len(f) > 0 {
				v := ""
				if len(f) > 1 {
					v = normVersion(f[1])
				}
				out = append(out, ghDep{reMajor.ReplaceAllString(f[0], ""), v})
			}
			continue
		}
		if line == "require (" {
			inBlock = true
			continue
		}
		if strings.HasPrefix(line, "require ") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				v := ""
				if len(f) > 2 {
					v = normVersion(f[2])
				}
				out = append(out, ghDep{reMajor.ReplaceAllString(f[1], ""), v})
			}
		}
	}
	return out
}

func depsPackageJSON(s string) []ghDep {
	var out []ghDep
	for _, block := range reDepBlock.FindAllStringSubmatch(s, -1) {
		for _, m := range reJSONDep.FindAllStringSubmatch(block[1], -1) {
			out = append(out, ghDep{m[1], normVersion(m[2])})
		}
	}
	return out
}

func depsRequirements(s string) []ghDep {
	var out []ghDep
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		if m := rePyReq.FindStringSubmatch(line); len(m) > 0 {
			out = append(out, ghDep{m[1], normVersion(m[2])})
		}
	}
	return out
}

func depsPyProject(s string) []ghDep {
	var out []ghDep
	inDeps := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inDeps = strings.HasPrefix(t, "[project]") || strings.Contains(t, "dependencies")
			continue
		}
		if !inDeps {
			continue
		}
		if m := reTomlDep.FindStringSubmatch(t); len(m) > 0 && m[1] != "python" {
			out = append(out, ghDep{strings.Trim(m[1], `"`), normVersion(strings.Trim(m[2], `"`))})
		}
	}
	return out
}

func depsCargo(s string) []ghDep {
	var out []ghDep
	inDeps := false
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") {
			inDeps = strings.HasPrefix(t, "[dependencies") || strings.HasPrefix(t, "[dev-dependencies")
			continue
		}
		if inDeps {
			if m := reTomlDep.FindStringSubmatch(t); len(m) > 0 {
				out = append(out, ghDep{strings.Trim(m[1], `"`), normVersion(strings.Trim(m[2], `"`))})
			}
		}
	}
	return out
}

func depNames(deps []ghDep) []string {
	out := make([]string, 0, len(deps))
	for _, d := range deps {
		out = append(out, d.Name)
	}
	return out
}

func depPairs(deps []ghDep) []string {
	out := make([]string, 0, len(deps))
	for _, d := range deps {
		out = append(out, d.Name+"@"+d.Version)
	}
	return out
}

func analyzePatterns(repos []ghRepo, top int) ghPatterns {
	total := len(repos)
	return ghPatterns{
		Languages: countBy(repos, total, func(r ghRepo) []string { return []string{r.Language} }, top),
		CI:        countBy(repos, total, func(r ghRepo) []string { return []string{r.CI} }, top),
		Licenses:  countBy(repos, total, func(r ghRepo) []string { return []string{r.License} }, top),
		Configs:   countBy(repos, total, func(r ghRepo) []string { return r.Configs }, top),
		Deps:      countBy(repos, total, func(r ghRepo) []string { return r.Deps }, top),
		Scripts:   countBy(repos, total, func(r ghRepo) []string { return r.Scripts }, top),
		Toolchain: countBy(repos, total, func(r ghRepo) []string { return r.Toolchain }, top),
		Topics:    countBy(repos, total, func(r ghRepo) []string { return r.Topics }, top),
	}
}

func countBy(repos []ghRepo, total int, pick func(ghRepo) []string, top int) []ghCount {
	counts := map[string]int{}
	for _, r := range repos {
		seen := map[string]bool{}
		for _, v := range pick(r) {
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			counts[v]++
		}
	}
	items := make([]ghCount, 0, len(counts))
	for name, n := range counts {
		pct := 0.0
		if total > 0 {
			pct = float64(n) / float64(total) * 100
		}
		items = append(items, ghCount{Name: name, Repos: n, Pct: pct})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Repos != items[j].Repos {
			return items[i].Repos > items[j].Repos
		}
		return items[i].Name < items[j].Name
	})
	if len(items) > top {
		items = items[:top]
	}
	return items
}

// normalizeForHash strips blank/comment lines and whitespace so that trivial
// formatting differences do not hide a duplicated script. Redaction is applied
// before hashing so secrets never become part of a fingerprint or preview.
func normalizeForHash(b []byte, redact bool) string {
	s := string(b)
	if redact {
		s = redactLine(s)
	}
	out := make([]string, 0, 32)
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "#") ||
			strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") ||
			strings.HasPrefix(t, "--") {
			continue
		}
		out = append(out, reSpace.ReplaceAllString(t, " "))
	}
	return strings.Join(out, "\n")
}

func findDuplicates(repos []ghRepo, minBytes, maxGroups int, redact bool) []ghDup {
	type acc struct {
		paths   []string
		repos   map[string]bool
		bytes   int
		preview string
	}
	groups := map[string]*acc{}
	repoNames := make(map[string]string, len(repos))
	for _, r := range repos {
		repoNames[r.Path] = r.Name
		if r.Owner != "" {
			repoNames[r.Path] = r.Owner + "/" + r.Name
		}
	}
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
			h := sha256.Sum256([]byte(norm))
			key := hex.EncodeToString(h[:])[:12]
			a := groups[key]
			if a == nil {
				a = &acc{repos: map[string]bool{}, preview: cleanText(firstLine(norm), redact, 120)}
				groups[key] = a
			}
			rel, _ := filepath.Rel(r.Path, f)
			a.paths = append(a.paths, repoNames[r.Path]+":"+filepath.ToSlash(rel))
			a.repos[r.Path] = true
			a.bytes += len(b)
		}
	}
	var dups []ghDup
	for key, a := range groups {
		if len(a.repos) < 2 {
			continue
		}
		sort.Strings(a.paths)
		dups = append(dups, ghDup{
			Hash: key, Repos: len(a.repos), Files: len(a.paths),
			Bytes: a.bytes, Paths: a.paths, Preview: a.preview,
		})
	}
	sort.Slice(dups, func(i, j int) bool {
		if dups[i].Repos != dups[j].Repos {
			return dups[i].Repos > dups[j].Repos
		}
		return dups[i].Bytes > dups[j].Bytes
	})
	if len(dups) > maxGroups {
		dups = dups[:maxGroups]
	}
	return dups
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func printGithubReport(rep ghReport, terseOut bool, max int) {
	fmt.Printf("# projects: %d\n", len(rep.Repos))
	shown := rep.Repos
	if len(shown) > max {
		shown = shown[:max]
	}
	for _, r := range shown {
		name := r.Name
		if r.Owner != "" {
			name = r.Owner + "/" + r.Name
		}
		if terseOut {
			fmt.Printf("%s\t%s\t%s\t%s\n", name, r.Language, r.CI, r.Updated)
			continue
		}
		desc := ""
		if r.Description != "" {
			desc = "  " + r.Description
		}
		fmt.Printf("%-38s %-10s %-15s %s%s\n", trim(name, 37), r.Language, r.CI, r.Updated, desc)
	}
	if len(rep.Repos) > len(shown) {
		fmt.Printf("... %d more projects (use --max)\n", len(rep.Repos)-len(shown))
	}

	printCounts("# languages", rep.Patterns.Languages, terseOut)
	printCounts("# ci", rep.Patterns.CI, terseOut)
	printCounts("# licenses", rep.Patterns.Licenses, terseOut)
	printCounts("# common configs", rep.Patterns.Configs, terseOut)
	printCounts("# common deps", rep.Patterns.Deps, terseOut)
	printCounts("# common scripts", rep.Patterns.Scripts, terseOut)
	printCounts("# toolchain", rep.Patterns.Toolchain, terseOut)
	printCounts("# topics", rep.Patterns.Topics, terseOut)

	fmt.Printf("# duplicate files/scripts: %d groups\n", len(rep.Duplicates))
	for _, d := range rep.Duplicates {
		if terseOut {
			fmt.Printf("%s\t%d\t%d\t%s\n", d.Hash, d.Repos, d.Files, strings.Join(d.Paths, ","))
			continue
		}
		fmt.Printf("%s  %d repos  %d files  %s\n", d.Hash, d.Repos, d.Files, strings.Join(d.Paths, ", "))
		if d.Preview != "" {
			fmt.Printf("    %s\n", d.Preview)
		}
	}
}

func printCounts(title string, items []ghCount, terseOut bool) {
	if len(items) == 0 {
		return
	}
	fmt.Println(title)
	for _, c := range items {
		if terseOut {
			fmt.Printf("%s\t%d\t%.0f%%\n", c.Name, c.Repos, c.Pct)
			continue
		}
		fmt.Printf("  %-34s %3d repos  %5.1f%%\n", trim(c.Name, 33), c.Repos, c.Pct)
	}
}
