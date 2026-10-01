package netpref

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type command struct {
	name  string
	args  []string
	input string
}
type fakeRunner struct {
	mu         sync.Mutex
	calls      []command
	table      bool
	foreign    bool
	failCheck  bool
	failWrite  bool
	failCommit bool
	config     string
	neighbours string
	nlbw       string
	counters   string
}

func (f *fakeRunner) Run(_ context.Context, n string, a []string, b []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, command{n, append([]string{}, a...), string(b)})
	key := strings.Join(a, " ")
	switch n {
	case "uci":
		if key == "-q export netpreference" {
			return []byte(f.config), nil
		}
		if key == "commit netpreference" && f.failCommit {
			return nil, errors.New("read-only overlay")
		}
		if strings.HasPrefix(key, "set netpreference.main.enabled=") || key == "commit netpreference" {
			return nil, nil
		}
	case "ip":
		return []byte(f.neighbours), nil
	case "conntrack":
		return []byte("0 flow entries have been deleted."), errors.New("exit 1")
	case "nlbw":
		return []byte(f.nlbw), nil
	case "nft":
		if key == "-j list tables" {
			if f.table {
				return []byte(`{"nftables":[{"table":{"family":"inet","name":"netpreference"}}]}`), nil
			}
			return []byte(`{"nftables":[]}`), nil
		}
		if key == "-j list table inet netpreference" {
			if f.counters != "" {
				return []byte(f.counters), nil
			}
			marker := OwnerComment
			if f.foreign {
				marker = "user owned"
			}
			return []byte(fmt.Sprintf(`{"nftables":[{"table":{"family":"inet","name":"netpreference","comment":%q}}]}`, marker)), nil
		}
		if key == "-c -f -" {
			if f.failCheck {
				return nil, errors.New("nft syntax failure")
			}
			return nil, nil
		}
		if key == "-f -" {
			if f.failWrite {
				return nil, errors.New("nft atomic transaction failed")
			}
			if strings.Contains(string(b), "table inet netpreference {") {
				f.table = true
			}
			return nil, nil
		}
		if key == "delete table inet netpreference" {
			f.table = false
			return nil, nil
		}
	}
	return nil, fmt.Errorf("unexpected external command: %s %s", n, key)
}
func testManager(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	f := &fakeRunner{neighbours: `[{"dst":"192.0.2.2","dev":"br-lan","lladdr":"02:00:00:00:00:01"}]`, config: `config global 'main'
 option enabled '0'
 list interface 'br-lan'
config device 'laptop'
 option enabled '1'
 option mac '02:00:00:00:00:01'
 option mode 'ipv6'
`}
	m := NewManager(f, NewEngine(DefaultConfig()))
	m.StateDir = t.TempDir()
	m.Health = func(context.Context, *Config) error { return nil }
	m.CheckInterfaces = func(*Config) error { return nil }
	m.Baseline = func() error {
		return AtomicJSON(filepath.Join(m.StateDir, "baseline.json"), map[string]string{"forensic": "unchanged"}, 0600)
	}
	return m, f
}
func TestApplyRestoreOwnedStateOnly(t *testing.T) {
	m, f := testManager(t)
	ctx := context.Background()
	if e := m.Apply(ctx, true); e != nil {
		t.Fatal(e)
	}
	if !m.Status().Active || !f.table {
		t.Fatal("not activated")
	}
	baseline, _ := os.ReadFile(filepath.Join(m.StateDir, "baseline.json"))
	unrelated := filepath.Join(m.StateDir, "user-new-dns.conf")
	os.WriteFile(unrelated, []byte("user changed later"), 0600)
	if e := m.Restore(ctx, true); e != nil {
		t.Fatal(e)
	}
	if f.table || m.Status().Active || m.Engine.Config().Enabled {
		t.Fatal("not restored")
	}
	if e := m.Restore(ctx, true); e != nil {
		t.Fatal("restore not idempotent", e)
	}
	after, _ := os.ReadFile(filepath.Join(m.StateDir, "baseline.json"))
	if string(after) != string(baseline) {
		t.Fatal("baseline overwritten")
	}
	data, _ := os.ReadFile(unrelated)
	if string(data) != "user changed later" {
		t.Fatal("user update damaged")
	}
	for _, c := range f.calls {
		if c.name == "uci" && c.args[0] != "-q" && !strings.Contains(strings.Join(c.args, " "), "netpreference") {
			t.Fatal("shared config write", c)
		}
		if c.name == "conntrack" && (!strings.Contains(strings.Join(c.args, " "), "--reply-port-src 1053") || !strings.Contains(strings.Join(c.args, " "), "--reply-src ")) {
			t.Fatal("unscoped conntrack deletion")
		}
	}
}
func TestApplyFailsBeforeWritesOnHealthOwnershipAndNFT(t *testing.T) {
	for _, stage := range []string{"health", "foreign", "syntax", "transaction", "baseline"} {
		t.Run(stage, func(t *testing.T) {
			m, f := testManager(t)
			switch stage {
			case "health":
				m.Health = func(context.Context, *Config) error { return errors.New("DNS down") }
			case "foreign":
				f.table = true
				f.foreign = true
			case "syntax":
				f.failCheck = true
			case "transaction":
				f.failWrite = true
			case "baseline":
				m.Baseline = func() error { return errors.New("disk full") }
			}
			if e := m.Apply(context.Background(), true); e == nil {
				t.Fatal("expected failure")
			}
			if m.Status().Active {
				t.Fatal("falsely active")
			}
			for _, c := range f.calls {
				if c.name == "uci" && c.args[0] == "set" {
					t.Fatal("persisted failed apply")
				}
			}
			if stage == "foreign" && !f.table {
				t.Fatal("foreign table deleted")
			}
		})
	}
}
func TestPersistenceFailureRollsBackInterception(t *testing.T) {
	m, f := testManager(t)
	f.failCommit = true
	if e := m.Apply(context.Background(), true); e == nil {
		t.Fatal("expected persistence failure")
	}
	if f.table || m.Status().Active {
		t.Fatal("failed activation left routing")
	}
	if _, e := os.Stat(filepath.Join(m.StateDir, "restored")); e != nil {
		t.Fatal("missing boot inhibitor", e)
	}
}
func TestRestoreStillRemovesRoutingWhenUCIFails(t *testing.T) {
	m, f := testManager(t)
	if e := m.Apply(context.Background(), true); e != nil {
		t.Fatal(e)
	}
	f.failCommit = true
	if e := m.Restore(context.Background(), true); e == nil {
		t.Fatal("expected error")
	}
	if f.table || m.Engine.Config().Enabled {
		t.Fatal("restore did not fail open")
	}
}
func TestTickHealthInterfaceAndRecovery(t *testing.T) {
	m, f := testManager(t)
	ctx := context.Background()
	if e := m.Apply(ctx, true); e != nil {
		t.Fatal(e)
	}
	m.Health = func(context.Context, *Config) error { return errors.New("upstream unavailable") }
	m.Tick(ctx)
	if m.Status().Active || m.Engine.Config().Enabled {
		t.Fatal("health failure policy still active")
	}
	found := false
	for _, c := range f.calls {
		if strings.HasPrefix(c.input, "flush set inet netpreference np_devices\n") && !strings.Contains(c.input, "add element inet netpreference np_devices") {
			found = true
		}
	}
	if !found {
		t.Fatal("lease not cleared")
	}
	m.Health = func(context.Context, *Config) error { return nil }
	m.Tick(ctx)
	if !m.Status().Active {
		t.Fatal("no automatic recovery")
	}
	m.CheckInterfaces = func(*Config) error { return errors.New("LAN disappeared") }
	m.Tick(ctx)
	if f.table || m.Status().Active || m.Engine.Config().Enabled {
		t.Fatal("interface failure not safe")
	}
}
func TestNFTIdentityAndMonitorSwitch(t *testing.T) {
	c := DefaultConfig()
	c.Enabled = true
	c.Devices = []Device{{MAC: "02:00:00:00:00:01", Enabled: true}}
	hosts := []Host{{MAC: c.Devices[0].MAC, IPv4: []string{"192.0.2.2"}, IPv6: []string{"2001:db8::2"}}}
	s := BuildNFT(&c, hosts, false)
	for _, want := range []string{"flags timeout; timeout 15s", "ether saddr . ip saddr @clients4", "ether saddr . ip6 saddr @clients6", "02:00:00:00:00:01 . 2001:db8::2", "priority -105"} {
		if !strings.Contains(s, want) {
			t.Fatal("missing", want)
		}
	}
	for _, bad := range []string{"flush ruleset", "fw4", "masquerade", "hook output", "chain observe"} {
		if strings.Contains(s, bad) {
			t.Fatal("unexpected", bad)
		}
	}
	c.Monitor = true
	s = BuildNFT(&c, hosts, true)
	if !strings.Contains(s, "chain observe") || !strings.Contains(s, "counter m020000000001_6_down") {
		t.Fatal("monitor counters missing")
	}
	s = RefreshNFT(&c, hosts, false)
	if strings.Contains(s, "add element inet netpreference np_devices") {
		t.Fatal("unhealthy activation")
	}
}
func TestForeignRemoveAndAbsentNoConntrack(t *testing.T) {
	f := &fakeRunner{table: true, foreign: true}
	fw := Firewall{f}
	if e := fw.Remove(context.Background()); e == nil {
		t.Fatal("foreign table accepted")
	}
	if !f.table {
		t.Fatal("foreign table removed")
	}
	f.table = false
	f.calls = nil
	if e := fw.Remove(context.Background()); e != nil {
		t.Fatal(e)
	}
	for _, c := range f.calls {
		if c.name == "conntrack" {
			t.Fatal("absent ownership cleaned flows")
		}
	}
}
func TestInventoryTrustBoundary(t *testing.T) {
	f := &fakeRunner{neighbours: `[{"dst":"192.0.2.2","dev":"br-lan","lladdr":"02:00:00:00:00:01"},{"dst":"2001:db8::2","dev":"br-lan","lladdr":"02:00:00:00:00:01"},{"dst":"198.51.100.9","dev":"wan","lladdr":"02:00:00:00:00:02"}]`}
	inv := NewInventory(f)
	inv.leasePath = filepath.Join(t.TempDir(), "missing")
	c := DefaultConfig()
	if e := inv.Refresh(context.Background(), &c); e != nil {
		t.Fatal(e)
	}
	hosts := inv.Hosts()
	if len(hosts) != 1 || len(hosts[0].IPv6) != 1 {
		t.Fatal(hosts)
	}
}
func TestControlSocketAndStrictAPI(t *testing.T) {
	m, _ := testManager(t)
	path := filepath.Join(t.TempDir(), "control.sock")
	s, e := ListenControl(path, m)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal("insecure socket")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b, e := Control(ctx, path, "status")
	if e != nil || !strings.Contains(string(b), "version") {
		t.Fatal(string(b), e)
	}
	if _, e := Control(ctx, path, "shell"); e == nil {
		t.Fatal("unexpected control method accepted")
	}
	if _, e := ListenControl(path, m); e == nil {
		t.Fatal("active socket replaced")
	}
}
func TestBaselinePreservedAndPrivate(t *testing.T) {
	dir := t.TempDir()
	if e := SaveBaseline(dir, &fakeRunner{}); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "baseline.json")
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0600 {
		t.Fatal("baseline permissions")
	}
	b, _ := os.ReadFile(path)
	if e := SaveBaseline(dir, &fakeRunner{}); e != nil {
		t.Fatal(e)
	}
	b2, _ := os.ReadFile(path)
	if string(b) != string(b2) {
		t.Fatal("baseline not immutable")
	}
}
