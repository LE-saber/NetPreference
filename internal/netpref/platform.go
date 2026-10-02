package netpref

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Runner interface {
	Run(context.Context, string, []string, []byte) ([]byte, error)
}
type SystemRunner struct{}

func (SystemRunner) Run(ctx context.Context, name string, args []string, input []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if input != nil {
		cmd.Stdin = strings.NewReader(string(input))
	}
	out, err := cmd.CombinedOutput()
	if len(out) > 4*1024*1024 {
		return nil, fmt.Errorf("%s output exceeds limit", name)
	}
	if err != nil {
		return out, fmt.Errorf("%s: %w: %.1000s", name, err, out)
	}
	return out, nil
}
func LoadConfig(ctx context.Context, r Runner) (Config, error) {
	b, e := r.Run(ctx, "uci", []string{"-q", "export", "netpreference"}, nil)
	if e != nil {
		return Config{}, e
	}
	return ParseUCI(string(b))
}
func SetEnabled(ctx context.Context, r Runner, enabled bool) error {
	v := "0"
	if enabled {
		v = "1"
	}
	if _, e := r.Run(ctx, "uci", []string{"set", "netpreference.main.enabled=" + v}, nil); e != nil {
		return e
	}
	_, e := r.Run(ctx, "uci", []string{"commit", "netpreference"}, nil)
	return e
}

type Host struct {
	MAC  string   `json:"mac"`
	Name string   `json:"name"`
	IPv4 []string `json:"ipv4"`
	IPv6 []string `json:"ipv6"`
}
type Inventory struct {
	mu        sync.RWMutex
	hosts     []Host
	byIP      map[string]string
	runner    Runner
	leasePath string
	warning   string
}

func NewInventory(r Runner) *Inventory {
	return &Inventory{runner: r, leasePath: "/tmp/dhcp.leases", byIP: map[string]string{}}
}
func (i *Inventory) Refresh(ctx context.Context, c *Config) error {
	hosts := map[string]*Host{}
	ipmac := map[string]string{}
	var warnings []string
	get := func(mac string) *Host {
		m, e := NormalizeMAC(mac)
		if e != nil {
			return nil
		}
		if h := hosts[m]; h != nil {
			return h
		}
		if len(hosts) >= MaxDevices {
			return nil
		}
		h := &Host{MAC: m, IPv4: []string{}, IPv6: []string{}}
		hosts[m] = h
		return h
	}
	for _, d := range c.Devices {
		if h := get(d.MAC); h != nil {
			h.Name = d.Name
		}
	}
	addIP := func(h *Host, s string) {
		p := net.ParseIP(s)
		if p == nil || p.IsUnspecified() || p.IsMulticast() || p.IsLoopback() {
			return
		}
		s = p.String()
		if old, ok := ipmac[s]; ok && old != h.MAC {
			return
		}
		ipmac[s] = h.MAC
		list := &h.IPv6
		if p.To4() != nil {
			list = &h.IPv4
		}
		for _, v := range *list {
			if v == s {
				return
			}
		}
		if len(*list) < 32 {
			*list = append(*list, s)
		}
	}
	// Discover neighbour entries only on explicitly trusted ingress interfaces.
	allowed := map[string]bool{}
	for _, n := range c.Interfaces {
		allowed[n] = true
	}
	b, e := i.runner.Run(ctx, "ip", []string{"-j", "neigh", "show"}, nil)
	if e == nil {
		var rows []struct {
			Dst, Dev, LLAddr string
			State            json.RawMessage
		}
		if e = json.Unmarshal(b, &rows); e == nil {
			for _, row := range rows {
				if !allowed[row.Dev] || row.LLAddr == "" {
					continue
				}
				if h := get(row.LLAddr); h != nil {
					addIP(h, row.Dst)
				}
			}
		}
	}
	if e != nil {
		warnings = append(warnings, "neighbour discovery: "+e.Error())
	}
	// DHCP supplies IPv4 names and addresses. Only retain lease addresses that
	// fall within a selected LAN interface prefix, preventing WAN identity spoofing.
	var nets []*net.IPNet
	for _, name := range c.Interfaces {
		if in, err := net.InterfaceByName(name); err == nil {
			if as, err := in.Addrs(); err == nil {
				for _, a := range as {
					if n, ok := a.(*net.IPNet); ok {
						nets = append(nets, n)
					}
				}
			}
		}
	}
	if raw, err := os.ReadFile(i.leasePath); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			p := strings.Fields(line)
			if len(p) < 4 {
				continue
			}
			if h := get(p[1]); h != nil {
				if h.Name == "" && p[3] != "*" {
					h.Name = p[3]
				}
				ip := net.ParseIP(p[2])
				for _, n := range nets {
					if n.Contains(ip) {
						addIP(h, p[2])
						break
					}
				}
			}
		}
	}
	var list []Host
	for _, h := range hosts {
		sort.Strings(h.IPv4)
		sort.Strings(h.IPv6)
		list = append(list, *h)
	}
	sort.Slice(list, func(a, b int) bool { return list[a].MAC < list[b].MAC })
	i.mu.Lock()
	i.hosts = list
	i.byIP = ipmac
	i.warning = strings.Join(warnings, "; ")
	i.mu.Unlock()
	return e
}
func (i *Inventory) Identify(ip net.IP) string {
	if ip.IsLoopback() {
		return "@local"
	}
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.byIP[ip.String()]
}
func (i *Inventory) Hosts() []Host {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]Host, len(i.hosts))
	copy(out, i.hosts)
	return out
}
func (i *Inventory) Warning() string { i.mu.RLock(); defer i.mu.RUnlock(); return i.warning }

func AtomicJSON(path string, v any, mode os.FileMode) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".netpref-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if e = f.Chmod(mode); e == nil {
		_, e = f.Write(append(b, '\n'))
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

// Baseline is forensic evidence, not an instruction to overwrite shared configs.
func SaveBaseline(root string, r Runner) error {
	path := filepath.Join(root, "baseline.json")
	if st, e := os.Lstat(path); e == nil {
		if !st.Mode().IsRegular() {
			return fmt.Errorf("baseline is not a regular file")
		}
		return nil
	} else if !os.IsNotExist(e) {
		return e
	}
	type original struct {
		SHA256  string `json:"sha256"`
		Content string `json:"content"`
		Exists  bool   `json:"exists"`
	}
	data := map[string]any{"version": Version, "created": time.Now().UTC().Format(time.RFC3339), "restore_strategy": "delete only owned nft table; do not write these shared files"}
	files := map[string]original{}
	for _, p := range []string{"/etc/config/dhcp", "/etc/config/homeproxy", "/etc/config/smartdns", "/etc/config/firewall", "/etc/config/nlbwmon", "/etc/resolv.conf"} {
		f, e := os.Open(p)
		if os.IsNotExist(e) {
			files[p] = original{}
			continue
		}
		if e != nil {
			return e
		}
		b, e := io.ReadAll(io.LimitReader(f, 1024*1024+1))
		f.Close()
		if e != nil {
			return e
		}
		if len(b) > 1024*1024 {
			return fmt.Errorf("baseline file too large: %s", p)
		}
		sum := sha256.Sum256(b)
		files[p] = original{hex.EncodeToString(sum[:]), string(b), true}
	}
	data["files"] = files
	data["existing_components"] = map[string]bool{"nlbw": binaryExists("nlbw"), "dnsmasq": binaryExists("dnsmasq"), "sing-box": binaryExists("sing-box"), "smartdns": binaryExists("smartdns")}
	return AtomicJSON(path, data, 0600)
}
func binaryExists(n string) bool { _, e := exec.LookPath(n); return e == nil }
