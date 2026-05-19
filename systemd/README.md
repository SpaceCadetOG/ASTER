# Systemd Folder Status

The old Pi `systemd` orchestration units were removed in cleanup patch 01.

What remains here are environment templates only:

- `systemd/env/live.env.example`
- `systemd/env/long.env.example`
- `systemd/env/short.env.example`
- `systemd/env/tape.env.example`
- `systemd/env/whale.env.example`
- `systemd/env/liqs.env.example`
- `systemd/env/oflow.env.example`

Use them as host configuration examples while the new GCP/runtime deployment model is being defined.

For manual local runs, use commands such as:

```bash
go run ./cmd/live
go run ./cmd/long
go run ./cmd/short
go run ./cmd/tape
go run ./cmd/whale
go run ./cmd/liqs
go run ./cmd/oflow
```
