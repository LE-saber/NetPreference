package netpref

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultSocket = "/var/run/netpreference/control.sock"
const DefaultStateDir = "/etc/netpreference"

type Status struct {
	Version          string `json:"version"`
	Desired          bool   `json:"desired"`
	Active           bool   `json:"active"`
	Monitoring       bool   `json:"monitoring"`
	Error            string `json:"error,omitempty"`
	LastTick         string `json:"last_tick"`
	Queries          uint64 `json:"queries"`
	Failures         uint64 `json:"failures"`
	Fallbacks        uint64 `json:"fallbacks"`
	Delayed          uint64 `json:"delayed"`
	Selected         int    `json:"selected_devices"`
	DiscoveryWarning string `json:"discovery_warning,omitempty"`
}
type Manager struct {
	mu              sync.Mutex
	Runner          Runner
	Firewall        Firewall
	Inventory       *Inventory
	Traffic         *Traffic
	Engine          *Engine
	StateDir        string
	current         Config
	hosts           []Host
	active          bool
	lastErr         string
	status          atomic.Pointer[Status]
	Health          func(context.Context, *Config) error
	CheckInterfaces func(*Config) error
	Baseline        func() error
}

func NewManager(r Runner, e *Engine) *Manager {
	m := &Manager{Runner: r, Firewall: Firewall{r}, Inventory: NewInventory(r), Traffic: NewTraffic(r), Engine: e, StateDir: DefaultStateDir, current: *e.Config()}
	m.Health = m.checkHealth
	m.CheckInterfaces = checkInterfaces
	m.Baseline = func() error { return SaveBaseline(m.StateDir, m.Runner) }
	m.publish()
	return m
}
func checkInterfaces(c *Config) error {
	for _, name := range c.Interfaces {
		in, e := net.InterfaceByName(name)
		if e != nil {
			return fmt.Errorf("LAN interface %s: %w", name, e)
		}
		if in.Flags&net.FlagUp == 0 {
			return fmt.Errorf("LAN interface %s is down", name)
		}
	}
	return nil
}
func (m *Manager) publish() {
	s := &Status{Version: Version, Desired: m.current.Enabled, Active: m.active, Monitoring: m.current.Enabled && m.current.Monitor, Error: m.lastErr, LastTick: time.Now().UTC().Format(time.RFC3339), Selected: len(selected(&m.current)), DiscoveryWarning: m.Inventory.Warning()}
	m.status.Store(s)
}
func (m *Manager) Status() Status {
	s := *m.status.Load()
	s.Queries = m.Engine.Queries.Load()
	s.Failures = m.Engine.Failures.Load()
	s.Fallbacks = m.Engine.Fallbacks.Load()
	s.Delayed = m.Engine.Delayed.Load()
	return s
}
func (m *Manager) checkHealth(ctx context.Context, c *Config) error {
	q := MakeQuery(".", 2)
	part, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutMS)*time.Millisecond)
	b, e := m.Engine.Exchange(part, q, c.Upstream)
	cancel()
	if e != nil {
		return fmt.Errorf("original DNS chain health: %w", e)
	}
	if len(b) < 4 || u16(b, 2)&15 != 0 {
		return errors.New("original DNS chain did not return NOERROR for root NS")
	}
	// Test both live proxy socket paths, not merely a process ID or HTTP endpoint.
	return SelfHealth(ctx)
}
func SelfHealth(ctx context.Context) error {
	q := MakeQuery("_netpreference.health", 1)
	parsed, _ := ValidateQuery(q)
	for _, network := range []string{"udp", "tcp"} {
		part, cancel := context.WithTimeout(ctx, time.Second)
		b, e := exchangeTransport(part, q, "127.0.0.1:1053", network, parsed)
		cancel()
		if e != nil {
			return fmt.Errorf("proxy %s health: %w", network, e)
		}
		if ok, _ := AddressEvidence(b, parsed); !ok {
			return fmt.Errorf("proxy %s health answer missing", network)
		}
	}
	return nil
}
func (m *Manager) Apply(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.publish()
	c, e := LoadConfig(ctx, m.Runner)
	if e != nil {
		m.lastErr = e.Error()
		return e
	}
	if force {
		c.Enabled = true
	}
	if !c.Enabled {
		return m.deactivateLocked(ctx, false)
	}
	if len(selected(&c)) == 0 && !c.Monitor {
		return errors.New("enable at least one device or traffic monitoring first")
	}
	if e = m.CheckInterfaces(&c); e != nil {
		m.lastErr = e.Error()
		return e
	}
	if e = m.Health(ctx, &c); e != nil {
		m.lastErr = e.Error()
		return e
	}
	exists, e := m.Firewall.Inspect(ctx)
	if e != nil {
		m.lastErr = e.Error()
		return e
	}
	if e = m.Baseline(); e != nil {
		m.lastErr = e.Error()
		return e
	}
	old := m.current
	oldhosts := m.hosts
	if e = m.Inventory.Refresh(ctx, &c); e != nil {
		m.lastErr = e.Error()
		return e
	}
	hosts := m.Inventory.Hosts()
	// DNS/profile-only changes need no table replacement. Named counters stay
	// alive while selected MAC/address sets are refreshed atomically.
	rebuild := !exists || !old.Enabled || old.Monitor != c.Monitor ||
		!slices.Equal(old.Interfaces, c.Interfaces) || !sameHosts(oldhosts, hosts)
	if rebuild {
		if old.Enabled && old.Monitor {
			m.Traffic.Sample(ctx, oldhosts)
		}
		e = m.Firewall.Apply(ctx, &c, hosts)
	} else {
		e = m.Firewall.Refresh(ctx, &c, hosts, true)
	}
	if e != nil {
		_ = m.Inventory.Refresh(ctx, &old)
		m.lastErr = e.Error()
		return e
	}
	// Persistence occurs after nft commits. On failure, restore only our own table.
	if force {
		e = SetEnabled(ctx, m.Runner, true)
	}
	if e == nil {
		if er := os.Remove(filepath.Join(m.StateDir, "restored")); er != nil && !os.IsNotExist(er) {
			e = er
		}
	}
	if e != nil {
		var rollback error
		if old.Enabled {
			rollback = m.Firewall.Apply(ctx, &old, oldhosts)
			m.Traffic.Rebase()
		} else {
			rollback = m.Firewall.Remove(ctx)
		}
		_ = m.Inventory.Refresh(ctx, &old)
		// Roll back only the plugin's own desired flag. Retain the inhibit
		// marker if storage cannot persist it, so an explicit failed Apply
		// cannot silently become an enabled boot configuration.
		persistRollback := SetEnabled(ctx, m.Runner, old.Enabled)
		if !old.Enabled {
			_ = AtomicJSON(filepath.Join(m.StateDir, "restored"), map[string]bool{"restored": true}, 0600)
		}
		m.lastErr = fmt.Sprintf("activation persistence: %v; rollback: %v; desired flag rollback: %v", e, rollback, persistRollback)
		if rollback != nil {
			disabled := old
			disabled.Enabled = false
			m.Engine.SetConfig(disabled)
			m.active = false
		}
		return errors.New(m.lastErr)
	}
	m.current = c
	m.hosts = hosts
	m.Engine.SetConfig(c)
	m.active = true
	m.lastErr = ""
	if rebuild {
		m.Traffic.Rebase()
	}
	if c.Monitor {
		// An enabled monitor is never advertised as off while awaiting first tick.
		m.Traffic.Sample(ctx, hosts)
	} else {
		m.Traffic.Off()
	}
	return nil
}
func (m *Manager) deactivateLocked(ctx context.Context, persist bool) error {
	var markerErr error
	if persist {
		markerErr = AtomicJSON(filepath.Join(m.StateDir, "restored"), map[string]any{"restored": time.Now().UTC().Format(time.RFC3339)}, 0600)
	}
	// First remove routing even when state storage is full/read-only.
	fwErr := m.Firewall.Remove(ctx)
	m.current.Enabled = false
	m.Engine.SetConfig(m.current)
	m.active = false
	m.Traffic.Off()
	var uciErr error
	if persist {
		uciErr = SetEnabled(ctx, m.Runner, false)
	}
	err := errors.Join(markerErr, fwErr, uciErr)
	m.lastErr = ""
	if err != nil {
		m.lastErr = err.Error()
	}
	return err
}
func (m *Manager) Restore(ctx context.Context, persist bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.publish()
	return m.deactivateLocked(ctx, persist)
}

