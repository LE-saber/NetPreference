package netpref

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Same named objects emitted by nft -j list table, including the ownership
// marker used by the runtime. This exercises more than the view's JSON labels.
func trafficObjects(v int) string {
	return fmt.Sprintf(`{"nftables":[{"table":{"family":"inet","name":"netpreference","comment":%q}},{"counter":{"family":"inet","table":"netpreference","name":"m020000000001_4_up","bytes":%d}},{"counter":{"name":"m020000000001_4_down","bytes":%d}},{"counter":{"name":"m020000000001_6_up","bytes":%d}},{"counter":{"name":"m020000000001_6_down","bytes":%d}}]}`, OwnerComment, v, v*2, v*3, v*4)
}
func monitoredManager(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	m, f := testManager(t)
	f.config = strings.Replace(f.config, "option enabled '0'", "option enabled '1'\n option monitor '1'", 1)
	f.counters = trafficObjects(100)
	if err := m.Apply(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	return m, f
}
func TestTrafficRuntimeImmediateEnabled(t *testing.T) {
	m, _ := monitoredManager(t)
	if !m.Traffic.Snapshot().Enabled {
		t.Fatal("monitor configured on but initial runtime snapshot reports off")
	}
}
func TestTrafficMissingObjectsAreNotAvailable(t *testing.T) {
	tr := NewTraffic(&fakeRunner{counters: `{"nftables":[]}`})
	tr.Sample(context.Background(), []Host{{MAC: "02:00:00:00:00:01"}})
	if s := tr.Snapshot(); s.TotalsAvailable || s.Error == "" {
		t.Fatalf("missing named counters silently marked available: %+v", s)
	}
}
func TestTrafficProfileReloadPreservesCountersAndSession(t *testing.T) {
	m, f := monitoredManager(t)
	ctx := context.Background()
	m.Traffic.Sample(ctx, m.hosts)
	time.Sleep(time.Millisecond)
	f.counters = trafficObjects(200)
	m.Tick(ctx)
	before := m.Traffic.Snapshot()
	if len(before.Rows) != 1 || before.Rows[0].Totals.V6.Down != 400 {
		t.Fatal(before)
	}
	f.config += "\nconfig profile 'daily'\n option name 'Daily'\n"
	start := len(f.calls)
	if err := m.Apply(ctx, false); err != nil {
		t.Fatal(err)
	}
	for _, call := range f.calls[start:] {
		if call.name == "nft" && strings.Contains(call.input, "delete table") {
			t.Fatal("profile-only change rebuilt all traffic counters")
		}
	}
	after := m.Traffic.Snapshot()
	if len(after.Rows) != 1 || after.Rows[0].Totals != before.Rows[0].Totals {
		t.Fatal("profile Apply erased accumulated traffic", after)
	}
	// Verify the same live object survives the control socket boundary.
	srv, err := ListenControl(t.TempDir()+"/control.sock", m)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	raw, err := Control(ctx, srv.path, "traffic")
	if err != nil {
		t.Fatal(err)
	}
	var rpc TrafficSnapshot
	if err = json.Unmarshal(raw, &rpc); err != nil {
		t.Fatal(err)
	}
	if rpc.Rows[0].Totals != after.Rows[0].Totals || rpc.Rows[0].IPv6Ratio != 0.7 {
		t.Fatal("runtime/control contract lost family totals", string(raw))
	}
}
func TestTrafficContinuesDuringDNSFailOpen(t *testing.T) {
	m, f := monitoredManager(t)
	ctx := context.Background()
	m.Traffic.Sample(ctx, m.hosts)
	time.Sleep(time.Millisecond)
	m.Health = func(context.Context, *Config) error { return errors.New("upstream DNS unavailable") }
	f.counters = trafficObjects(300)
	m.Tick(ctx)
	s := m.Traffic.Snapshot()
	if m.Status().Active || m.Engine.Config().Enabled {
		t.Fatal("DNS failure did not fail open")
	}
	if len(s.Rows) != 1 || s.Rows[0].Totals.V4.Up != 200 || s.Rows[0].Totals.V6.Down != 800 {
		t.Fatal("DNS health failure stopped independent traffic observation", s)
	}
}

func TestObservationIncludesRouterLocalProxyPath(t *testing.T) {
	c := DefaultConfig()
	c.Monitor = true
	rules := BuildNFT(&c, []Host{{MAC: "02:00:00:00:00:01", IPv4: []string{"192.0.2.2"}, IPv6: []string{"fd42::2"}}}, false)
	for _, hook := range []string{"chain observe_upload { type filter hook prerouting priority -310", "chain observe_download { type filter hook postrouting priority 310"} {
		if !strings.Contains(rules, hook) {
			t.Fatal("traffic must include INPUT/OUTPUT proxy paths", rules)
		}
	}
	if strings.Contains(rules, "hook forward") {
		t.Fatal("double counting with an additional forward counter")
	}
	c.Monitor = false
	rules = BuildNFT(&c, nil, false)
	if strings.Contains(rules, "chain observe") {
		t.Fatal("monitor off installed observation rules")
	}
}

func TestTrafficIndependentFamiliesMissingResetAndGap(t *testing.T) {
	f := &fakeRunner{counters: trafficObjects(100)}
	tr := NewTraffic(f)
	ctx := context.Background()
	hosts := []Host{{MAC: "02:00:00:00:00:01"}}
	now := time.Unix(100, 0)
	tr.sampleAt(ctx, hosts, now)
	f.counters = trafficObjects(200)
	tr.sampleAt(ctx, hosts, now.Add(2*time.Second))
	s := tr.Snapshot()
	if s.Rows[0].V4.Up != 50 || s.Rows[0].V6.Down != 200 || s.Rows[0].IPv6Ratio != 0.7 {
		t.Fatal(s)
	}
	tr.Rebase()
	f.counters = trafficObjects(1000)
	tr.sampleAt(ctx, hosts, now.Add(3*time.Second))
	if s = tr.Snapshot(); s.Rows[0].V4.Valid || s.Rows[0].Totals.V6.Down != 400 {
		t.Fatal("cross-generation spike/reset", s)
	}
	f.counters = trafficObjects(1100)
	tr.sampleAt(ctx, hosts, now.Add(5*time.Second))
	if s = tr.Snapshot(); s.Rows[0].Totals.V4.Up != 200 || s.Rows[0].Totals.V6.Down != 800 {
		t.Fatal(s)
	}
	f.counters = `{"nftables":[{"counter":{"name":"m020000000001_4_up","bytes":1200}},{"counter":{"name":"m020000000001_4_down","bytes":2400}}]}`
	tr.sampleAt(ctx, hosts, now.Add(7*time.Second))
	s = tr.Snapshot()
	if s.State != "partial" || s.TotalsAvailable || !s.Rows[0].V4Available || s.Rows[0].V6Available || !s.Rows[0].V4.Valid || s.Rows[0].V6.Valid || len(s.MissingCounters) != 2 {
		t.Fatal(s)
	}
	f.counters = `broken`
	tr.sampleAt(ctx, hosts, now.Add(9*time.Second))
	s = tr.Snapshot()
	if s.State != "unavailable" || !s.Enabled || s.Rows[0].V4.Valid || s.Error == "" {
		t.Fatal(s)
	}
	f.counters = trafficObjects(2000)
	tr.sampleAt(ctx, hosts, now.Add(11*time.Second))
	s = tr.Snapshot()
	if s.Rows[0].V4.Valid || s.Rows[0].Totals.V4.Up != 300 || s.Rows[0].Totals.V6.Down != 800 {
		t.Fatal("gap estimated rather than rebased", s)
	}
	for _, data := range []string{
		`{"nftables":[{"counter":{"name":"x","bytes":-1}}]}`,
		`{"nftables":[{"counter":{"name":"x","bytes":1}},{"counter":{"name":"x","bytes":2}}]}`,
	} {
		if _, err := ParseCounters([]byte(data)); err == nil {
			t.Fatal("invalid counters accepted", data)
		}
	}
}
