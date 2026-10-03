package netpref

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func wireEngine(t *testing.T, c Config) (*Engine, string) {
	t.Helper()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(c)
	s, err := ListenDNS("127.0.0.1:0", e, func(net.IP) string { return testMAC })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return e, s.Addr()
}
func wireQuery(t *testing.T, addr, proto, name string, typ uint16) (time.Duration, []byte) {
	t.Helper()
	raw := MakeQuery(name, typ)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	b, err := Exchange(ctx, raw, proto+"://"+addr)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	q, _ := ValidateQuery(raw)
	if err := ValidateResponse(b, q); err != nil {
		t.Fatal(err)
	}
	t.Logf("%s %s %s: %s", proto, name, qtypeName(typ), elapsed)
	return elapsed, b
}

// Measure the actual UDP/TCP client-facing replies, not merely resolved structs.
func TestProfileDomainWireTimingAndSwitch(t *testing.T) {
	for _, proto := range []string{"udp", "tcp"} {
		t.Run(proto, func(t *testing.T) {
			c := profileConfig()
			c.Upstream = fixtureUpstream(t, normalAnswer)
			c.Devices[0].Mode = "ipv4"
			c.Devices[0].WaitMS = 100
			c.Devices[0].DelayMS = 65
			c.Policies = []Policy{
				{ID: "media", Enabled: true, Profile: "work", DomainSet: "media", Mode: "ipv6", WaitMS: intp(100), DelayMS: intp(160)},
				{ID: "short", Enabled: true, Profile: "work", Domain: "short.test", Mode: "ipv6", WaitMS: intp(100), DelayMS: intp(45)},
				{ID: "passive", Enabled: true, Profile: "work", Domain: "passive.test", Mode: "ipv6", WaitMS: intp(90), DelayMS: intp(200), Probe: boolp(false)},
				{ID: "dual", Enabled: true, Profile: "travel", Domain: "*", Mode: "dual"},
			}
			// Keep IDs in the common UCI namespace distinct from the set named media.
			c.Policies[0].ID = "media_policy"
			e, addr := wireEngine(t, c)
			cases := []struct {
				name    string
				typ     uint16
				min     time.Duration
				delayed bool
			}{
				{"video.test", TypeA, 140 * time.Millisecond, true},
				{"sub.media.test", TypeA, 140 * time.Millisecond, true},
				{"short.test", TypeA, 35 * time.Millisecond, true},
				{"notmedia.test", TypeAAAA, 55 * time.Millisecond, true},
				{"video.test", TypeAAAA, 0, false},
				{"notmedia.test", TypeA, 0, false},
				{"passive.test", TypeA, 75 * time.Millisecond, false},
			}
			for _, tc := range cases {
				before := e.Delayed.Load()
				elapsed, b := wireQuery(t, addr, proto, tc.name, tc.typ)
				q, _ := ValidateQuery(MakeQuery(tc.name, tc.typ))
				positive, _ := AddressEvidence(b, q)
				if !positive || elapsed < tc.min || elapsed > time.Second {
					t.Fatalf("unexpected response/timing: %s positive=%v", elapsed, positive)
				}
				if (e.Delayed.Load() > before) != tc.delayed {
					t.Fatal("wrong delay policy", tc.name)
				}
			}
			// A config switch must not erase the device default or leave the old profile active.
			c.Devices[0].Profile = "travel"
			e.SetConfig(c)
			before := e.Delayed.Load()
			wireQuery(t, addr, proto, "video.test", TypeA)
			if e.Delayed.Load() != before {
				t.Fatal("old profile applied after switch")
			}
			c.Devices[0].Profile = ""
			e.SetConfig(c)
			elapsed, _ := wireQuery(t, addr, proto, "video.test", TypeAAAA)
			if elapsed < 55*time.Millisecond {
				t.Fatal("device default was lost")
			}
		})
	}
}

