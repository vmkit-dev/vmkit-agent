# vmkit-agent — seeded from supabyoi/agent (2026-05-15)

This repo was seeded from `~/supabyoi/agent/` (Mac, commit prior to 2026-05-15).
Git history is **not** preserved — fresh start under the `vmkit-dev` org.

Module path renamed: `github.com/badri/supabyoi/agent` → `github.com/vmkit-dev/vmkit-agent`.
Binary renamed: `supabyoi-agent` → `vmkit-agent`. `go build ./...` is green at seed time.

## Follow-up beads to file in the planning phase

### vk-?: Strip Supabase-specific code paths
30 Go files still reference Supabase. Per `yc-2026-research-mcp-pivot.md` §3, the
agent's internal packages are 95% generic; the Supabase-specific bits are mostly:
- `internal/rotatecreds/` — Supabase JWT rotation; generalize to a credential-rotation plugin
- `internal/backup/`, `internal/destroy/`, `internal/resetvm/` — references to
  Supabase volume names / docker-compose service names
- `internal/compose/`, `internal/nginx/` — Supabase-stack-specific templates
- `cmd/vmkit-agent/main.go` — startup banner / labels

Reference: `grep -rl -i supabase --include="*.go" .` for the canonical list.

### vk-?: Add daemon mode (WS pull uplink)
Per `mcp-agent-surface.md` §1.6.2(C) and `yc-2026-research-mcp-pivot.md` §11.
- New subcommand: `vmkit-agent daemon --gateway-url <wss://...> --instance-id <uuid> --token <bootstrap>`
- Persistent outbound WebSocket with heartbeat + auto-reconnect (exp backoff, 60s cap)
- JSON-RPC request dispatcher routing into existing `internal/*` packages
- Bootstrap token rotation on first connect; session token at `/etc/vmkit/session`
- Systemd unit installed alongside the binary

### vk-?: Replace Supabase-preset assumptions with config
Today every internal package "knows" about Supabase. Goal: make presets *data*, not
code (per yc-pivot §3 unified-engine architecture). The "plain Docker app" preset
is the v1 vmkit Solo SKU.

### vk-?: Wire to vmkit-backend WS daemon server
Once `vmkit-backend` implements the daemon-side WS endpoint, hook this agent's
daemon mode up. End-to-end test: provision a fresh Hetzner cax11, install daemon
via cloud-init, see it connect and heartbeat.

## Dormant subsystems

### `internal/deploy/` — supabyoi heritage, not wired into vmkit deploys (vk-d0m, 2026-05-18)

`internal/deploy/deploy.go` implements a 7-step Docker Compose orchestration
(pull → nginx → config → compose → health → nginx-config → TLS) and registers a
`kamal.deploy` JSON-RPC handler. **vmkit's deploy path does not call it.**

The actual flow:

1. `vmkit-backend` dispatches the `vmkit-deploy.yml` GitHub Actions workflow
   (see `vmkit_backend.api.internal.deploy_impl`).
2. The workflow runs `kamal deploy` from CI against the target VM over SSH.

The handler stays here as a **v2 candidate** for the day we want to drop the
GH Actions dependency and drive deploys straight into the on-VM daemon. Until
then, treat `internal/deploy/` and its supabyoi-shaped templates as inert —
extending them without first re-wiring the deploy path is wasted effort.
