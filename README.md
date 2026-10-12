# vmkit-agent

The on-VM agent for [VMKit](https://vmkit.dev). A single static Go binary that runs on each
customer VM, keeps an outbound connection to the VMKit control plane, and executes
host-level operations on its behalf: hardening, firewall changes, diagnostics, log and
metric collection, container lifecycle, and its own upgrades.

It also works as a one-shot CLI: each subcommand reads a JSON config file and prints a JSON
result, so the same operations can be driven over SSH.

## Why a pull-based agent

The VM dials out; the control plane never needs inbound SSH or an open management port.

- **Outbound WebSocket, JSON-RPC 2.0.** `vmkit-agent daemon` connects to the gateway and
  serves requests over a single long-lived socket.
- **Token rotation.** The first connection authenticates with a single-use bootstrap token
  (`/etc/vmkit/token`). The gateway's welcome message returns a session token, which the
  agent persists (`/etc/vmkit/session`) and uses from then on.
- **Reconnect with backoff.** Dropped connections retry with exponential backoff from 1s,
  capped at 60s. A heartbeat loop runs at the interval the gateway assigns.
- **No dependencies on the host.** One binary, two Go module dependencies
  (`gorilla/websocket`, `x/sync`), built for linux/amd64 and linux/arm64.

```
vmkit-agent daemon \
  --gateway-url wss://gateway.vmkit.dev/agent \
  --instance-id <uuid> \
  --token-file /etc/vmkit/token
```

## RPC methods (daemon mode)

| Method | What it does |
|---|---|
| `health.check` | Liveness and basic host health |
| `vm.harden` | SSH, firewall and fail2ban hardening (see below) |
| `vm.openport` | Open a UFW port, optionally restricted to source IPs |
| `vm.diagnose` | Host and container diagnostics |
| `vm.upgrade` | Download a newer agent release, swap the binary atomically, and restart via systemd after replying |
| `tls.issue` | Issue a TLS certificate |
| `creds.rotate` | Rotate service credentials |
| `logs.fetch` | Tail container logs |
| `metrics.collect` | Host and container metrics |
| `containers.stop` | Stop and remove an environment's containers |
| `kamal.deploy` | Dormant; see Status |

Registration lives in `internal/daemon/handlers/register.go`.

## Hardening safety

`vm.harden` / `vmkit-agent harden` changes SSH and firewall settings on a live machine, so a
mistake can lock everyone out. `internal/harden/harden.go` guards against that:

- pre-flight checks before touching anything,
- an SSH connectivity re-test after the SSH changes, with automatic rollback if it fails,
- an emergency rollback script written to the VM for manual recovery.

## CLI mode

```
vmkit-agent <command> --config <file.json>
```

Commands include `harden`, `diagnose`, `health`, `open-port`, `update-allowlist`,
`backup`, `restore`, `verify`, `rotate-credentials` and `reset-vm`. Run `vmkit-agent` with
no arguments for the full list. Request and response shapes are JSON Schemas in
`schemas/`, with examples in `test-fixtures/`.

## Building

```bash
make build          # current OS -> bin/vmkit-agent
make build-linux    # bin/vmkit-agent-linux-{amd64,arm64}
make test
```

The version is stamped from `git describe --tags`. Pushing a `v*` tag runs
`.github/workflows/release.yml`, which tests, builds both architectures, and attaches the
binaries to the matching GitHub release.

## Status

This agent began as the deploy agent for Supabyoi (managed self-hosted Supabase) and was
seeded into VMKit on 2026-05-15. The daemon, hardening, firewall, diagnostics, logs,
metrics and self-upgrade paths are used by VMKit. Some packages still carry
Supabase-specific assumptions and are not on VMKit's deploy path:

- `internal/deploy/` (Docker Compose orchestration behind `kamal.deploy`). VMKit deploys
  run `kamal deploy` from GitHub Actions instead.
- `internal/backup/` (including restore and verify), `internal/destroy/`,
  `internal/resetvm/`, `internal/compose/`, `internal/rotatecreds/` and `internal/upgrade/`,
  which reference Supabase service and volume names.

`TODO.md` tracks generalizing these.

## License

Apache-2.0. See [LICENSE](LICENSE).
