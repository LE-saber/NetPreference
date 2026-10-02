package netpref

import (
	"context"
	"encoding/json"
	"strconv"
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
	Host      Host       `json:"host"`
	Totals    HostTotals `json:"totals"`
	V4        Rates      `json:"ipv4_rate"`
	V6        Rates      `json:"ipv6_rate"`
	IPv6Ratio float64    `json:"ipv6_ratio"`
}
type TrafficSnapshot struct {
	Enabled         bool         `json:"enabled"`
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
				Name  string
				Bytes json.Number
			} `json:"counter"`
		} `json:"nftables"`
	}
	if e := json.Unmarshal(b, &v); e != nil {
		return nil, e
	}
	out := map[string]uint64{}
	for _, r := range v.NFTables {
		if r.Counter != nil {
			n, e := strconv.ParseUint(string(r.Counter.Bytes), 10, 64)
			if e != nil {
				return nil, e
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
	return &Traffic{runner: r, totals: map[string]HostTotals{}}
}
func (t *Traffic) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prev = nil
	t.totals = map[string]HostTotals{}
	t.at = time.Time{}
	t.snapshot = TrafficSnapshot{Rows: []TrafficRow{}}
}
func (t *Traffic) Off() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prev = nil
	t.totals = map[string]HostTotals{}
	t.at = time.Time{}
	t.snapshot = TrafficSnapshot{Rows: []TrafficRow{}}
}
func (t *Traffic) Snapshot() TrafficSnapshot { t.mu.RLock(); defer t.mu.RUnlock(); return t.snapshot }
func (t *Traffic) Sample(ctx context.Context, hosts []Host) {
	now := time.Now()
	s := TrafficSnapshot{
		Enabled: true,
		At:      now.UTC().Format(time.RFC3339),
		Rows:    []TrafficRow{},
		Source:  "rates and session totals=nft software-path counters",
		Caveat:  "Session totals are diagnostic only and reset when monitoring/service counters reset. Flow/hardware offload can undercount traffic that bypasses the software path.",
	}

	b, e := t.runner.Run(ctx, "nft", []string{"-j", "list", "table", "inet", TableName}, nil)
	var counters map[string]uint64
	if e == nil {
		counters, e = ParseCounters(b)
	}
	s.TotalsAvailable = e == nil
	if e != nil {
		s.Error = "traffic counters: " + e.Error()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.totals == nil {
		t.totals = map[string]HostTotals{}
	}
	if !t.at.IsZero() {
		s.Interval = now.Sub(t.at).Seconds()
	}

	sampleFamily := func(prefix string, total *Totals) Rates {
		u, uok := counters[prefix+"_up"]
		d, dok := counters[prefix+"_down"]
		pu, puok := t.prev[prefix+"_up"]
		pd, pdok := t.prev[prefix+"_down"]
		valid := uok && dok && puok && pdok && u >= pu && d >= pd && s.Interval > 0
		if !valid {
			return Rates{}
		}
		du, dd := u-pu, d-pd
		total.Up += du
		total.Down += dd
		return Rates{float64(du) / s.Interval, float64(dd) / s.Interval, true}
	}

	for _, h := range hosts {
		v := t.totals[h.MAC]
		p := counterPrefix(h.MAC)
		v4 := sampleFamily(p+"_4", &v.V4)
		v6 := sampleFamily(p+"_6", &v.V6)
		t.totals[h.MAC] = v

		all := v.V4.Up + v.V4.Down + v.V6.Up + v.V6.Down
		ratio := float64(0)
		if all > 0 {
			ratio = float64(v.V6.Up+v.V6.Down) / float64(all)
		}
		s.Rows = append(s.Rows, TrafficRow{h, v, v4, v6, ratio})
	}
	t.prev = counters
	t.at = now
	t.snapshot = s
}