// QueueStartup retries a validated desired configuration after boot-time DNS
// unavailability. Tick still performs baseline/ownership checks before writes.
func (m *Manager) QueueStartup(c Config, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.current = c
	m.active = false
	disabled := c
	disabled.Enabled = false
	m.Engine.SetConfig(disabled)
	if err != nil {
		m.lastErr = err.Error()
	}
	m.publish()
}
func (m *Manager) Tick(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer m.publish()
	c := m.current
	if e := m.Inventory.Refresh(ctx, &c); e != nil {
		m.lastErr = e.Error()
	}
	if !c.Enabled {
		return
	}
	hosts := m.Inventory.Hosts()
	// Observation is a read-only activity, independent of DNS health. Even on
	// a fail-open return, publish fresh samples or an explicit diagnostic error.
	defer func() {
		if c.Monitor {
			m.Traffic.Sample(ctx, hosts)
		} else {
			m.Traffic.Off()
		}
	}()
	healthy := m.Health(ctx, &c) == nil
	if !healthy {
		m.active = false
		disabled := c
		disabled.Enabled = false
		m.Engine.SetConfig(disabled)
		m.lastErr = "health check failed: DNS interception paused; original DNS path retained"
		if exists, er := m.Firewall.Inspect(ctx); er == nil && exists {
			_ = m.Firewall.Refresh(ctx, &c, m.hosts, false)
			if er = m.Firewall.CleanConntrack(ctx); er != nil {
				m.lastErr += "; " + er.Error()
			}
		}
		return
	}
	if er := m.CheckInterfaces(&c); er != nil {
		m.pauseLocked(ctx, c, er)
		return
	}
	if er := m.Baseline(); er != nil {
		m.pauseLocked(ctx, c, er)
		return
	}
	exists, er := m.Firewall.Inspect(ctx)
	if er == nil {
		if !exists || !sameHosts(m.hosts, hosts) {
			if c.Monitor && exists {
				m.Traffic.Sample(ctx, m.hosts)
			}
			er = m.Firewall.Apply(ctx, &c, hosts)
			if er == nil {
				m.Traffic.Rebase()
			}
		} else {
			er = m.Firewall.Refresh(ctx, &c, hosts, true)
		}
	}
	if er != nil {
		m.lastErr = er.Error()
		m.active = false
		disabled := c
		disabled.Enabled = false
		m.Engine.SetConfig(disabled)
		_ = m.Firewall.Remove(ctx)
		return
	}
	m.hosts = hosts
	m.active = true
	m.Engine.SetConfig(c)
	m.lastErr = ""
}
func (m *Manager) pauseLocked(ctx context.Context, c Config, err error) {
	m.active = false
	c.Enabled = false
	m.Engine.SetConfig(c)
	cleanup := m.Firewall.Remove(ctx)
	m.lastErr = errors.Join(err, cleanup).Error()
}
func (m *Manager) Run(ctx context.Context) {
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.Tick(ctx)
		}
	}
}

