package handlers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/vmkit-dev/vmkit-agent/internal/deploy"
	"github.com/vmkit-dev/vmkit-agent/internal/diagnose"
	"github.com/vmkit-dev/vmkit-agent/internal/enabletls"
	"github.com/vmkit-dev/vmkit-agent/internal/harden"
	"github.com/vmkit-dev/vmkit-agent/internal/health"
	"github.com/vmkit-dev/vmkit-agent/internal/openport"
	"github.com/vmkit-dev/vmkit-agent/internal/rotatecreds"
	"github.com/vmkit-dev/vmkit-agent/internal/upgrade"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

func HealthCheck(_ context.Context, params json.RawMessage) (any, error) {
	var cfg types.EnhancedHealthCheckConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return health.CheckEnhanced(&cfg)
}

func VMHarden(_ context.Context, params json.RawMessage) (any, error) {
	var cfg types.HardenConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	result := harden.Harden(&cfg)
	if !result.Success {
		return result, fmt.Errorf("harden failed: %s", result.ErrorMessage)
	}
	return result, nil
}

func VMOpenPort(_ context.Context, params json.RawMessage) (any, error) {
	var cfg types.OpenPortConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	result := openport.OpenPort(&cfg)
	if !result.Success {
		return result, fmt.Errorf("open-port failed: %s", result.ErrorMessage)
	}
	return result, nil
}

func VMUpgrade(_ context.Context, params json.RawMessage) (any, error) {
	var cfg types.UpgradeConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	result := upgrade.Upgrade(cfg)
	if !result.Success {
		return result, fmt.Errorf("upgrade failed: %s", result.ErrorMessage)
	}
	return result, nil
}

func VMDiagnose(_ context.Context, params json.RawMessage) (any, error) {
	var cfg types.DiagnoseConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return diagnose.Diagnose(&cfg)
}

func KamalDeploy(_ context.Context, params json.RawMessage) (any, error) {
	var cfg types.DeployConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	result := deploy.Deploy(&cfg)
	if !result.Success {
		return result, fmt.Errorf("deploy failed: %s", result.ErrorMessage)
	}
	return result, nil
}

func TLSIssue(_ context.Context, params json.RawMessage) (any, error) {
	var cfg types.EnableTLSConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	result := enabletls.EnableTLS(&cfg)
	if !result.Success {
		return result, fmt.Errorf("tls failed: %s", result.ErrorMessage)
	}
	return result, nil
}

func CredsRotate(_ context.Context, params json.RawMessage) (any, error) {
	var cfg types.RotateCredentialsConfig
	if err := json.Unmarshal(params, &cfg); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	result := rotatecreds.RotateCredentials(&cfg)
	if !result.Success {
		return result, fmt.Errorf("rotate-credentials failed: %s", result.ErrorMessage)
	}
	return result, nil
}
