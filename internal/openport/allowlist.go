package openport

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ExistingRule describes a single UFW ALLOW rule for a specific port as
// reported by `ufw status numbered`. The RuleNumber is what `ufw delete N`
// expects. Source is normalized: "Anywhere" / "Anywhere (v6)" → "0.0.0.0/0"
// / "::/0".
type ExistingRule struct {
	RuleNumber int
	Port       int
	Protocol   string
	Source     string // CIDR (e.g. "203.0.113.4/32"). "0.0.0.0/0" means blanket.
	IPv6       bool   // True if UFW printed "(v6)" on the destination.
}

// ufwNumberedLine example lines we want to parse:
//
//	[ 3] 15432/tcp                  ALLOW IN    Anywhere
//	[ 4] 15432/tcp                  ALLOW IN    203.0.113.4
//	[ 5] 15432/tcp                  ALLOW IN    203.0.113.0/24
//	[ 6] 15432/tcp (v6)             ALLOW IN    Anywhere (v6)
//
// We intentionally match with flexible whitespace; `ufw` pads columns.
var ufwLineRE = regexp.MustCompile(
	`^\s*\[\s*(\d+)\s*\]\s+(\d+)/(tcp|udp)(\s+\(v6\))?\s+ALLOW\s+IN\s+(.+?)\s*$`,
)

// ParseAllUFWAllowRules scans `ufw status numbered` output and returns every
// ALLOW-IN rule, regardless of port or protocol. Used by reset-vm to rebuild
// the UFW baseline without per-port knowledge.
func ParseAllUFWAllowRules(ufwStatus string) []ExistingRule {
	var out []ExistingRule
	scanner := bufio.NewScanner(strings.NewReader(ufwStatus))
	for scanner.Scan() {
		line := scanner.Text()
		m := ufwLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rulePort, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		ipv6 := strings.TrimSpace(m[4]) == "(v6)"
		source := normalizeSource(strings.TrimSpace(m[5]), ipv6)
		ruleNum, _ := strconv.Atoi(m[1])
		out = append(out, ExistingRule{
			RuleNumber: ruleNum,
			Port:       rulePort,
			Protocol:   m[3],
			Source:     source,
			IPv6:       ipv6,
		})
	}
	return out
}

// ParseUFWRulesForPort scans `ufw status numbered` output and returns every
// ALLOW-IN rule that targets the given port+protocol.
func ParseUFWRulesForPort(ufwStatus string, port int, protocol string) []ExistingRule {
	var out []ExistingRule
	scanner := bufio.NewScanner(strings.NewReader(ufwStatus))
	for scanner.Scan() {
		line := scanner.Text()
		m := ufwLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rulePort, err := strconv.Atoi(m[2])
		if err != nil || rulePort != port {
			continue
		}
		if m[3] != protocol {
			continue
		}
		ipv6 := strings.TrimSpace(m[4]) == "(v6)"
		source := normalizeSource(strings.TrimSpace(m[5]), ipv6)
		ruleNum, _ := strconv.Atoi(m[1])
		out = append(out, ExistingRule{
			RuleNumber: ruleNum,
			Port:       rulePort,
			Protocol:   m[3],
			Source:     source,
			IPv6:       ipv6,
		})
	}
	return out
}

// normalizeSource canonicalizes UFW's free-form "From" column into a CIDR we
// can compare against a desired allowlist.
func normalizeSource(raw string, ipv6 bool) string {
	raw = strings.TrimSpace(raw)
	// UFW prints "Anywhere" for 0.0.0.0/0 and "Anywhere (v6)" for ::/0
	if raw == "Anywhere" || raw == "Anywhere (v6)" {
		if ipv6 {
			return "::/0"
		}
		return "0.0.0.0/0"
	}
	// Strip a trailing "(v6)" annotation if UFW printed it on the source column.
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "(v6)"))
	// Bare IPs ("203.0.113.4") → /32 or /128 so comparisons line up with
	// what `ip_network(strict=False)` produces on the Python side.
	if !strings.Contains(raw, "/") {
		if strings.Contains(raw, ":") {
			return raw + "/128"
		}
		return raw + "/32"
	}
	return raw
}

// DiffAllowlist computes the reconcile plan: which existing rule numbers to
// delete and which CIDRs to add, so that the UFW rules for this port end up
// as exactly `desired`.
//
// Semantics:
//   - `desired` with one entry "0.0.0.0/0" (or empty) means "publicly reachable"
//     and the caller should end up with a single blanket rule.
//   - Duplicate desired entries are collapsed.
//   - Existing rules whose Source already matches a desired CIDR are kept
//     (not deleted and not re-added).
//
// Returned delete numbers are sorted DESCENDING so the caller can `ufw delete`
// in order without renumbering shifting the remaining ones.
func DiffAllowlist(existing []ExistingRule, desired []string) (toDelete []int, toAdd []string) {
	desiredSet := map[string]struct{}{}
	for _, c := range desired {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		desiredSet[c] = struct{}{}
	}
	if len(desiredSet) == 0 {
		desiredSet["0.0.0.0/0"] = struct{}{}
	}

	keep := map[string]bool{}
	for _, r := range existing {
		if _, ok := desiredSet[r.Source]; ok && !keep[r.Source] {
			keep[r.Source] = true
			continue
		}
		toDelete = append(toDelete, r.RuleNumber)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(toDelete)))

	for c := range desiredSet {
		if !keep[c] {
			toAdd = append(toAdd, c)
		}
	}
	sort.Strings(toAdd)
	return toDelete, toAdd
}

// FormatAddRule returns the UFW command args to add a rule for `cidr` on
// `port/protocol`. Blanket allow (0.0.0.0/0) uses the short form because
// that's what UFW itself prints back and compares cleanly across IPv4/IPv6.
func FormatAddRule(port int, protocol, cidr string) []string {
	switch cidr {
	case "0.0.0.0/0", "::/0", "":
		return []string{"allow", fmt.Sprintf("%d/%s", port, protocol)}
	}
	return []string{
		"allow", "from", cidr, "to", "any", "port",
		strconv.Itoa(port), "proto", protocol,
	}
}
