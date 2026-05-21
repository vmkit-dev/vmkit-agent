package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vmkit-dev/vmkit-agent/internal/daemon"
	"github.com/vmkit-dev/vmkit-agent/internal/selfupgrade"
)

// AgentUpgrade handles the vm.upgrade RPC (vk-phz): it downloads the latest
// vmkit-agent binary from the backend-supplied URL, atomically swaps the
// running binary, and schedules a systemd restart.
//
// The RPC response is sent before the restart fires — selfupgrade delays the
// `systemctl restart` by a few seconds — so the backend reliably sees the
// result frame before this process is replaced.
func AgentUpgrade(_ context.Context, params json.RawMessage) (any, error) {
	var cfg selfupgrade.Config
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	res := selfupgrade.Run(cfg, selfupgrade.Options{CurrentVersion: daemon.Version})
	if !res.Success {
		return res, fmt.Errorf("agent upgrade failed: %s", res.Error)
	}
	return res, nil
}
