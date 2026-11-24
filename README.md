# shave

**Shave AI token usage before it reaches the model.**

`shave` is a small, dependency-free Go CLI that compresses, filters and
summarises the inputs you hand to an AI: logs, `kubectl` output, code context,
and repetitive command output. Less input → fewer tokens → lower cost, with no
loss of the signal the model actually needs.

```
kubectl logs deploy/api -n prod --tail=5000 | shave logs --errors
kubectl get pods -A -o json | shave k8s pods
shave ctx ./src -q "retry logic" --budget 4000
go test ./... | shave test
git diff | shave diff --review
shave pack ./src -q "retry logic" --budget 3000
shave github ~/src --depth 2 --find "retry logic"
echo "..." | shave tokens -m claude-sonnet-4
```

## Why

Most token spend is not the answer — it is the question. Raw logs, full
manifests, and whole repositories get pasted into context when a 2 KB summary
carries the same information. `shave` puts a compression step between
*the world* and *the model*.

Core rules (inherited from the `probe` philosophy):

- Everything is a **filter on stdin or on a file**. Composable with pipes.
- Everything prints **pointers / compact summaries**, never full dumps, unless you ask.
- **Secrets are redacted by default.** You should never have to remember.
- **Zero network**, zero runtime dependencies. One static binary.

## Install

```bash
cd go
go build -o shave ./cmd/shave
# then put it on PATH, e.g.
sudo mv shave /usr/local/bin/shave
```

Or via the helper:

```bash
./install.sh
```

## MCP server (optional)

`shave` can speak MCP over stdio so agents can call every filter mid-session:
`shave_logs`, `shave_tokens`, `shave_k8s_pods`, `shave_k8s_events`, `shave_ctx`,
`shave_github`, `shave_test`, `shave_diff` and `shave_pack`. Repeated identical
calls return `[unchanged]` from a 5-minute result cache; pass `refresh: true`
to bypass it.

```bash
shave mcp
```

Example OpenCode config:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "shave": { "type": "local", "command": ["shave", "mcp"] }
    }
  }
}
```

## Commands

| Command | What it does |
|---|---|
| `shave tokens [file]` | Count tokens (chars/4) and estimate USD cost for a model |
| `shave logs [file]` | Dedupe, truncate, redact logs; `--errors` keeps relevant lines + context |
| `shave test [file]` | Test output: hide passes, keep failures + context |
| `shave diff [file]` | Compress a diff; `--review` keeps only declaration/secret/TODO changes |
| `shave pack <paths...>` | Fit the most relevant context into an explicit token budget |
| `shave k8s pods` | Compact pod status table from `kubectl get pods -o json` |
| `shave k8s events` | Deduped, compact event list |
| `shave k8s diagnose <kind/name>` | One report: describe + events + (for pods) tail logs |
| `shave ctx <paths> [-q query]` | Prune code to declarations/query snippets; `--map` gives a compact outline |
| `shave cache get/set/stats/clear` | Reuse expensive outputs across runs |
| `shave run -- <cmd>` | Run a command; on repeat identical output prints `[unchanged]` instead |
| `shave github [paths...] [--depth N]` | Analyze local git clones: inventory, shared patterns, duplicated files/scripts |

## Token savings in practice

Typical reductions (measured on the author's workloads):

- `kubectl logs` (5 k lines) → `shave logs --errors`: **~95%**
- `kubectl get pods -o json` (50 pods) → `shave k8s pods`: **~90%**
- Whole-repo paste → `shave ctx -q topic`: **~80%**
- Re-running a diagnostic command → `shave run --`: **100%** on unchanged runs

Estimate any payload with `shave tokens` before you send it.

## Project portfolio (local clones)

`shave github` scans directories for local git work trees — no network, no
`gh` token needed — and builds a compact portfolio report:

```bash
shave github ~/src --depth 2 --owner rahulkj --top 10 --max 20
shave github ~/src --json > portfolio.json
```

It prints one line per project (owner/name, primary language, CI, last commit,
redacted README description), then cross-repo commonalities — languages, CI,
licenses, config files, dependencies, scripts and toolchain pins — with counts
and percentages. It also reports **version skew** (the same dependency pinned
at several versions) and fingerprints normalized file contents to list exact
and near-duplicate files as consolidation candidates.

Focused modes print only what you asked for:

```bash
shave github ~/src --find "retry logic"        # existing implementations + paths
shave github ~/src --profile                    # ~1 KB house-style block for AGENTS.md
shave github ~/src --audit                      # secret/hygiene findings, paths only
shave github ~/src --near --fixes               # near-duplicates + repeated fixes
shave github ~/src --since                      # only what changed since last run
gh api 'users/rahulkj/repos?per_page=100' | shave github --from-stdin --json
```

Secrets are redacted *before* hashing and printing, so they never leak into a
fingerprint; use `--no-redact` to opt out and `--min-bytes` to tune the
duplication threshold. Flags work before or after the paths. The `--since`
report is small enough to run as a scheduled OpenCode automation that posts
only portfolio deltas.

## Kubernetes

A sample Deployment lives in `k8s/deployment.yaml`. `Dockerfile` builds a
minimal image (multi-stage, non-root). The pod runs `sleep infinity` so you can
`kubectl exec` into it and run `shave` against in-cluster outputs:

```bash
kubectl apply -f k8s/deployment.yaml
kubectl exec -it deploy/shave -- shave k8s pods -A
```

## Development

```bash
cd go
go build ./...
go test ./...
gofmt -w .
```

## License

MIT (add as desired).