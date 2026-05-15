package openport

import (
	"reflect"
	"testing"
)

func TestParseUFWRulesForPort(t *testing.T) {
	// Sample `ufw status numbered` output covering:
	// - unrelated port (should be filtered)
	// - blanket "Anywhere" rule
	// - specific-IP rule without /32 (should normalize to /32)
	// - CIDR rule (passed through)
	// - IPv6 blanket rule (should normalize to ::/0)
	status := `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] 15432/tcp                  ALLOW IN    Anywhere
[ 3] 15432/tcp                  ALLOW IN    203.0.113.4
[ 4] 15432/tcp                  ALLOW IN    198.51.100.0/24
[ 5] 15432/tcp (v6)             ALLOW IN    Anywhere (v6)
[ 6] 15432/udp                  ALLOW IN    10.0.0.0/8
`
	got := ParseUFWRulesForPort(status, 15432, "tcp")
	want := []ExistingRule{
		{RuleNumber: 2, Port: 15432, Protocol: "tcp", Source: "0.0.0.0/0"},
		{RuleNumber: 3, Port: 15432, Protocol: "tcp", Source: "203.0.113.4/32"},
		{RuleNumber: 4, Port: 15432, Protocol: "tcp", Source: "198.51.100.0/24"},
		{RuleNumber: 5, Port: 15432, Protocol: "tcp", Source: "::/0", IPv6: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseUFWRulesForPort mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDiffAllowlist_NarrowFromBlanketToSpecific(t *testing.T) {
	// Common path: current state is "open to the world" (left over from a
	// pre-allowlist deploy) and the user asks to narrow to two sources.
	existing := []ExistingRule{
		{RuleNumber: 2, Source: "0.0.0.0/0"},
		{RuleNumber: 5, Source: "::/0", IPv6: true},
	}
	del, add := DiffAllowlist(existing, []string{"203.0.113.4/32", "198.51.100.0/24"})
	// Both existing rules must go; two new ones added.
	if !reflect.DeepEqual(del, []int{5, 2}) {
		t.Errorf("expected reverse-sorted delete [5, 2], got %v", del)
	}
	if !reflect.DeepEqual(add, []string{"198.51.100.0/24", "203.0.113.4/32"}) {
		t.Errorf("unexpected add set: %v", add)
	}
}

func TestDiffAllowlist_KeepsMatchingRules(t *testing.T) {
	// When the existing state already matches part of the desired set, those
	// rules must not be deleted-and-re-added (avoids needless UFW churn).
	existing := []ExistingRule{
		{RuleNumber: 2, Source: "203.0.113.4/32"},
		{RuleNumber: 3, Source: "10.0.0.0/8"}, // not in desired — delete
	}
	del, add := DiffAllowlist(existing, []string{"203.0.113.4/32", "198.51.100.0/24"})
	if !reflect.DeepEqual(del, []int{3}) {
		t.Errorf("expected only rule 3 deleted, got %v", del)
	}
	if !reflect.DeepEqual(add, []string{"198.51.100.0/24"}) {
		t.Errorf("expected only 198.51.100.0/24 to be added, got %v", add)
	}
}

func TestDiffAllowlist_EmptyDesiredMeansBlanket(t *testing.T) {
	// An empty desired list means the caller wants the port publicly reachable
	// (back-compat with the pre-0.12.0 open-port contract).
	existing := []ExistingRule{
		{RuleNumber: 7, Source: "203.0.113.4/32"},
	}
	del, add := DiffAllowlist(existing, nil)
	if !reflect.DeepEqual(del, []int{7}) {
		t.Errorf("expected rule 7 deleted, got %v", del)
	}
	if !reflect.DeepEqual(add, []string{"0.0.0.0/0"}) {
		t.Errorf("expected 0.0.0.0/0 to be added, got %v", add)
	}
}

func TestDiffAllowlist_DuplicatesCollapsed(t *testing.T) {
	existing := []ExistingRule{}
	_, add := DiffAllowlist(existing, []string{"203.0.113.4/32", "203.0.113.4/32"})
	if !reflect.DeepEqual(add, []string{"203.0.113.4/32"}) {
		t.Errorf("expected dedupe, got %v", add)
	}
}

func TestFormatAddRule(t *testing.T) {
	cases := []struct {
		name     string
		port     int
		protocol string
		cidr     string
		want     []string
	}{
		{
			name:     "blanket collapses to short form",
			port:     15432,
			protocol: "tcp",
			cidr:     "0.0.0.0/0",
			want:     []string{"allow", "15432/tcp"},
		},
		{
			name:     "ipv6 blanket also short form",
			port:     15432,
			protocol: "tcp",
			cidr:     "::/0",
			want:     []string{"allow", "15432/tcp"},
		},
		{
			name:     "specific cidr uses from/to/port syntax",
			port:     15432,
			protocol: "tcp",
			cidr:     "203.0.113.4/32",
			want:     []string{"allow", "from", "203.0.113.4/32", "to", "any", "port", "15432", "proto", "tcp"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FormatAddRule(c.port, c.protocol, c.cidr)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
