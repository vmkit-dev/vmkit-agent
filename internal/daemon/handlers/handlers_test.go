package handlers

import (
	"context"
	"encoding/json"
	"testing"
)

func TestHealthCheckInvalidParams(t *testing.T) {
	_, err := HealthCheck(context.Background(), json.RawMessage(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestVMHardenInvalidParams(t *testing.T) {
	_, err := VMHarden(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestVMOpenPortInvalidParams(t *testing.T) {
	_, err := VMOpenPort(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestVMUpgradeInvalidParams(t *testing.T) {
	_, err := VMUpgrade(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestVMDiagnoseInvalidParams(t *testing.T) {
	_, err := VMDiagnose(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestKamalDeployInvalidParams(t *testing.T) {
	_, err := KamalDeploy(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestTLSIssueInvalidParams(t *testing.T) {
	_, err := TLSIssue(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestCredsRotateInvalidParams(t *testing.T) {
	_, err := CredsRotate(context.Background(), json.RawMessage(`not-json`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}
