# agent-cli-sdk

`agentcli` is one command-line interface for driving agent CLIs (Codex today,
Claude Code next) from Claude Code, hooks and scripts: one-shot runs and
resumable conversations, in the foreground or as background jobs, with an
append-only telemetry stream that accepts new attributes without code changes.

It ships as a Claude Code plugin with prebuilt static binaries, so installing
it needs nothing besides the provider CLI itself.

Status: under construction. The requirements are in [`docs/PRD.md`](docs/PRD.md),
the verification checklist in [`docs/test-cases.md`](docs/test-cases.md), and
the shared implementation decisions in [`docs/FRAME.md`](docs/FRAME.md).

## Development

Everything runs inside a pinned Go container; the host needs only Docker.

```sh
make test   # go test ./...
make vet    # go vet ./...
make build  # build/agentcli
```

## License

MIT — see [LICENSE](LICENSE).
