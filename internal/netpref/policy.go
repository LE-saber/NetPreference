package netpref

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func qtypeName(t uint16) string {
	switch t {
	case 1:
		return "A"
	case 28:
		return "AAAA"
	case 64:
		return "SVCB"
	case 65:
		return "HTTPS"
	}
	return "OTHER"
}
func domainScore(pattern, name string) int {
	if pattern == "*" {
		return 0
	}
	if strings.HasPrefix(pattern, "*.") {
		s := pattern[2:]
		if name == s || strings.HasSuffix(name, "."+s) {
			return len(s) * 2
		}
		return -1
	}
	if pattern == name {
		return len(pattern)*2 + 1
	}
	return -1
}

// Device scope wins, then matched domain specificity. Profile-scoped actions
// win only a complete tie, then UCI order. Timing policies never compete here.
func MatchRule(c *Config, d *Device, q Question) *Rule {
	best := -1
	var result *Rule
	for i := range c.Rules {
		r := &c.Rules[i]
		if !r.Enabled || r.Device != "" && r.Device != "*" && r.Device != d.MAC {
			continue
		}
		if r.Profile != "" && r.Profile != d.Profile {
			continue
		}
		if r.QType != "" && r.QType != "*" && r.QType != qtypeName(q.Type) {
			continue
		}
		s, _ := selectorMatch(c, r.Domain, r.DomainSet, q.Name)
		if s < 0 {
			continue
		}
		s *= 2
		if r.Profile != "" {
			s++
		}
		if r.Device == d.MAC {
			s += 10000
		}
		if s > best {
			best = s
			result = r
		}
	}
	return result
}
func LocalRule(q Question, r *Rule) ([]byte, bool) {
	var ips []netip.Addr
	action := r.Action
	if action == "ipv4_only" {
		if q.Type == TypeAAAA {
			action = "nodata"
		} else {
			return nil, false
		}
	}
	if action == "ipv6_only" {
		if q.Type == TypeA {
			action = "nodata"
		} else {
			return nil, false
		}
	}
	switch action {
	case "forward":
		return nil, false
	case "nxdomain":
		return Reply(q, 3, nil, r.TTL), true
	case "refused":
		return Reply(q, 5, nil, r.TTL), true
	case "nodata":
		return Reply(q, 0, nil, r.TTL), true
	case "sinkhole":
		if q.Type == TypeA {
			ips = []netip.Addr{netip.MustParseAddr("0.0.0.0")}
		} else if q.Type == TypeAAAA {
			ips = []netip.Addr{netip.IPv6Unspecified()}
		}
	case "rewrite":
		list := r.IPv4
		if q.Type == TypeAAAA {
			list = r.IPv6
		}
		for _, s := range list {
			ip, e := netip.ParseAddr(s)
			if e == nil {
				ips = append(ips, ip)
			}
		}
	default:
		return nil, false
	}
	return Reply(q, 0, ips, r.TTL), true
}

type observation struct {
	ready                 chan struct{}
	done, exists, probing bool
	until                 time.Time
}
type evidenceCache struct {
	mu sync.Mutex
	m  map[string]*observation
}

func newCache() *evidenceCache { return &evidenceCache{m: map[string]*observation{}} }
func (c *evidenceCache) watch(key string) (*observation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if o := c.m[key]; o != nil && now.Before(o.until) {
		return o, false
	}
	if len(c.m) >= 2048 {
		for k, o := range c.m {
			if now.After(o.until) {
				delete(c.m, k)
			}
		}
		if len(c.m) >= 2048 {
			for k := range c.m {
				delete(c.m, k)
				break
			}
		}
	}
	o := &observation{ready: make(chan struct{}), until: now.Add(5 * time.Second)}
	c.m[key] = o
	return o, true
}
func (c *evidenceCache) beginProbe(o *observation) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if o.done || o.probing {
		return false
	}
	o.probing = true
	return true
}
func (c *evidenceCache) publish(key string, exists bool, ttl time.Duration) {
	o, _ := c.watch(key)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !o.done {
		o.done = true
		o.exists = exists
		o.until = time.Now().Add(ttl)
		close(o.ready)
	}
}
func (c *evidenceCache) positive(o *observation) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return o.done && o.exists
}

