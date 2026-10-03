package netpref

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func intp(v int) *int    { return &v }
func boolp(v bool) *bool { return &v }
func profileConfig() Config {
	c := testConfig()
	c.Profiles = []Profile{{ID: "work", Name: "Work"}, {ID: "travel", Name: "Travel"}}
	c.Devices[0].Profile = "work"
	c.DomainSets = []DomainSet{{ID: "media", Name: "Media", Domains: []string{"*.media.test", "video.test"}}}
	return c
}
func readFixture(t *testing.T, name string) Config {
	t.Helper()
	raw, err := os.ReadFile("../../tests/fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseUCI(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestLegacyR6NoMigrationOrValueLoss(t *testing.T) {
	c := readFixture(t, "legacy-r6.uci")
	if !c.Enabled || !c.Monitor || len(c.Rules) != 3 || len(c.Devices) != 2 {
		t.Fatal(c)
	}
	d := c.Devices[0]
	if d.Mode != "custom" || d.Prefer != "ipv4" || d.WaitMS != 0 || d.DelayMS != 0 || d.Probe || d.Upstream != "tcp://127.0.0.1:5333" {
		t.Fatal(d)
	}
	if d.Profile != "" || len(c.Profiles)+len(c.Policies)+len(c.DomainSets) != 0 {
		t.Fatal("legacy config gained advanced policies")
	}
	p := ResolvePreference(&c, &d, "anything.test")
	if p.Policy != "" || p.Mode != d.Mode || p.WaitMS != 0 || p.DelayMS != 0 || p.Probe {
		t.Fatal(p)
	}
	q := Question{Name: "sub.example.com", Type: TypeA}
	if r := MatchRule(&c, &d, q); r == nil || r.ID != "legacy_static" {
		t.Fatal(r)
	}
	before, _ := json.Marshal(c)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(c)
	if string(before) != string(after) {
		t.Fatal("validation not idempotent")
	}
}
func TestParseProfilePresenceAndNormalization(t *testing.T) {
	c := readFixture(t, "profiles.uci")
	if c.DomainSets[0].Domains[0] != "*.media.test" {
		t.Fatal(c.DomainSets)
	}
	if c.Policies[0].Probe != nil || c.Policies[0].WaitMS == nil || *c.Policies[0].WaitMS != 250 {
		t.Fatal(c.Policies[0])
	}
	z := c.Policies[1]
	if z.Probe == nil || *z.Probe || z.WaitMS == nil || *z.WaitMS != 0 || z.DelayMS == nil || *z.DelayMS != 0 {
		t.Fatal(z)
	}
	inherit := c.Policies[2]
	if inherit.Probe != nil || inherit.WaitMS != nil || inherit.DelayMS != nil {
		t.Fatal("empty values must inherit", inherit)
	}
}
func TestAdvancedValidationRejectsUnsafeReferencesAndCombinations(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing-device-profile", func(c *Config) { c.Devices[0].Profile = "missing" }},
		{"missing-policy-profile", func(c *Config) { c.Policies[0].Profile = "missing" }},
		{"unowned-policy", func(c *Config) { c.Policies[0].Profile = "" }},
		{"missing-policy-set", func(c *Config) { c.Policies[0].Domain = ""; c.Policies[0].DomainSet = "missing" }},
		{"missing-action-profile", func(c *Config) { c.Rules = []Rule{{ID: "r", Domain: "*", Action: "nodata", Profile: "missing"}} }},
		{"missing-action-set", func(c *Config) { c.Rules = []Rule{{ID: "r", DomainSet: "missing", Action: "nodata"}} }},
		{"two-action-selectors", func(c *Config) { c.Rules = []Rule{{ID: "r", Domain: "*", DomainSet: "media", Action: "nodata"}} }},
		{"two-policy-selectors", func(c *Config) { c.Policies[0].DomainSet = "media" }},
		{"no-selector", func(c *Config) { c.Policies[0].Domain = "" }},
		{"bad-pattern", func(c *Config) { c.DomainSets[0].Domains = []string{"foo.*.test"} }},
		{"recursive-set", func(c *Config) { c.DomainSets[0].Domains = []string{"@media"} }},
		{"duplicate-pattern", func(c *Config) { c.DomainSets[0].Domains = []string{"A.TEST", "a.test."} }},
		{"empty-set", func(c *Config) { c.DomainSets[0].Domains = nil }},
		{"id-collision", func(c *Config) { c.DomainSets[0].ID = "work" }},
		{"reserved-id", func(c *Config) { c.Profiles[0].ID = "main" }},
		{"duplicate-name", func(c *Config) { c.Profiles[1].Name = " WORK " }},
		{"bad-label", func(c *Config) { c.Profiles[0].Name = "\n" }},
		{"infinite-wait", func(c *Config) { c.Policies[0].WaitMS = intp(-1) }},
		{"huge-delay", func(c *Config) { c.Policies[0].DelayMS = intp(501) }},
		{"over-total", func(c *Config) { c.Policies[0].WaitMS = intp(750); c.Policies[0].DelayMS = intp(500) }},
		{"inherited-over-total", func(c *Config) {
			c.Devices[0].WaitMS = 750
			c.Devices[0].DelayMS = 250
			c.Policies[0].DelayMS = intp(500)
		}},
		{"bad-policy-mode", func(c *Config) { c.Policies[0].Mode = "block" }},
		{"bad-prefer", func(c *Config) { c.Policies[0].Prefer = "nat64" }},
		{"invalid-inherited-custom-family", func(c *Config) { c.Devices[0].Prefer = "garbage"; c.Policies[0].Mode = "custom" }},
		{"empty-wildcard", func(c *Config) { c.Policies[0].Domain = "*." }},
		{"double-trailing-dot", func(c *Config) { c.Policies[0].Domain = "example.test.." }},
		{"too-many-profiles", func(c *Config) { c.Profiles = make([]Profile, MaxProfiles+1) }},
		{"too-many-policies", func(c *Config) { c.Policies = make([]Policy, MaxPolicies+1) }},
		{"too-many-sets", func(c *Config) { c.DomainSets = make([]DomainSet, MaxDomainSets+1) }},
		{"too-many-patterns-per-set", func(c *Config) { c.DomainSets[0].Domains = make([]string, MaxPatternsPerSet+1) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := profileConfig()
			c.Policies = []Policy{{ID: "p", Enabled: true, Profile: "work", Domain: "*", Mode: "inherit"}}
			tc.mutate(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("invalid advanced configuration accepted")
			}
		})
	}
	raw, _ := os.ReadFile("../../tests/fixtures/profiles.uci")
	for _, replacement := range []string{"option probe 'yes'", "option wait_ms 'forever'", "option delay_ms '1.5'"} {
		broken := strings.Replace(string(raw), "option probe '0'", replacement, 1)
		if _, err := ParseUCI(broken); err == nil {
			t.Fatal("invalid UCI value accepted", replacement)
		}
	}
}
func TestDomainPolicySelectionAndDirectDeviceInheritance(t *testing.T) {
	c := profileConfig()
	d := &c.Devices[0]
	d.Mode = "ipv4"
	d.WaitMS = 75
	d.DelayMS = 40
	c.Policies = []Policy{
		{ID: "set_policy", Enabled: true, Profile: "work", DomainSet: "media", Mode: "ipv6", DelayMS: intp(200)},
		{ID: "exact", Enabled: true, Profile: "work", Domain: "api.media.test", Mode: "dual"},
		{ID: "exact_later", Enabled: true, Profile: "work", Domain: "api.media.test", Mode: "ipv6"},
		{ID: "unused_profile", Enabled: true, Profile: "travel", Domain: "*", Mode: "ipv6"},
		{ID: "zero", Enabled: true, Profile: "work", Domain: "zero.test", Mode: "custom", Prefer: "ipv4", WaitMS: intp(0), DelayMS: intp(0), Probe: boolp(false)},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, policy, mode string
		a, b               int
		probe              bool
	}{
		{"media.test", "set_policy", "ipv6", 75, 200, true},
		{"deep.a.media.test", "set_policy", "ipv6", 75, 200, true},
		{"VIDEO.TEST.", "set_policy", "ipv6", 75, 200, true},
		{"api.media.test", "exact", "dual", 75, 40, true},
		{"notmedia.test", "", "ipv4", 75, 40, true},
		{"media.test.evil.test", "", "ipv4", 75, 40, true},
		{"zero.test", "zero", "custom", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolvePreference(&c, d, tc.name)
			if got.Policy != tc.policy || got.Mode != tc.mode || got.WaitMS != tc.a || got.DelayMS != tc.b || got.Probe != tc.probe {
				t.Fatal(got)
			}
		})
	}
	d.Profile = "travel"
	if got := ResolvePreference(&c, d, "api.media.test"); got.Policy != "unused_profile" {
		t.Fatal(got)
	}
	d.Profile = ""
	if got := ResolvePreference(&c, d, "media.test"); got.Policy != "" || got.Mode != "ipv4" {
		t.Fatal(got)
	}
	d.Profile = "work"
	d.Mode = "block"
	if got := ResolvePreference(&c, d, "media.test"); got.Policy != "" || got.Mode != "block" {
		t.Fatal("whole-device block overridden", got)
	}
}
func TestSetSpecificityAndDNSActionLayers(t *testing.T) {
	c := profileConfig()
	d := &c.Devices[0]
	c.DomainSets[0].Domains = append(c.DomainSets[0].Domains, "api.media.test", "*")
	c.Policies = []Policy{
		{ID: "broad", Enabled: true, Profile: "work", Domain: "*.media.test", Mode: "ipv4"},
		{ID: "set", Enabled: true, Profile: "work", DomainSet: "media", Mode: "ipv6"},
	}
	c.Rules = []Rule{
		{ID: "legacy", Enabled: true, Domain: "api.media.test", Action: "nxdomain"},
		{ID: "profile_broad", Enabled: true, Profile: "work", Domain: "*.media.test", Action: "forward"},
		{ID: "other_profile", Enabled: true, Profile: "travel", Domain: "api.media.test", Action: "forward"},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := ResolvePreference(&c, d, "api.media.test"); got.Policy != "set" || got.Pattern != "api.media.test" {
		t.Fatal(got)
	}
	q := Question{Name: "api.media.test", Type: TypeA}
	if got := MatchRule(&c, d, q); got.ID != "legacy" {
		t.Fatal("preference/profile broad action escaped exact legacy block", got)
	}
	c.Rules = append(c.Rules, Rule{ID: "profile_exact", Enabled: true, Profile: "work", DomainSet: "media", Action: "nodata"})
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := MatchRule(&c, d, q); got.ID != "profile_exact" {
		t.Fatal("profile did not win exact tie", got)
	}
	c.Rules = append(c.Rules, Rule{ID: "device_block", Enabled: true, Device: testMAC, Domain: "*", Action: "refused"})
	if got := MatchRule(&c, d, q); got.ID != "device_block" {
		t.Fatal("device scope no longer takes precedence", got)
	}
}
func TestProfileSnapshotOwnsAllMutableData(t *testing.T) {
	c := profileConfig()
	c.Policies = []Policy{{ID: "p", Enabled: true, Profile: "work", DomainSet: "media", Mode: "ipv6", DelayMS: intp(90), Probe: boolp(false)}}
	c.Rules = []Rule{{ID: "r", Enabled: true, Domain: "static.test", Action: "rewrite", IPv4: []string{"192.0.2.1"}}}
	e := NewEngine(c)
	first := e.Config()
	e.SetConfig(c)
	if e.Config() != first {
		t.Fatal("identical heartbeat discarded configuration/cache namespace")
	}
	c.Devices[0].Mode = "block"
	*c.Policies[0].DelayMS = 500
	*c.Policies[0].Probe = true
	c.DomainSets[0].Domains[0] = "*.different.test"
	c.Rules[0].IPv4[0] = "192.0.2.2"
	c.Interfaces[0] = "changed"
	p := ResolvePreference(first, &first.Devices[0], "a.media.test")
	if p.Mode != "ipv6" || p.DelayMS != 90 || p.Probe || first.Interfaces[0] != "br-lan" || first.Rules[0].IPv4[0] != "192.0.2.1" {
		t.Fatal("caller mutation leaked into snapshot", p)
	}
	e.SetConfig(c)
	if e.Config().fingerprint == first.fingerprint {
		t.Fatal("configuration change did not isolate evidence")
	}
}
func TestBadProfileApplyKeepsCurrentPolicyAndFirewall(t *testing.T) {
	m, f := testManager(t)
	if err := m.Apply(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	previous := m.Engine.Config()
	f.calls = nil
	f.config += "\nconfig policy 'broken'\n option profile 'missing'\n option domain '*'\n"
	if err := m.Apply(context.Background(), true); err == nil {
		t.Fatal("bad profile accepted")
	}
	if m.Engine.Config() != previous || !f.table || !m.Status().Active {
		t.Fatal("failed validation altered live state")
	}
	for _, call := range f.calls {
		if call.name != "uci" || strings.Join(call.args, " ") != "-q export netpreference" {
			t.Fatal("wrote before validation", call)
		}
	}
}
func FuzzDomainSetMatchesLinearReference(f *testing.F) {
	f.Add("*.example.com,api.example.com,*", "API.Example.Com.")
	f.Add("*.example.com,*.sub.example.com", "notexample.com")
	f.Fuzz(func(t *testing.T, patterns, name string) {
		if len(patterns) > 4096 || len(name) > 253 {
			return
		}
		var list []string
		for _, p := range strings.Split(patterns, ",") {
			p = normalizeDomain(p)
			if ValidDomain(p) {
				list = append(list, p)
			}
		}
		if len(list) > 32 {
			return
		}
		name = normalizeQueryName(name)
		got, pattern := newDomainMatcher(list).match(name)
		want := -1
		for _, p := range list {
			if s := domainScore(p, name); s > want {
				want = s
			}
		}
		if got != want || got >= 0 && domainScore(pattern, name) != got {
			t.Fatalf("%q %q: got %d/%q want %d", patterns, name, got, pattern, want)
		}
	})
}

func TestQueryWhitespaceIsNotConfigurationPadding(t *testing.T) {
	c := profileConfig()
	c.DomainSets[0].Domains = []string{"api", "*.media.test"}
	c.Policies = []Policy{{ID: "timing", Enabled: true, Profile: "work", DomainSet: "media", Mode: "ipv6"}}
	c.Rules = []Rule{{ID: "local", Enabled: true, Domain: "api", Action: "nxdomain"}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"api ", " api", "media.test ", " sub.media.test "} {
		if p := ResolvePreference(&c, &c.Devices[0], name); p.Policy != "" {
			t.Fatal("trimmed query matched timing", name)
		}
		if r := MatchRule(&c, &c.Devices[0], Question{Name: name, Type: TypeA}); r != nil {
			t.Fatal("trimmed query matched action", name)
		}
	}
}

func TestCustomDomainInheritsEffectiveDeviceFamily(t *testing.T) {
	d := DefaultDevice()
	d.Mode = "ipv4"
	d.Prefer = "ipv6"
	p := Policy{Mode: "custom"}
	if got := applyPolicy(&d, &p); got.Preferred() != TypeA {
		t.Fatal(got)
	}
	d.Mode = "ipv6"
	d.Prefer = "ipv4"
	if got := applyPolicy(&d, &p); got.Preferred() != TypeAAAA {
		t.Fatal(got)
	}
	p.Prefer = "ipv4"
	if got := applyPolicy(&d, &p); got.Preferred() != TypeA {
		t.Fatal(got)
	}
}
