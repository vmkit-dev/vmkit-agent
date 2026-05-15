package resetvm

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/vmkit-dev/vmkit-agent/internal/openport"
	"github.com/vmkit-dev/vmkit-agent/pkg/types"
)

// Canonical `ufw status numbered` sample mixing baseline rules, one custom
// SSH port (2222), a leftover postgres port from a killed instance, one
// IPv6 blanket, and one source-CIDR-scoped rule on a now-dead port.
const ufwStatusSample = `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 2222/tcp                   ALLOW IN    Anywhere
[ 2] 80/tcp                     ALLOW IN    Anywhere
[ 3] 443/tcp                    ALLOW IN    Anywhere
[ 4] 15432/tcp                  ALLOW IN    203.0.113.4
[ 5] 15432/tcp                  ALLOW IN    Anywhere
[ 6] 2222/tcp (v6)              ALLOW IN    Anywhere (v6)
[ 7] 80/tcp (v6)                ALLOW IN    Anywhere (v6)
[ 8] 443/tcp (v6)               ALLOW IN    Anywhere (v6)
[ 9] 9090/tcp                   ALLOW IN    10.0.0.0/24
[10] 15432/tcp (v6)             ALLOW IN    Anywhere (v6)
`

func TestPlanUFWReset(t *testing.T) {
	rules := openport.ParseAllUFWAllowRules(ufwStatusSample)
	if len(rules) == 0 {
		t.Fatal("parser returned no rules — fixture or regex broke")
	}

	tests := []struct {
		name         string
		allowedPorts []int
		wantDelete   []int
	}{
		{
			// Plain SSH-on-22 baseline — keeps 22/80/443 only. Our sample
			// has no :22 rules, so the custom 2222 rules must be purged too.
			name:         "default baseline drops 2222 + stale app ports",
			allowedPorts: []int{22, 80, 443},
			wantDelete:   []int{10, 9, 6, 5, 4, 1}, // desc order
		},
		{
			// Hardened VM on SSH :2222 — keep 2222/80/443, purge leftovers.
			name:         "hardened baseline keeps 2222",
			allowedPorts: []int{2222, 80, 443},
			wantDelete:   []int{10, 9, 5, 4}, // 1/6 kept (2222), 2/3/7/8 kept (80/443)
		},
		{
			// Empty allowed list currently means "delete everything". This is
			// intentional: the caller is expected to pass a non-empty list
			// via the Reset() wrapper which defaults to DefaultAllowedPorts.
			// Sanity-check: no rule is kept.
			name:         "empty allowed deletes everything",
			allowedPorts: nil,
			wantDelete:   []int{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
		},
		{
			// Idempotency — a post-reset rerun where only the baseline is
			// left on the firewall should plan zero deletes.
			name: "no-op when only baseline remains",
			// Simulate post-reset state by restricting rules to what a
			// clean baseline would contain.
			allowedPorts: []int{2222, 80, 443},
			// (rules filtered below)
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inputRules := rules
			if tc.name == "no-op when only baseline remains" {
				inputRules = filterByPort(rules, []int{2222, 80, 443})
				tc.wantDelete = nil
			}
			got := PlanUFWReset(inputRules, tc.allowedPorts)
			if !sliceEq(got, tc.wantDelete) {
				t.Errorf("PlanUFWReset(%v) = %v, want %v",
					tc.allowedPorts, got, tc.wantDelete)
			}
			if !sort.IntsAreSorted(reverseInts(got)) {
				t.Errorf("result not in descending order: %v", got)
			}
		})
	}
}

// Defense-in-depth against a classic bug: if we accidentally returned ascending
// rule numbers, UFW's renumber-on-delete would shift remaining rules and the
// wrong ones would get killed. PlanUFWReset's contract is descending — this
// test locks that in across any rule shape.
func TestPlanUFWResetAlwaysDescending(t *testing.T) {
	rules := openport.ParseAllUFWAllowRules(ufwStatusSample)
	got := PlanUFWReset(rules, []int{22})
	for i := 1; i < len(got); i++ {
		if got[i] >= got[i-1] {
			t.Fatalf("expected strictly descending, got %v", got)
		}
	}
}

func TestListInstanceSubdirs(t *testing.T) {
	tmp := t.TempDir()

	// Mix of dirs, hidden dirs, and a regular file.
	for _, dir := range []string{"alpha", "beta-sub", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(tmp, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := listInstanceSubdirs(tmp)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "beta-sub"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("listInstanceSubdirs = %v, want %v", got, want)
	}
}

func TestListInstanceSubdirsMissingBaseDir(t *testing.T) {
	// A fresh VM may not have /opt/supabase yet — treat as empty, not error.
	got, err := listInstanceSubdirs(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Errorf("missing base dir should not error, got %v", err)
	}
	if got != nil {
		t.Errorf("want nil, got %v", got)
	}
}

// Structural smoke test: Reset must emit all four expected steps in order,
// even with empty config (no instances, docker/ufw may be absent in CI/macOS).
// Step statuses may be "completed" or "failed" depending on host — we only
// verify the step *names* and ordering here.
func TestResetEmitsExpectedSteps(t *testing.T) {
	tmp := t.TempDir()
	cfg := &types.ResetVMConfig{BaseDir: tmp, AllowedPorts: []int{22}}
	result := Reset(cfg)

	wantNames := []string{"purge_instances", "prune_docker", "reset_ufw", "cleanup_configs"}
	if len(result.Steps) != len(wantNames) {
		t.Fatalf("got %d steps, want %d: %+v", len(result.Steps), len(wantNames), result.Steps)
	}
	for i, name := range wantNames {
		if result.Steps[i].Name != name {
			t.Errorf("step %d: got %q, want %q", i, result.Steps[i].Name, name)
		}
	}
}

// --- helpers ---

func filterByPort(rules []openport.ExistingRule, allowed []int) []openport.ExistingRule {
	keep := map[int]bool{}
	for _, p := range allowed {
		keep[p] = true
	}
	var out []openport.ExistingRule
	for _, r := range rules {
		if keep[r.Port] {
			out = append(out, r)
		}
	}
	return out
}

func sliceEq(a, b []int) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func reverseInts(in []int) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
