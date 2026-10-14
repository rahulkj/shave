# AGENTS.md — shave

Small Go CLI that reduces AI token usage by compressing logs, k8s output, code
context and repetitive command output before it reaches the model.

## Conventions

- Single static binary, standard library only. No runtime deps.
- Everything is a stdin/file filter; commands are composable with pipes.
- Default to redacting secrets. Never print raw credentials.
- Terse mode via `SHAVE_TERSE=1` for agent-sized output.
- Keep changes small; prefer adding a flag over a new command.

## Build

```bash
cd go && go build ./... && go test ./...
```