package netpref

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxProfiles       = 64
	MaxDomainSets     = 128
	MaxPolicies       = 512
	MaxPatternsPerSet = 256
	MaxDomainPatterns = 4096
)

// A profile groups domain preferences and optionally DNS action rules. It never
// replaces a device's default. References use the stable section ID, not Name.
type Profile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type DomainSet struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
}

// Policy is independent from Rule (DNS content/action). One best policy wins;
// unset fields inherit directly from the device, not from less specific rules.
// Pointers distinguish a missing value from a deliberate 0 or false override.
type Policy struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	Profile   string `json:"profile"`
	Domain    string `json:"domain,omitempty"`
	DomainSet string `json:"domain_set,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Prefer    string `json:"prefer,omitempty"`
	WaitMS    *int   `json:"wait_ms,omitempty"`
	DelayMS   *int   `json:"delay_ms,omitempty"`
	Probe     *bool  `json:"probe,omitempty"`
}

// Preference is a resolved, request-local value. It is safe to adjust without
// mutating the device or any other client's shared profile.
type Preference struct {
	Mode    string `json:"mode"`
	Prefer  string `json:"prefer"`
	WaitMS  int    `json:"wait_ms"`
	DelayMS int    `json:"delay_ms"`
	Probe   bool   `json:"probe"`
	Profile string `json:"profile,omitempty"`
	Policy  string `json:"policy,omitempty"`
	Pattern string `json:"pattern,omitempty"`
}

func (p Preference) Preferred() uint16 {
	if p.Mode == "ipv4" || p.Mode == "custom" && p.Prefer == "ipv4" {
		return TypeA
	}
	return TypeAAAA
}
func (p Preference) Delays(qtype uint16) bool {
	return (p.Mode == "ipv4" || p.Mode == "ipv6" || p.Mode == "custom") &&
		(qtype == TypeA || qtype == TypeAAAA) && qtype != p.Preferred()
}
func devicePreference(d *Device) Preference {
	return Preference{Mode: d.Mode, Prefer: d.Prefer, WaitMS: d.WaitMS, DelayMS: d.DelayMS, Probe: d.Probe, Profile: d.Profile}
}
func applyPolicy(d *Device, p *Policy) Preference {
	result := devicePreference(d)
	if p.Mode != "" && p.Mode != "inherit" {
		result.Mode = p.Mode
	}
	if p.Prefer != "" {
		result.Prefer = p.Prefer
	} else if result.Mode == "custom" && (d.Mode == "ipv4" || d.Mode == "ipv6") {
		// Inherit the device's effective family, not an inactive custom-mode field.
		result.Prefer = d.Mode
	}
	if p.WaitMS != nil {
		result.WaitMS = *p.WaitMS
	}
	if p.DelayMS != nil {
		result.DelayMS = *p.DelayMS
	}
	if p.Probe != nil {
		result.Probe = *p.Probe
	}
	result.Policy = p.ID
	return result
}

func normalizeDomain(s string) string {
	s = strings.TrimSpace(s)
	if s == "*." {
		return s
	} // Do not turn an empty wildcard suffix into all domains.
	return strings.ToLower(strings.TrimSuffix(s, "."))
}

// Query labels are wire data: unlike configuration input, whitespace is not padding.
func normalizeQueryName(s string) string {
	b := []byte(strings.TrimSuffix(s, "."))
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
func validSelector(domain, set string) error {
	if (domain == "") == (set == "") {
		return errors.New("choose exactly one domain or domain_set")
	}
	if domain != "" && !ValidDomain(domain) {
		return fmt.Errorf("invalid domain %q", domain)
	}
	if set != "" && !idRE.MatchString(set) {
		return fmt.Errorf("invalid domain_set reference %q", set)
	}
	return nil
}
func validLabel(name string) bool {
	if !utf8.ValidString(name) || strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 80 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validTiming(a, b int) bool { return a >= 0 && a <= 750 && b >= 0 && b <= 500 && a+b <= 1000 }

func (c *Config) validateProfiles() error {
	if len(c.Profiles) > MaxProfiles || len(c.DomainSets) > MaxDomainSets || len(c.Policies) > MaxPolicies {
		return errors.New("limit: 64 profiles / 128 domain sets / 512 domain policies")
	}
	// UCI IDs share a single namespace, including the mandatory global section.
	ids := map[string]bool{"main": true}
	addID := func(id string) error {
		if !idRE.MatchString(id) || ids[id] {
			return fmt.Errorf("invalid/duplicate section id %q", id)
		}
		ids[id] = true
		return nil
	}
	for _, d := range c.Devices {
		if err := addID(d.ID); err != nil {
			return err
		}
	}
	for _, r := range c.Rules {
		if err := addID(r.ID); err != nil {
			return err
		}
	}
	profiles := map[string]bool{}
	names := map[string]bool{}
	for i := range c.Profiles {
		p := &c.Profiles[i]
		if err := addID(p.ID); err != nil {
			return err
		}
		p.Name = strings.TrimSpace(p.Name)
		key := strings.ToLower(p.Name)
		if !validLabel(p.Name) || names[key] {
			return fmt.Errorf("profile %s: invalid/duplicate name", p.ID)
		}
		if !utf8.ValidString(p.Description) || utf8.RuneCountInString(p.Description) > 256 {
			return fmt.Errorf("profile %s: description too long/invalid", p.ID)
		}
		profiles[p.ID], names[key] = true, true
	}
	sets := map[string]bool{}
	names = map[string]bool{}
	count := 0
	for i := range c.DomainSets {
		s := &c.DomainSets[i]
		if err := addID(s.ID); err != nil {
			return err
		}
		s.Name = strings.TrimSpace(s.Name)
		key := strings.ToLower(s.Name)
		if !validLabel(s.Name) || names[key] {
			return fmt.Errorf("domain_set %s: invalid/duplicate name", s.ID)
		}
		names[key], sets[s.ID] = true, true
		if len(s.Domains) == 0 || len(s.Domains) > MaxPatternsPerSet {
			return fmt.Errorf("domain_set %s: provide 1..256 domains", s.ID)
		}
		seen := map[string]bool{}
		for j, pattern := range s.Domains {
			pattern = normalizeDomain(pattern)
			if !ValidDomain(pattern) || seen[pattern] {
				return fmt.Errorf("domain_set %s: invalid/duplicate domain %q", s.ID, pattern)
			}
			s.Domains[j] = pattern
			seen[pattern] = true
			count++
		}
	}
	if count > MaxDomainPatterns {
		return errors.New("limit: 4096 total domain-set patterns")
	}
	checkRefs := func(profile, set string) error {
		if profile != "" && !profiles[profile] {
			return fmt.Errorf("unknown profile %q", profile)
		}
		if set != "" && !sets[set] {
			return fmt.Errorf("unknown domain_set %q", set)
		}
		return nil
	}
	for _, d := range c.Devices {
		if err := checkRefs(d.Profile, ""); err != nil {
			return fmt.Errorf("device %s: %w", d.ID, err)
		}
	}
	for _, r := range c.Rules {
		if err := checkRefs(r.Profile, r.DomainSet); err != nil {
			return fmt.Errorf("rule %s: %w", r.ID, err)
		}
	}
	for i := range c.Policies {
		p := &c.Policies[i]
		if err := addID(p.ID); err != nil {
			return err
		}
		if p.Profile == "" {
			return fmt.Errorf("policy %s requires a profile", p.ID)
		}
		if err := checkRefs(p.Profile, p.DomainSet); err != nil {
			return fmt.Errorf("policy %s: %w", p.ID, err)
		}
		p.Domain = normalizeDomain(p.Domain)
		if err := validSelector(p.Domain, p.DomainSet); err != nil {
			return fmt.Errorf("policy %s: %w", p.ID, err)
		}
		switch p.Mode {
		case "", "inherit", "dual", "ipv4", "ipv6", "custom":
		default:
			return fmt.Errorf("policy %s: invalid mode %q", p.ID, p.Mode)
		}
		if p.Prefer != "" && p.Prefer != "ipv4" && p.Prefer != "ipv6" {
			return fmt.Errorf("policy %s: invalid preferred family", p.ID)
		}
		// Validate standalone profiles against the ordinary defaults, and all their
		// actual device combinations (including disabled devices/rules for safe reuse).
		base := DefaultDevice()
		effective := applyPolicy(&base, p)
		if !validTiming(effective.WaitMS, effective.DelayMS) {
			return fmt.Errorf("policy %s: A=0..750, B=0..500 and A+B<=1000 ms", p.ID)
		}
		for j := range c.Devices {
			d := &c.Devices[j]
			if d.Profile != p.Profile {
				continue
			}
			effective = applyPolicy(d, p)
			if !validTiming(effective.WaitMS, effective.DelayMS) {
				return fmt.Errorf("policy %s + device %s: inherited A+B exceeds 1000 ms", p.ID, d.ID)
			}
			if effective.Mode == "custom" && effective.Prefer != "ipv4" && effective.Prefer != "ipv6" {
				return fmt.Errorf("policy %s + device %s: custom mode needs ipv4/ipv6 prefer", p.ID, d.ID)
			}
		}
	}
	c.compileDomainSets()
	return nil
}

// A set is indexed once, not expanded into rules or scanned for every query.
// Exact matches and successive label suffixes are O(number of DNS labels).
type domainMatcher struct {
	exact  map[string]bool
	suffix map[string]bool
	any    bool
}

func newDomainMatcher(patterns []string) *domainMatcher {
	m := &domainMatcher{exact: map[string]bool{}, suffix: map[string]bool{}}
	for _, p := range patterns {
		p = normalizeDomain(p)
		if p == "*" {
			m.any = true
		} else if strings.HasPrefix(p, "*.") {
			m.suffix[p[2:]] = true
		} else {
			m.exact[p] = true
		}
	}
	return m
}
func (m *domainMatcher) match(name string) (int, string) {
	name = normalizeQueryName(name)
	best, pattern := -1, ""
	if m.any {
		best, pattern = 0, "*"
	}
	if m.exact[name] {
		return len(name)*2 + 1, name
	}
	for suffix := name; suffix != ""; {
		if m.suffix[suffix] {
			return len(suffix) * 2, "*." + suffix
		}
		i := strings.IndexByte(suffix, '.')
		if i < 0 {
			break
		}
		suffix = suffix[i+1:]
	}
	return best, pattern
}
func (c *Config) compileDomainSets() {
	c.setMatchers = make(map[string]*domainMatcher, len(c.DomainSets))
	for _, s := range c.DomainSets {
		c.setMatchers[s.ID] = newDomainMatcher(s.Domains)
	}
}
func selectorMatch(c *Config, domain, set, name string) (int, string) {
	name = normalizeQueryName(name)
	if set == "" {
		return domainScore(normalizeDomain(domain), name), normalizeDomain(domain)
	}
	if m := c.setMatchers[set]; m != nil {
		return m.match(name)
	}
	// Pure fallback for callers constructing a Config directly in tests/tools.
	// Never mutate the published Config while servicing a DNS request.
	for _, s := range c.DomainSets {
		if s.ID == set {
			return newDomainMatcher(s.Domains).match(name)
		}
	}
	return -1, ""
}
func ResolvePreference(c *Config, d *Device, name string) Preference {
	result := devicePreference(d)
	if d.Profile == "" || d.Mode == "block" {
		return result
	}
	best := -1
	for i := range c.Policies {
		p := &c.Policies[i]
		if !p.Enabled || p.Profile != d.Profile {
			continue
		}
		score, pattern := selectorMatch(c, p.Domain, p.DomainSet, name)
		if score > best {
			best = score
			result = applyPolicy(d, p)
			result.Pattern = pattern
		}
	}
	return result
}
