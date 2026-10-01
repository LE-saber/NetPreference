package netpref

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

const Version = "0.1.0"
const DNSPort = 1053
const MaxDevices = 128

// Config is validated before publication and must then be treated as immutable.
type Config struct {
	Enabled    bool     `json:"enabled"`
	Monitor    bool     `json:"monitor"`
	Interfaces []string `json:"interfaces"`
	Upstream   string   `json:"upstream"`
	TimeoutMS  int      `json:"timeout_ms"`
	Devices    []Device `json:"devices"`
	Rules      []Rule   `json:"rules"`
}
type Device struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MAC      string `json:"mac"`
	Enabled  bool   `json:"enabled"`
	Mode     string `json:"mode"`
	Prefer   string `json:"prefer"`
	WaitMS   int    `json:"wait_ms"`
	DelayMS  int    `json:"delay_ms"`
	Probe    bool   `json:"probe"`
	Upstream string `json:"upstream"`
}
type Rule struct {
	ID       string   `json:"id"`
	Enabled  bool     `json:"enabled"`
	Device   string   `json:"device"` // empty or * = global; otherwise device MAC
	Domain   string   `json:"domain"` // exact, *.suffix (includes apex), or *
	QType    string   `json:"qtype"`  // empty or * = all; A, AAAA, HTTPS, SVCB
	Action   string   `json:"action"`
	IPv4     []string `json:"ipv4"`
	IPv6     []string `json:"ipv6"`
	Upstream string   `json:"upstream"`
	TTL      int      `json:"ttl"`
}

func DefaultConfig() Config {
	return Config{Interfaces: []string{"br-lan"}, Upstream: "127.0.0.1:53", TimeoutMS: 2000}
}
func DefaultDevice() Device {
	return Device{Enabled: true, Mode: "dual", Prefer: "ipv6", WaitMS: 120, DelayMS: 80, Probe: true}
}
func (d Device) Preferred() uint16 {
	if d.Mode == "ipv4" || d.Mode == "custom" && d.Prefer == "ipv4" {
		return 1
	}
	return 28
}
func (c Config) Device(mac string) *Device {
	for i := range c.Devices {
		if c.Devices[i].Enabled && c.Devices[i].MAC == strings.ToLower(mac) {
			return &c.Devices[i]
		}
	}
	return nil
}

var ifaceRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,14}$`)
var idRE = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

func NormalizeMAC(s string) (string, error) {
	m, e := net.ParseMAC(s)
	if e != nil || len(m) != 6 || m[0]&1 != 0 || m.String() == "00:00:00:00:00:00" {
		return "", fmt.Errorf("invalid unicast MAC %q", s)
	}
	return m.String(), nil
}
func Endpoint(s string) (network, addr string, err error) {
	network = "udp"
	addr = s
	if strings.Contains(s, "://") {
		p := strings.SplitN(s, "://", 2)
		network, addr = p[0], p[1]
	}
	if network != "udp" && network != "tcp" {
		return "", "", errors.New("upstream must use udp or tcp")
	}
	ap, e := netip.ParseAddrPort(addr)
	if e != nil || ap.Port() == 0 || ap.Addr().IsUnspecified() || ap.Addr().IsMulticast() || ap.Addr().Zone() != "" {
		return "", "", fmt.Errorf("upstream requires literal IP:port (IPv6 in brackets): %q", s)
	}
	// Reserve this port completely; this also avoids indirect local aliases looping.
	if ap.Port() == DNSPort {
		return "", "", errors.New("upstream may not use reserved NetPreference port 1053")
	}
	return network, ap.String(), nil
}
func ValidDomain(s string) bool {
	if s == "*" {
		return true
	}
	s = strings.TrimPrefix(s, "*.")
	s = strings.TrimSuffix(s, ".")
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if len(l) == 0 || len(l) > 63 {
			return false
		}
		for _, r := range l {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				return false
			}
		}
	}
	return true
}
func (c *Config) Validate() error {
	if len(c.Interfaces) == 0 || len(c.Interfaces) > 16 {
		return errors.New("provide 1..16 trusted LAN bridge/interfaces")
	}
	seen := map[string]bool{}
	for _, v := range c.Interfaces {
		if !ifaceRE.MatchString(v) || v == "lo" || seen[v] {
			return fmt.Errorf("invalid/duplicate LAN interface %q", v)
		}
		seen[v] = true
	}
	if _, _, e := Endpoint(c.Upstream); e != nil {
		return e
	}
	if c.TimeoutMS < 100 || c.TimeoutMS > 5000 {
		return errors.New("timeout_ms must be 100..5000")
	}
	if len(c.Devices) > MaxDevices || len(c.Rules) > 512 {
		return errors.New("limit: 128 devices / 512 rules")
	}
	seen = map[string]bool{}
	ids := map[string]bool{}
	for i := range c.Devices {
		d := &c.Devices[i]
		if !idRE.MatchString(d.ID) || ids[d.ID] {
			return fmt.Errorf("invalid/duplicate device id %q", d.ID)
		}
		ids[d.ID] = true
		m, e := NormalizeMAC(d.MAC)
		if e != nil {
			return e
		}
		d.MAC = m
		if seen[m] {
			return fmt.Errorf("duplicate device %s", m)
		}
		seen[m] = true
		if len(d.Name) > 80 {
			return errors.New("device name too long")
		}
		switch d.Mode {
		case "dual", "ipv4", "ipv6", "custom", "block":
		default:
			return fmt.Errorf("invalid device mode %q", d.Mode)
		}
		if d.Mode == "custom" && d.Prefer != "ipv4" && d.Prefer != "ipv6" {
			return errors.New("custom prefer must be ipv4 or ipv6")
		}
		if d.WaitMS < 0 || d.WaitMS > 750 || d.DelayMS < 0 || d.DelayMS > 500 || d.WaitMS+d.DelayMS > 1000 {
			return errors.New("A=0..750ms, B=0..500ms, A+B <=1000ms; infinite waits are not supported")
		}
		if d.Upstream != "" {
			if _, _, e := Endpoint(d.Upstream); e != nil {
				return e
			}
		}
	}
	for i := range c.Rules {
		r := &c.Rules[i]
		if !idRE.MatchString(r.ID) || ids[r.ID] {
			return fmt.Errorf("invalid/duplicate rule id %q", r.ID)
		}
		ids[r.ID] = true
		if r.Device != "" && r.Device != "*" {
			m, e := NormalizeMAC(r.Device)
			if e != nil {
				return e
			}
			r.Device = m
			if !seen[m] {
				return fmt.Errorf("rule %s refers to unknown device", r.ID)
			}
		}
		r.Domain = strings.ToLower(strings.TrimSuffix(r.Domain, "."))
		if !ValidDomain(r.Domain) {
			return fmt.Errorf("invalid domain %q", r.Domain)
		}
		switch r.QType {
		case "", "*", "A", "AAAA", "HTTPS", "SVCB":
		default:
			return fmt.Errorf("invalid qtype %q", r.QType)
		}
		switch r.Action {
		case "forward", "rewrite", "nxdomain", "nodata", "refused", "sinkhole", "ipv4_only", "ipv6_only":
		default:
			return fmt.Errorf("invalid action %q", r.Action)
		}
		if r.TTL < 0 || r.TTL > 86400 {
			return errors.New("TTL must be 0..86400")
		}
		if len(r.IPv4) > 8 || len(r.IPv6) > 8 {
			return errors.New("max 8 override addresses per family")
		}
		for _, a := range r.IPv4 {
			ip, e := netip.ParseAddr(a)
			if e != nil || !ip.Is4() {
				return fmt.Errorf("invalid IPv4 override %q", a)
			}
		}
		for _, a := range r.IPv6 {
			ip, e := netip.ParseAddr(a)
			if e != nil || !ip.Is6() || ip.Is4In6() || ip.Zone() != "" {
				return fmt.Errorf("invalid IPv6 override %q", a)
			}
		}
		if r.Action == "rewrite" && len(r.IPv4)+len(r.IPv6) == 0 {
			return errors.New("rewrite requires at least one address")
		}
		if r.Upstream != "" {
			if _, _, e := Endpoint(r.Upstream); e != nil {
				return e
			}
		}
	}
	return nil
}

// UCITokens reads UCI export syntax without invoking a shell or evaluating values.
func UCITokens(s string) ([]string, error) {
	var out []string
	var b strings.Builder
	var quote byte
	in := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else if ch == '\\' && quote == '"' {
				i++
				if i >= len(s) {
					return nil, errors.New("trailing escape")
				}
				b.WriteByte(s[i])
			} else {
				b.WriteByte(ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			in = true
		case '\\':
			i++
			if i >= len(s) {
				return nil, errors.New("trailing escape")
			}
			b.WriteByte(s[i])
			in = true
		case ' ', '\t', '\r':
			if in {
				out = append(out, b.String())
				b.Reset()
				in = false
			}
		case '#':
			if !in {
				return out, nil
			}
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
			in = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated UCI quote")
	}
	if in {
		out = append(out, b.String())
	}
	return out, nil
}

type uciSection struct {
	kind, id string
	values   map[string][]string
}

func ParseUCI(text string) (Config, error) {
	c := DefaultConfig()
	var secs []uciSection
	scan := bufio.NewScanner(strings.NewReader(text))
	scan.Buffer(make([]byte, 1024), 1024*1024)
	for n := 1; scan.Scan(); n++ {
		t, e := UCITokens(scan.Text())
		if e != nil {
			return c, fmt.Errorf("line %d: %w", n, e)
		}
		if len(t) == 0 {
			continue
		}
		switch t[0] {
		case "package":
			if len(t) != 2 || t[1] != "netpreference" {
				return c, errors.New("wrong UCI package")
			}
		case "config":
			if len(t) < 2 || len(t) > 3 {
				return c, errors.New("invalid config section")
			}
			id := fmt.Sprintf("section%d", len(secs))
			if len(t) == 3 {
				id = t[2]
			}
			secs = append(secs, uciSection{t[1], id, map[string][]string{}})
		case "option", "list":
			if len(t) != 3 || len(secs) == 0 {
				return c, errors.New("invalid option/list")
			}
			s := &secs[len(secs)-1]
			if t[0] == "option" {
				s.values[t[1]] = []string{t[2]}
			} else {
				s.values[t[1]] = append(s.values[t[1]], t[2])
			}
		default:
			return c, fmt.Errorf("unexpected UCI directive %q", t[0])
		}
	}
	if e := scan.Err(); e != nil {
		return c, e
	}
	global := 0
	for _, s := range secs {
		get := func(k, def string) string {
			v := s.values[k]
			if len(v) == 0 {
				return def
			}
			return v[len(v)-1]
		}
		num := func(k string, def int) (int, error) { return strconv.Atoi(get(k, strconv.Itoa(def))) }
		boolean := func(k string, def bool) (bool, error) {
			d := "0"
			if def {
				d = "1"
			}
			v := get(k, d)
			if v != "0" && v != "1" {
				return false, fmt.Errorf("%s must be 0 or 1", k)
			}
			return v == "1", nil
		}
		var e error
		switch s.kind {
		case "global":
			global++
			if s.id != "main" {
				return c, errors.New("global section must be named main")
			}
			c.Enabled, e = boolean("enabled", false)
			if e != nil {
				return c, e
			}
			c.Monitor, e = boolean("monitor", false)
			if e != nil {
				return c, e
			}
			c.Upstream = get("upstream", c.Upstream)
			c.TimeoutMS, e = num("timeout_ms", c.TimeoutMS)
			if e != nil {
				return c, e
			}
			if v := s.values["interface"]; len(v) > 0 {
				c.Interfaces = v
			}
		case "device":
			d := DefaultDevice()
			d.ID = s.id
			d.Name = get("name", "")
			d.MAC = get("mac", "")
			d.Mode = get("mode", d.Mode)
			d.Prefer = get("prefer", d.Prefer)
			d.Upstream = get("upstream", "")
			d.Enabled, e = boolean("enabled", true)
			if e != nil {
				return c, e
			}
			d.Probe, e = boolean("probe", true)
			if e != nil {
				return c, e
			}
			d.WaitMS, e = num("wait_ms", d.WaitMS)
			if e != nil {
				return c, e
			}
			d.DelayMS, e = num("delay_ms", d.DelayMS)
			if e != nil {
				return c, e
			}
			c.Devices = append(c.Devices, d)
		case "rule":
			r := Rule{ID: s.id, Device: get("device", "*"), Domain: get("domain", ""), QType: get("qtype", "*"), Action: get("action", "nxdomain"), Upstream: get("upstream", ""), IPv4: s.values["ipv4"], IPv6: s.values["ipv6"]}
			r.Enabled, e = boolean("enabled", true)
			if e != nil {
				return c, e
			}
			r.TTL, e = num("ttl", 60)
			if e != nil {
				return c, e
			}
			c.Rules = append(c.Rules, r)
		default:
			return c, fmt.Errorf("unknown section type %q", s.kind)
		}
	}
	if global != 1 {
		return c, errors.New("exactly one global main section is required")
	}
	return c, c.Validate()
}