type Engine struct {
	config    atomic.Pointer[Config]
	cache     *evidenceCache
	Exchange  ExchangeFunc
	Queries   atomic.Uint64
	Failures  atomic.Uint64
	Fallbacks atomic.Uint64
	Delayed   atomic.Uint64
	probes    chan struct{}
}

func NewEngine(c Config) *Engine {
	e := &Engine{cache: newCache(), Exchange: Exchange, probes: make(chan struct{}, 32)}
	e.SetConfig(c)
	return e
}

// SetConfig takes ownership of a deep immutable snapshot, not the caller's
// slice backing arrays or nullable policy fields. A stable content fingerprint
// isolates evidence across profile/action/parameter changes, while the ordinary
// 3-second manager heartbeat with identical configuration keeps its cache.
func (e *Engine) SetConfig(c Config) {
	c.Devices = slices.Clone(c.Devices)
	c.Profiles = slices.Clone(c.Profiles)
	c.DomainSets = slices.Clone(c.DomainSets)
	for i := range c.DomainSets {
		c.DomainSets[i].Domains = slices.Clone(c.DomainSets[i].Domains)
	}
	c.Rules = slices.Clone(c.Rules)
	for i := range c.Rules {
		c.Rules[i].IPv4 = slices.Clone(c.Rules[i].IPv4)
		c.Rules[i].IPv6 = slices.Clone(c.Rules[i].IPv6)
	}
	c.Policies = slices.Clone(c.Policies)
	for i := range c.Policies {
		p := &c.Policies[i]
		if p.WaitMS != nil {
			v := *p.WaitMS
			p.WaitMS = &v
		}
		if p.DelayMS != nil {
			v := *p.DelayMS
			p.DelayMS = &v
		}
		if p.Probe != nil {
			v := *p.Probe
			p.Probe = &v
		}
	}
	c.Interfaces = slices.Clone(c.Interfaces)
	// Config contains only JSON primitives/slices; marshaling cannot fail.
	raw, _ := json.Marshal(c)
	c.fingerprint = sha256.Sum256(raw)
	if old := e.config.Load(); old != nil && old.fingerprint == c.fingerprint {
		return
	}
	c.compileDomainSets()
	e.config.Store(&c)
}
func (e *Engine) Config() *Config { return e.config.Load() }
func (e *Engine) evidenceKey(c *Config, d *Device, q Question, up string) string {
	return fmt.Sprintf("%x|%s|%s|%s|%d", c.fingerprint, d.MAC, up, q.Name, q.Type)
}