type ControlServer struct {
	server   *http.Server
	listener net.Listener
	path     string
}

func ListenControl(path string, m *Manager) (*ControlServer, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	if e := os.Chmod(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	if st, e := os.Lstat(path); e == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return nil, errors.New("control path exists and is not a socket")
		}
		if c, e := net.DialTimeout("unix", path, 300*time.Millisecond); e == nil {
			c.Close()
			return nil, errors.New("NetPreference control socket already active")
		}
		if e = os.Remove(path); e != nil {
			return nil, e
		}
	}
	l, e := net.Listen("unix", path)
	if e != nil {
		return nil, e
	}
	if e = os.Chmod(path, 0600); e != nil {
		l.Close()
		return nil, e
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		body, e := io.ReadAll(r.Body)
		if e != nil {
			w.WriteHeader(400)
			return
		}
		if len(body) > 0 {
			var v map[string]any
			if json.Unmarshal(body, &v) != nil || len(v) > 0 {
				w.WriteHeader(400)
				return
			}
		}
		method := strings.TrimPrefix(r.URL.Path, "/")
		var result any
		var err error
		switch method {
		case "status":
			result = m.Status()
		case "devices":
			result = map[string]any{"devices": m.Inventory.Hosts(), "warning": m.Inventory.Warning()}
		case "traffic":
			result = m.Traffic.Snapshot()
		case "apply":
			err = m.Apply(r.Context(), true)
		case "reload":
			err = m.Apply(r.Context(), false)
		case "restore":
			err = m.Restore(r.Context(), true)
		case "deactivate":
			err = m.Restore(r.Context(), false)
		case "validate":
			var c Config
			c, err = LoadConfig(r.Context(), m.Runner)
			if err == nil {
				result = map[string]any{"valid": true, "devices": len(c.Devices), "rules": len(c.Rules), "profiles": len(c.Profiles), "domain_sets": len(c.DomainSets), "policies": len(c.Policies)}
			}
		default:
			w.WriteHeader(404)
			return
		}
		if err != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if result == nil {
			result = map[string]any{"ok": true, "status": m.Status()}
		}
		_ = json.NewEncoder(w).Encode(result)
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 3 * time.Second}
	s := &ControlServer{server, l, path}
	go func() {
		if e := server.Serve(l); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Printf("control server: %v", e)
		}
	}()
	return s, nil
}
func (s *ControlServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.server.Shutdown(ctx)
	_ = s.listener.Close()
	_ = os.Remove(s.path)
}
func Control(ctx context.Context, path, method string) ([]byte, error) {
	switch method {
	case "status", "devices", "traffic", "apply", "reload", "restore", "deactivate", "validate":
	default:
		return nil, errors.New("unknown control method")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	req, e := http.NewRequestWithContext(ctx, "POST", "http://localhost/"+method, strings.NewReader("{}"))
	if e != nil {
		return nil, e
	}
	res, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024))
	if e != nil {
		return nil, e
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("control HTTP %d", res.StatusCode)
	}
	var failure struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if e = json.Unmarshal(b, &failure); e != nil {
		return nil, e
	}
	if failure.OK != nil && !*failure.OK {
		return b, errors.New(failure.Error)
	}
	return b, nil
}

// Guard is a separate procd instance. It never enables interception. If the
// daemon/socket stalls it removes owned routing; nft timeout is a second layer.
func Guard(ctx context.Context, r Runner, path string) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			part, cancel := context.WithTimeout(ctx, 2*time.Second)
			b, e := Control(part, path, "status")
			cancel()
			if e == nil {
				var s Status
				if json.Unmarshal(b, &s) != nil {
					e = errors.New("invalid status")
				} else if t, err := time.Parse(time.RFC3339, s.LastTick); err != nil || time.Since(t) > 35*time.Second {
					e = errors.New("stale manager heartbeat")
				}
			}
			if e == nil {
				part, cancel = context.WithTimeout(ctx, 3*time.Second)
				e = SelfHealth(part)
				cancel()
			}
			if e != nil {
				log.Printf("guard recovery: %v", e)
				part, cancel = context.WithTimeout(ctx, 10*time.Second)
				if er := (Firewall{r}).Remove(part); er != nil {
					log.Printf("guard cleanup: %v", er)
				}
				cancel()
			}
		}
	}
}
