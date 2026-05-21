package handlers

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMetricsCollectInvalidParams(t *testing.T) {
	_, err := MetricsCollect(context.Background(), json.RawMessage(`{bad`))
	if err == nil {
		t.Error("expected error for invalid JSON params")
	}
}

func TestMetricsCollectEmptyParams(t *testing.T) {
	// metrics.collect takes no payload — empty and {} must both be accepted.
	for _, p := range []string{"", "{}", "  "} {
		if _, err := MetricsCollect(context.Background(), json.RawMessage(p)); err != nil {
			t.Errorf("expected no error for params %q, got %v", p, err)
		}
	}
}

func TestParseDisk(t *testing.T) {
	out := "Filesystem      Size  Used Avail Use% Mounted on\n" +
		"/dev/sda1        38G  4.2G   32G  12% /\n"
	d := parseDisk(out)
	if d.Error != "" {
		t.Fatalf("unexpected error: %s", d.Error)
	}
	if d.Total != "38G" || d.Used != "4.2G" || d.Pct != 12 {
		t.Errorf("got %+v", d)
	}
}

func TestParseDiskMalformed(t *testing.T) {
	cases := map[string]string{
		"header only":   "Filesystem Size Used Avail Use% Mounted on\n",
		"too few fields": "Filesystem Size Used Avail Use% Mounted on\n/dev/sda1 38G\n",
		"bad percent":    "Filesystem Size Used Avail Use% Mounted on\n/dev/sda1 38G 4.2G 32G NaN% /\n",
	}
	for name, out := range cases {
		d := parseDisk(out)
		if d.Error == "" {
			t.Errorf("%s: expected error field, got %+v", name, d)
		}
	}
}

func TestParseMemory(t *testing.T) {
	meminfo := "MemTotal:        1946000 kB\n" +
		"MemFree:          200000 kB\n" +
		"MemAvailable:    1421000 kB\n" +
		"Buffers:           50000 kB\n"
	m := parseMemory(meminfo)
	if m.Error != "" {
		t.Fatalf("unexpected error: %s", m.Error)
	}
	// used = (1946000 - 1421000) kB = 525000 kB -> 512 MB; total -> 1900 MB
	if m.TotalMB != 1900 || m.UsedMB != 512 || m.Pct != 26 {
		t.Errorf("got %+v", m)
	}
}

func TestParseMemoryMalformed(t *testing.T) {
	m := parseMemory("Garbage:  nothing useful\n")
	if m.Error == "" {
		t.Errorf("expected error field, got %+v", m)
	}
}

func TestParseLoad(t *testing.T) {
	load, errStr := parseLoad("0.12 0.08 0.05 1/234 5678\n")
	if errStr != "" {
		t.Fatalf("unexpected error: %s", errStr)
	}
	if len(load) != 3 || load[0] != 0.12 || load[1] != 0.08 || load[2] != 0.05 {
		t.Errorf("got %v", load)
	}
}

func TestParseLoadMalformed(t *testing.T) {
	if _, errStr := parseLoad("0.12 0.08\n"); errStr == "" {
		t.Error("expected error for too few load fields")
	}
	if _, errStr := parseLoad("x y z\n"); errStr == "" {
		t.Error("expected error for non-numeric load values")
	}
}
