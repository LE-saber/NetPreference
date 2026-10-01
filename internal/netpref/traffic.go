package netpref

import (
	"context"
	"encoding/json"
	"fmt"
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

func ParseNLBW(b []byte) (map[string]HostTotals, error) {
	var v struct {
		Columns []string            `json:"columns"`
		Data    [][]json.RawMessage `json:"data"`
	}
	if e := json.Unmarshal(b, &v); e != nil {
		return nil, e
	}
	ix := map[string]int{}
	for i, k := range v.Columns {
		ix[k] = i
	}
	for _, k := range []string{"family", "mac", "rx_bytes", "tx_bytes"} {
		if _, ok := ix[k]; !ok {
			return nil, fmt.Errorf("nlbw missing %s column", k)
		}
	}
	out := map[string]HostTotals{}
	for _, row := range v.Data {
		if len(row) != len(v.Columns) {
			return nil, fmt.Errorf("nlbw row length mismatch")
		}
		var mac string
		var fam int
		var rx, tx uint64
		if e := json.Unmarshal(row[ix["mac"]], &mac); e != nil {
			return nil, e
		}
		if e := json.Unmarshal(row[ix["family"]], &fam); e != nil {
			return nil, e
		}
		if e := json.Unmarshal(row[ix["rx_bytes"]], &rx); e != nil {
			return nil, e
		}
		if e := json.Unmarshal(row[ix["tx_bytes"]], &tx); e != nil {
			return nil, e
		}
		m, e := NormalizeMAC(mac)
		if e != nil {
			continue
		}
		h := out[m]
		switch fam {
		case 4:
			h.V4.Up += tx
			h.V4.Down += rx
		case 6:
			h.V6.Up += tx
			h.V6.Down += rx
		default:
			continue
		}
		out[m] = h
	}
	return out, nil
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
	at       time.Time
	snapshot TrafficSnapshot
}

func NewTraffic(r Runner) *Traffic { return &Traffic{runner: r} }
func (t *Traffic) Reset()          { t.mu.Lock(); defer t.mu.Unlock(); t.prev = nil; t.at = time.Time{} }
func (t *Traffic) Off() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prev = nil
	t.at = time.Time{}
	t.snapshot = TrafficSnapshot{Rows: []TrafficRow{}}
}
func (t *Traffic) Snapshot() TrafficSnapshot { t.mu.RLock(); defer t.mu.RUnlock(); return t.snapshot }
func (t *Traffic) Sample(ctx context.Context, hosts []Host) {
	now := time.Now()
	s := TrafficSnapshot{Enabled: true, At: now.UTC().Format(time.RFC3339), Rows: []TrafficRow{}, Source: "rates=nft software path; totals=nlbwmon active accounting period", Caveat: "Flow/hardware offload can undercount live rates. nlbwmon coverage and polling settings remain unchanged; totals are not lifetime totals."}
	var totals map[string]HostTotals
	b, e := t.runner.Run(ctx, "nlbw", []string{"-c", "json", "-g", "mac,family"}, nil)
	if e == nil {
		totals, e = ParseNLBW(b)
	}
	s.TotalsAvailable = e == nil
	if e != nil {
		s.Error = "nlbwmon: " + e.Error()
	}
	b, e = t.runner.Run(ctx, "nft", []string{"-j", "list", "table", "inet", TableName}, nil)
	var counters map[string]uint64
	if e == nil {
		counters, e = ParseCounters(b)
	}
	if e != nil {
		s.Error += "; live counters: " + e.Error()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.at.IsZero() {
		s.Interval = now.Sub(t.at).Seconds()
	}
	rate := func(p string) Rates {
		u, uok := counters[p+"_up"]
		d, dok := counters[p+"_down"]
		pu, puok := t.prev[p+"_up"]
		pd, pdok := t.prev[p+"_down"]
		valid := uok && dok && puok && pdok && u >= pu && d >= pd && s.Interval > 0
		if !valid {
			return Rates{}
		}
		return Rates{float64(u-pu) / s.Interval, float64(d-pd) / s.Interval, true}
	}
	for _, h := range hosts {
		v := totals[h.MAC]
		all := v.V4.Up + v.V4.Down + v.V6.Up + v.V6.Down
		ratio := float64(0)
		if all > 0 {
			ratio = float64(v.V6.Up+v.V6.Down) / float64(all)
		}
		p := counterPrefix(h.MAC)
		s.Rows = append(s.Rows, TrafficRow{h, v, rate(p + "_4"), rate(p + "_6"), ratio})
	}
	t.prev = counters
	t.at = now
	t.snapshot = s
}