func TestProfileProbeUsesPreferredFamilyRouteAndBlock(t *testing.T) {
	var wrongAAAA, correctAAAA atomic.Int64
	aOnly := fixtureUpstream(t, func(raw []byte, p string) []byte {
		q, _ := ValidateQuery(raw)
		if q.Type == TypeAAAA {
			wrongAAAA.Add(1)
			return Reply(q, 0, nil, 0)
		}
		return normalAnswer(raw, p)
	})
	preferred := fixtureUpstream(t, func(raw []byte, p string) []byte {
		q, _ := ValidateQuery(raw)
		if q.Type == TypeAAAA {
			correctAAAA.Add(1)
		}
		return normalAnswer(raw, p)
	})
	c := profileConfig()
	c.Upstream = preferred
	c.Policies = []Policy{{ID: "bias", Enabled: true, Profile: "work", Domain: "*", Mode: "ipv6", WaitMS: intp(150), DelayMS: intp(100)}}
	c.Rules = []Rule{{ID: "a_route", Enabled: true, Domain: "*", QType: "A", Action: "forward", Upstream: aOnly}}
	e, addr := wireEngine(t, c)
	elapsed, _ := wireQuery(t, addr, "udp", "route.test", TypeA)
	if elapsed < 85*time.Millisecond || wrongAAAA.Load() != 0 || correctAAAA.Load() != 1 {
		t.Fatal("probe used nonpreferred-family resolver", elapsed, wrongAAAA.Load(), correctAAAA.Load())
	}
	c.Rules = append(c.Rules, Rule{ID: "noaaaa", Enabled: true, Domain: "blocked.test", QType: "AAAA", Action: "nodata"})
	e.SetConfig(c)
	before := e.Delayed.Load()
	_, b := wireQuery(t, addr, "tcp", "blocked.test", TypeA)
	q, _ := ValidateQuery(MakeQuery("blocked.test", TypeA))
	positive, _ := AddressEvidence(b, q)
	if !positive || e.Delayed.Load() != before || correctAAAA.Load() != 1 {
		t.Fatal("AAAA local block bypassed or valid A removed")
	}
}

func TestOldInflightProbeCannotPolluteNewProfile(t *testing.T) {
	var once sync.Once
	arrived := make(chan struct{})
	release := make(chan struct{})
	c := profileConfig()
	c.Upstream = fixtureUpstream(t, func(raw []byte, p string) []byte {
		q, _ := ValidateQuery(raw)
		if q.Type == TypeAAAA {
			once.Do(func() { close(arrived) })
			<-release
		}
		return normalAnswer(raw, p)
	})
	// Always unblock before fixture cleanup, including assertion failures.
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	c.Policies = []Policy{
		{ID: "old", Enabled: true, Profile: "work", Domain: "*", Mode: "ipv6", WaitMS: intp(100), DelayMS: intp(80)},
		{ID: "new", Enabled: true, Profile: "travel", Domain: "*", Mode: "ipv6", WaitMS: intp(80), DelayMS: intp(200), Probe: boolp(false)},
	}
	e := NewEngine(c)
	oldDone := make(chan struct{})
	go func() { defer close(oldDone); e.Handle(context.Background(), MakeQuery("race.test", TypeA), testMAC) }()
	select {
	case <-arrived:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	c.Devices[0].Profile = "travel"
	e.SetConfig(c)
	unblock.Do(func() { close(release) })
	<-oldDone
	before := e.Delayed.Load()
	start := time.Now()
	b := e.Handle(context.Background(), MakeQuery("race.test", TypeA), testMAC)
	if len(b) == 0 || e.Delayed.Load() != before || time.Since(start) < 65*time.Millisecond {
		t.Fatal("old profile's evidence crossed generation")
	}
}

func TestWireLabelsCannotAliasDottedOrUnicodeNames(t *testing.T) {
	c := profileConfig()
	var probeOK atomic.Bool
	c.Upstream = fixtureUpstream(t, func(raw []byte, p string) []byte {
		q, _ := ValidateQuery(raw)
		if q.Type == TypeAAAA && len(q.Labels) == 2 && q.Labels[0] == "api.media" {
			probeOK.Store(true)
		}
		return normalAnswer(raw, p)
	})
	c.Policies = []Policy{{ID: "v6", Enabled: true, Profile: "work", Domain: "*", Mode: "ipv6", WaitMS: intp(150), DelayMS: intp(20)}}
	c.Rules = []Rule{{ID: "exact", Enabled: true, Domain: "api.media.test", Action: "nxdomain"}, {ID: "unicode", Enabled: true, Domain: "k.test", Action: "nxdomain"}}
	e := NewEngine(c)
	for _, labels := range [][]string{{"api.media", "test"}, {"api ", "test"}, {"\u212a", "test"}} {
		q := Question{ID: 17, Type: TypeA, Class: 1, Labels: labels}
		raw := questionWire(q)
		put16(raw, 2, 0x0100)
		actual, err := ValidateQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		b := e.Handle(context.Background(), raw, testMAC)
		if u16(b, 2)&15 != 0 {
			t.Fatal("wire label aliased local block", actual.Name)
		}
		if err := ValidateResponse(b, actual); err != nil {
			t.Fatal(err)
		}
	}
	if !probeOK.Load() {
		t.Fatal("probe changed wire labels")
	}
	// A forged reply containing split labels must not validate as the same question.
	q, _ := ValidateQuery(questionWire(Question{ID: 17, Type: TypeA, Class: 1, Labels: []string{"api.media", "test"}}))
	forged := normalAnswer(MakeQuery("api.media.test", TypeA), "udp")
	put16(forged, 0, 17)
	if ValidateResponse(forged, q) == nil {
		t.Fatal("ambiguous labels passed response validation")
	}
}
