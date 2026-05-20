# Systemd Folder Status

The old Pi `systemd` orchestration units were removed in cleanup patch 01.

The repo now carries two systemd-related assets:

- `systemd/env/*.env.example`
  - host configuration examples for each runtime
- `systemd/gcp/*.service`
  - persistent GCP-oriented host units for the current VM-based deployment model

Current unit files:

- `systemd/gcp/aster-long.service`
- `systemd/gcp/aster-short.service`
- `systemd/gcp/aster-tape.service`
- `systemd/gcp/aster-whale.service`
- `systemd/gcp/aster-liqs.service`
- `systemd/gcp/aster-oflow.service`
- `systemd/gcp/aster-live.service`

These units:

- assume binaries are already built into `/opt/aster/bin`
- assume env files live under `/opt/aster/env`
- run from `/opt/aster/repo`
- rely on systemd for restart behavior and journald for process logs

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

For the GCP host install sequence, see:

- [docs/gcp/phase_08_persistent_services.md](/Users/victorogbebor/2026/go-machine/docs/gcp/phase_08_persistent_services.md:1)
