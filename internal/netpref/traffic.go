package netpref

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Totals struct {
	Up   uint64 `json:"upload_bytes"`
	Down uint64 `json:"download_bytes"`
}
type HostTotals struct {
	V4 Totals `json:"ipv4"`
	V6 Totals `json:"ipv6"`
}
type Rates struct {
	Up    float64 `json:"upload_Bps"`
	Down  float64 `json:"download_Bps"`
	Valid bool    `json:"valid"`
}
type TrafficRow struct {
	Host        Host       `json:"host"`
	Totals      HostTotals `json:"totals"`
	V4          Rates      `json:"ipv4_rate"`
	V6          Rates      `json:"ipv6_rate"`
	IPv6Ratio   float64    `json:"ipv6_ratio"`
	V4Available bool       `json:"ipv4_available"`
	V6Available bool       `json:"ipv6_available"`
}
type TrafficSnapshot struct {
	Enabled         bool         `json:"enabled"`
	State           string       `json:"state"`
	MissingCounters []string     `json:"missing_counters,omitempty"`
	At              string       `json:"at"`
	Interval        float64      `json:"interval_seconds"`
	TotalsAvailable bool         `json:"totals_available"`
	Error           string       `json:"error,omitempty"`
	Rows            []TrafficRow `json:"rows"`
	Source          string       `json:"source"`
	Caveat          string       `json:"caveat"`
}

func ParseCounters(b []byte) (map[string]uint64, error) {
	var v struct {
		NFTables []struct {
			Counter *struct {
				Name   string
				Family string
				Table  string
				Bytes  json.Number
			} `json:"counter"`
		} `json:"nftables"`
	}
	if e := json.Unmarshal(b, &v); e != nil {
		return nil, e
	}
	out := map[string]uint64{}
	for _, r := range v.NFTables {
		if r.Counter != nil {
			if (r.Counter.Family != "" && r.Counter.Family != "inet") || (r.Counter.Table != "" && r.Counter.Table != TableName) {
				continue
			}
			n, e := strconv.ParseUint(string(r.Counter.Bytes), 10, 64)
			if e != nil {
				return nil, e
			}
			if _, exists := out[r.Counter.Name]; exists {
				return nil, fmt.Errorf("duplicate named counter %s", r.Counter.Name)
			}
			out[r.Counter.Name] = n
		}
	}
	return out, nil
}

type Traffic struct {
	mu       sync.RWMutex
	runner   Runner
	prev     map[string]uint64
	totals   map[string]HostTotals
	at       time.Time
	snapshot TrafficSnapshot
}

func NewTraffic(r Runner) *Traffic {
	return &Traffic{runner: r, totals: map[string]HostTotals{}, snapshot: TrafficSnapshot{State: "off", Rows: []TrafficRow{}}}
}
func (t *Traffic) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prev = nil
	t.totals = map[string]HostTotals{}
	t.at = time.Time{}
	t.snapshot = TrafficSnapshot{State: "off", Rows: []TrafficRow{}}
}

// Rebase invalidates only the raw-counter generation. Completed diagnostic
// totals survive profile changes, host discovery and owned table recreation.
func (t *Traffic) Rebase() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prev = nil
	t.at = time.Time{}
}
func (t *Traffic) Off() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prev = nil
	t.totals = map[string]HostTotals{}
	t.at = time.Time{}
	t.snapshot = TrafficSnapshot{State: "off", Rows: []TrafficRow{}}
}
func (t *Traffic) Snapshot() TrafficSnapshot { t.mu.RLock(); defer t.mu.RUnlock(); return t.snapshot }
func (t *Traffic) Sample(ctx context.Context, hosts []Host) {
	t.sampleAt(ctx, hosts, time.Now())
}
func (t *Traffic) sampleAt(ctx context.Context, hosts []Host, now time.Time) {
	s := TrafficSnapshot{
		Enabled: true,
		State:   "warming_up",
		At:      now.UTC().Format(time.RFC3339),
		Rows:    []TrafficRow{},
		Source:  "LAN family rates and session totals=nft software-path counters",
		Caveat:  "Session totals are diagnostic only. They reset on monitoring disable/service restart, not profile changes. Counter gaps are not estimated. Includes router/local-proxy traffic; client-facing IP family is not the proxy WAN family. Flow/hardware offload can undercount bypassed traffic.",
	}

	b, e := t.runner.Run(ctx, "nft", []string{"-j", "list", "table", "inet", TableName}, nil)
	var counters map[string]uint64
	if e == nil {
		counters, e = ParseCounters(b)
	}
	s.TotalsAvailable = e == nil
	if e == nil {
		for _, h := range hosts {
			for _, suffix := range []string{"_4_up", "_4_down", "_6_up", "_6_down"} {
				name := counterPrefix(h.MAC) + suffix
				if _, ok := counters[name]; !ok {
					s.MissingCounters = append(s.MissingCounters, name)
				}
			}
		}
		if len(s.MissingCounters) > 0 {
			s.TotalsAvailable = false
			e = fmt.Errorf("missing %d named counters (%s); check the owned nft table and LAN discovery", len(s.MissingCounters), strings.Join(s.MissingCounters, ", "))
		}
	}
	if e != nil {
		s.Error = "traffic counters: " + e.Error()
		s.State = "unavailable"
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.totals == nil {
		t.totals = map[string]HostTotals{}
	}
	if !t.at.IsZero() {
		s.Interval = now.Sub(t.at).Seconds()
	}

	sampleFamily := func(prefix string, total *Totals) (Rates, bool) {
		u, uok := counters[prefix+"_up"]
		d, dok := counters[prefix+"_down"]
		pu, puok := t.prev[prefix+"_up"]
		pd, pdok := t.prev[prefix+"_down"]
		valid := uok && dok && puok && pdok && u >= pu && d >= pd && s.Interval > 0
		if !valid {
			return Rates{}, uok && dok
		}
		du, dd := u-pu, d-pd
		total.Up += du
		total.Down += dd
		return Rates{float64(du) / s.Interval, float64(dd) / s.Interval, true}, true
	}

	for _, h := range hosts {
		v := t.totals[h.MAC]
		p := counterPrefix(h.MAC)
		v4, a4 := sampleFamily(p+"_4", &v.V4)
		v6, a6 := sampleFamily(p+"_6", &v.V6)
		t.totals[h.MAC] = v

		all := v.V4.Up + v.V4.Down + v.V6.Up + v.V6.Down
		ratio := float64(0)
		if all > 0 {
			ratio = float64(v.V6.Up+v.V6.Down) / float64(all)
		}
		s.Rows = append(s.Rows, TrafficRow{Host: h, Totals: v, V4: v4, V6: v6, IPv6Ratio: ratio, V4Available: a4, V6Available: a6})
		if e == nil && v4.Valid && v6.Valid {
			s.State = "ready"
		}
		if e != nil && (a4 || a6) {
			s.State = "partial"
		}
	}
	t.prev = counters
	t.at = now
	t.snapshot = s
}
