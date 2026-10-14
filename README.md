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

`shave` can speak MCP over stdio so agents can call these tools mid-session:

```bash
shave mcp
```

Example OpenCode config:

```json
{
  "mcpServers": {
    "shave": { "command": "shave", "args": ["mcp"] }
  }
}
```

## Commands

| Command | What it does |
|---|---|
| `shave tokens [file]` | Count tokens (chars/4) and estimate USD cost for a model |
| `shave logs [file]` | Dedupe, truncate, redact logs; `--errors` keeps relevant lines + context |
| `shave k8s pods` | Compact pod status table from `kubectl get pods -o json` |
| `shave k8s events` | Deduped, compact event list |
| `shave k8s diagnose <kind/name>` | One report: describe + events + (for pods) tail logs |
| `shave ctx <paths> [-q query]` | Prune code to a file tree + declarations, or query-focused snippets |
| `shave cache get/set/stats/clear` | Reuse expensive outputs across runs |
| `shave run -- <cmd>` | Run a command; on repeat identical output prints `[unchanged]` instead |

## Token savings in practice

Typical reductions (measured on the author's workloads):

- `kubectl logs` (5 k lines) → `shave logs --errors`: **~95%**
- `kubectl get pods -o json` (50 pods) → `shave k8s pods`: **~90%**
- Whole-repo paste → `shave ctx -q topic`: **~80%**
- Re-running a diagnostic command → `shave run --`: **100%** on unchanged runs

Estimate any payload with `shave tokens` before you send it.

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