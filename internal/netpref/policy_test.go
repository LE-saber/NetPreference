package netpref

import (
	"context"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRulePrecedenceAndDomainBoundary(t *testing.T) {
	c := testConfig()
	c.Rules = []Rule{
		{ID: "global", Enabled: true, Domain: "specific.example.com", Device: "*", Action: "nxdomain"},
		{ID: "scoped", Enabled: true, Domain: "*.example.com", Device: testMAC, Action: "rewrite", IPv4: []string{"192.0.2.9"}, TTL: 60},
	}
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	q, _ := ValidateQuery(MakeQuery("specific.example.com", 1))
	r := MatchRule(&c, &c.Devices[0], q)
	if r == nil || r.ID != "scoped" {
		t.Fatal("scope precedence lost")
	}
	q.Name = "badexample.com"
	if MatchRule(&c, &c.Devices[0], q) != nil {
		t.Fatal("suffix boundary bypass")
	}
	q.Name = "example.com"
	if MatchRule(&c, &c.Devices[0], q) == nil {
		t.Fatal("wildcard apex missing")
	}
}
func TestLocalPolicies(t *testing.T) {
	for _, typ := range []uint16{1, 28, 65} {
		q, _ := ValidateQuery(MakeQuery("override.example", typ))
		for _, action := range []string{"rewrite", "nxdomain", "nodata", "refused", "sinkhole", "ipv4_only", "ipv6_only"} {
			r := Rule{Action: action, IPv4: []string{"192.0.2.10"}, IPv6: []string{"2001:db8::10"}, TTL: 60}
			b, local := LocalRule(q, &r)
			expected := !(action == "ipv4_only" && typ != 28 || action == "ipv6_only" && typ != 1)
			if local != expected {
				t.Fatal(action, typ)
			}
			if !local {
				continue
			}
			if e := ValidateResponse(b, q); e != nil {
				t.Fatal(action, e)
			}
			if action == "nxdomain" && u16(b, 2)&15 != 3 {
				t.Fatal("wrong NXDOMAIN")
			}
			if action == "refused" && u16(b, 2)&15 != 5 {
				t.Fatal("wrong REFUSED")
			}
		}
	}
}
func TestIPv6PreferenceRealTiming(t *testing.T) {
	up := fixtureUpstream(t, normalAnswer)
	c := testConfig()
	c.Upstream = up
	c.Devices[0].Mode = "ipv6"
	c.Devices[0].DelayMS = 100
	c.Devices[0].WaitMS = 150
	e := NewEngine(c)
	start := time.Now()
	b := e.Handle(context.Background(), MakeQuery("timing.example", 1), testMAC)
	elapsed := time.Since(start)
	if u16(b, 6) != 1 {
		t.Fatal("valid A suppressed")
	}
	if elapsed < 80*time.Millisecond || elapsed > time.Second {
		t.Fatalf("unexpected IPv4 hold %s", elapsed)
	}
	t.Logf("positive AAAA: A response held %s; A answer retained", elapsed)
}
func TestAbsentAAAADoesNotBreakIPv4(t *testing.T) {
	up := fixtureUpstream(t, func(raw []byte, p string) []byte {
		q, _ := ValidateQuery(raw)
		if q.Type == 28 {
			return Reply(q, 0, nil, 30)
		}
		return normalAnswer(raw, p)
	})
	c := testConfig()
	c.Upstream = up
	c.Devices[0].Mode = "ipv6"
	c.Devices[0].WaitMS = 500
	c.Devices[0].DelayMS = 300
	e := NewEngine(c)
	start := time.Now()
	b := e.Handle(context.Background(), MakeQuery("ipv4-only.example", 1), testMAC)
	elapsed := time.Since(start)
	if u16(b, 6) != 1 || elapsed > 300*time.Millisecond {
		t.Fatalf("IPv4 unnecessarily blocked/delayed: %s", elapsed)
	}
	t.Logf("negative AAAA: usable A returned in %s", elapsed)
}
func TestIPv4PreferenceRealTiming(t *testing.T) {
	up := fixtureUpstream(t, normalAnswer)
	c := testConfig()
	c.Upstream = up
	c.Devices[0].Mode = "ipv4"
	c.Devices[0].DelayMS = 90
	e := NewEngine(c)
	start := time.Now()
	b := e.Handle(context.Background(), MakeQuery("prefer4.example", 28), testMAC)
	if u16(b, 6) != 1 || time.Since(start) < 70*time.Millisecond {
		t.Fatal("AAAA was not delayed for positive A evidence")
	}
}
func TestNoProbeWaitIsFinite(t *testing.T) {
	var probes atomic.Int64
	up := fixtureUpstream(t, func(raw []byte, p string) []byte {
		q, _ := ValidateQuery(raw)
		if q.Type == 28 {
			probes.Add(1)
		}
		return normalAnswer(raw, p)
	})
	c := testConfig()
	c.Upstream = up
	c.Devices[0].Mode = "ipv6"
	c.Devices[0].Probe = false
	c.Devices[0].WaitMS = 70
	e := NewEngine(c)
	start := time.Now()
	b := e.Handle(context.Background(), MakeQuery("no-probe.example", 1), testMAC)
	elapsed := time.Since(start)
	if u16(b, 6) != 1 || elapsed < 50*time.Millisecond || elapsed > 500*time.Millisecond || probes.Load() != 0 {
		t.Fatalf("finite no-probe wait failed: %s probes %d", elapsed, probes.Load())
	}
}
func TestProbeHonorsAAAAOverrides(t *testing.T) {
	var probes atomic.Int64
	up := fixtureUpstream(t, func(raw []byte, p string) []byte {
		q, _ := ValidateQuery(raw)
		if q.Type == 28 {
			probes.Add(1)
		}
		return normalAnswer(raw, p)
	})
	c := testConfig()
	c.Upstream = up
	c.Devices[0].Mode = "ipv6"
	c.Rules = []Rule{{ID: "no6", Enabled: true, Device: testMAC, Domain: "*", QType: "AAAA", Action: "nodata"}}
	e := NewEngine(c)
	b := e.Handle(context.Background(), MakeQuery("blocked6.example", 1), testMAC)
	if u16(b, 6) != 1 || probes.Load() != 0 {
		t.Fatal("active probe bypassed domain policy")
	}
}
func TestUpstreamFailureFallsBack(t *testing.T) {
	up := fixtureUpstream(t, normalAnswer)
	c := testConfig()
	c.Upstream = up
	c.TimeoutMS = 100
	c.Devices[0].Upstream = "tcp://127.0.0.1:1"
	e := NewEngine(c)
	b := e.Handle(context.Background(), MakeQuery("fallback.example", 1), testMAC)
	if u16(b, 6) != 1 || e.Fallbacks.Load() != 1 {
		t.Fatal("original DNS fallback failed")
	}
}
func TestUnselectedAndUnknownBehavior(t *testing.T) {
	up := fixtureUpstream(t, normalAnswer)
	c := testConfig()
	c.Upstream = up
	c.Rules = []Rule{{ID: "all", Enabled: true, Domain: "*", Action: "nxdomain"}}
	e := NewEngine(c)
	raw := MakeQuery("example.com", 1)
	b := e.Handle(context.Background(), raw, "")
	if u16(b, 2)&15 != 5 {
		t.Fatal("open resolver")
	}
	b = e.Handle(context.Background(), raw, otherMAC)
	if u16(b, 6) != 1 {
		t.Fatal("global rule affected unenrolled LAN device")
	}
	b = e.Handle(context.Background(), raw, testMAC)
	if u16(b, 2)&15 != 3 {
		t.Fatal("managed global rule missing")
	}
	c.Devices[0].Mode = "block"
	c.Rules[0].Action = "rewrite"
	c.Rules[0].IPv4 = []string{"192.0.2.1"}
	e.SetConfig(c)
	b = e.Handle(context.Background(), raw, testMAC)
	if u16(b, 2)&15 != 5 {
		t.Fatal("domain override escaped whole-device block")
	}
}
func TestConcurrentConfigAndQueries(t *testing.T) {
	c := testConfig()
	e := NewEngine(c)
	e.Exchange = func(_ context.Context, raw []byte, _ string) ([]byte, error) {
		q, _ := ValidateQuery(raw)
		return Reply(q, 0, []netip.Addr{netip.MustParseAddr("192.0.2.1")}, 60), nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 80; j++ {
				_ = e.Handle(context.Background(), MakeQuery("race.example", 1), testMAC)
			}
		}()
	}
	for j := 0; j < 100; j++ {
		next := c
		next.Enabled = j%2 == 0
		e.SetConfig(next)
	}
	wg.Wait()
}