// Resolve each query type independently. An A-only resolver override must not
// silently become the AAAA probe's resolver (and vice versa).
func dnsRoute(c *Config, d *Device, q Question) (string, *Rule) {
	up := c.Upstream
	if d.Upstream != "" {
		up = d.Upstream
	}
	r := MatchRule(c, d, q)
	if r != nil && r.Upstream != "" {
		up = r.Upstream
	}
	return up, r
}
func (e *Engine) upstream(ctx context.Context, raw []byte, up string, c *Config) ([]byte, error) {
	call := func(s string) ([]byte, error) {
		part, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutMS)*time.Millisecond)
		defer cancel()
		return e.Exchange(part, raw, s)
	}
	b, err := call(up)
	if (err != nil || len(b) >= 4 && (u16(b, 2)&15 == 2 || u16(b, 2)&15 == 5)) && up != c.Upstream {
		e.Fallbacks.Add(1)
		return call(c.Upstream)
	}
	return b, err
}
func (e *Engine) Handle(ctx context.Context, raw []byte, mac string) []byte {
	e.Queries.Add(1)
	q, err := ValidateQuery(raw)
	if err != nil {
		return ErrorReply(raw, 1)
	}
	c := e.Config()
	d := c.Device(mac)
	// Unknown devices can never turn this listener into an open resolver.
	if mac == "@local" {
		if q.Name == "_netpreference.health" {
			var ip netip.Addr
			if q.Type == TypeA {
				ip = netip.MustParseAddr("127.0.0.1")
			} else if q.Type == TypeAAAA {
				ip = netip.IPv6Loopback()
			}
			if ip.IsValid() {
				return Reply(q, 0, []netip.Addr{ip}, 0)
			}
			return Reply(q, 0, nil, 0)
		}
		return Reply(q, 5, nil, 0)
	}
	if d == nil {
		if mac == "" {
			return Reply(q, 5, nil, 0)
		}
		// Known LAN neighbours displaced by a config update keep ordinary DNS.
		b, err := e.upstream(ctx, raw, c.Upstream, c)
		if err != nil {
			e.Failures.Add(1)
			return Reply(q, 2, nil, 0)
		}
		return b
	}
	if !c.Enabled {
		b, err := e.upstream(ctx, raw, c.Upstream, c)
		if err != nil {
			e.Failures.Add(1)
			return Reply(q, 2, nil, 0)
		}
		return b
	}
	if d.Mode == "block" {
		return Reply(q, 5, nil, 0)
	}
	up, r := dnsRoute(c, d, q)
	if r != nil {
		if b, local := LocalRule(q, r); local {
			if q.Type == TypeA || q.Type == TypeAAAA {
				positive, ttl := AddressEvidence(b, q)
				e.cache.publish(e.evidenceKey(c, d, q, up), positive, ttl)
			}
			return b
		}
	}
	policy := ResolvePreference(c, d, q.Name)
	prefer := policy.Preferred()
	delaying := policy.Delays(q.Type)
	var obs *observation
	started := time.Now()
	if delaying {
		pq := q
		pq.Type = prefer
		pup, pr := dnsRoute(c, d, pq)
		key := e.evidenceKey(c, d, pq, pup)
		obs, _ = e.cache.watch(key)
		if policy.Probe && e.cache.beginProbe(obs) {
			select {
			case e.probes <- struct{}{}:
				go func() {
					defer func() { <-e.probes }()
					pctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.TimeoutMS)*time.Millisecond)
					defer cancel()
					// Probes also obey the preferred-family domain policy; never bypass a block.
					var b []byte
					var err error
					if pr != nil {
						if local, ok := LocalRule(pq, pr); ok {
							b = local
						}
					}
					if b == nil {
						b, err = e.upstream(pctx, probeQuery(q, prefer), pup, c)
					}
					if err == nil {
						actual, er := ParseQuestion(b)
						if er == nil {
							positive, ttl := AddressEvidence(b, actual)
							e.cache.publish(key, positive, ttl)
							return
						}
					}
					e.cache.publish(key, false, time.Second)
				}()
			default:
				e.cache.publish(key, false, time.Second)
			}
		}
	}
	b, err := e.upstream(ctx, raw, up, c)
	if err != nil {
		e.Failures.Add(1)
		return Reply(q, 2, nil, 0)
	}
	if q.Type == 1 || q.Type == 28 {
		positive, ttl := AddressEvidence(b, q)
		e.cache.publish(e.evidenceKey(c, d, q, up), positive, ttl)
	}
	positiveAnswer, _ := AddressEvidence(b, q)
	if delaying && positiveAnswer {
		// A is measured from arrival, not after upstream latency. B is applied only
		// after positive preferred-family evidence. A+B is globally bounded at 1s.
		remaining := time.Until(started.Add(time.Duration(policy.WaitMS) * time.Millisecond))
		if remaining > 0 {
			timer := time.NewTimer(remaining)
			select {
			case <-obs.ready:
			case <-timer.C:
			case <-ctx.Done():
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		if e.cache.positive(obs) && policy.DelayMS > 0 {
			until := time.Now().Add(time.Duration(policy.DelayMS) * time.Millisecond)
			cap := started.Add(time.Duration(policy.WaitMS+policy.DelayMS) * time.Millisecond)
			if until.After(cap) {
				until = cap
			}
			if wait := time.Until(until); wait > 0 {
				e.Delayed.Add(1)
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
				case <-ctx.Done():
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
				}
			}
		}
	}
	return b
}
