package netpref

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNLBWColumnOrderAggregationAndErrors(t *testing.T) {
	b := []byte(`{"columns":["tx_bytes","mac","family","rx_bytes"],"data":[[10,"02:00:00:00:00:01",4,20],[30,"02:00:00:00:00:01",6,40],[5,"02:00:00:00:00:01",4,7]]}`)
	m, e := ParseNLBW(b)
	if e != nil {
		t.Fatal(e)
	}
	v := m["02:00:00:00:00:01"]
	if v.V4.Up != 15 || v.V4.Down != 27 || v.V6.Up != 30 || v.V6.Down != 40 {
		t.Fatal(v)
	}
	for _, s := range []string{`{}`, `{"columns":["mac","family","rx_bytes","tx_bytes"],"data":[[]]}`, `{"columns":["mac","family","rx_bytes","tx_bytes"],"data":[["02:00:00:00:00:01",4,-1,3]]}`} {
		if _, e := ParseNLBW([]byte(s)); e == nil {
			t.Fatal("bad data accepted", s)
		}
	}
}
func TestTrafficSamplingResetOffAndReadOnly(t *testing.T) {
	f := &fakeRunner{nlbw: `{"columns":["family","mac","rx_bytes","tx_bytes"],"data":[[4,"02:00:00:00:00:01",100,200],[6,"02:00:00:00:00:01",300,400]]}`}
	traffic := NewTraffic(f)
	hosts := []Host{{MAC: "02:00:00:00:00:01"}}
	set := func(v int) {
		f.counters = fmt.Sprintf(`{"nftables":[{"counter":{"name":"m020000000001_4_up","bytes":%d}},{"counter":{"name":"m020000000001_4_down","bytes":%d}},{"counter":{"name":"m020000000001_6_up","bytes":%d}},{"counter":{"name":"m020000000001_6_down","bytes":%d}}]}`, v, v*2, v*3, v*4)
	}
	set(100)
	traffic.Sample(context.Background(), hosts)
	s := traffic.Snapshot()
	if s.Rows[0].V4.Valid || !s.TotalsAvailable {
		t.Fatal(s)
	}
	time.Sleep(5 * time.Millisecond)
	set(200)
	traffic.Sample(context.Background(), hosts)
	s = traffic.Snapshot()
	if !s.Rows[0].V4.Valid || !s.Rows[0].V6.Valid || s.Rows[0].V4.Up <= 0 || s.Rows[0].IPv6Ratio != 0.7 {
		t.Fatal(s)
	}
	set(1)
	traffic.Sample(context.Background(), hosts)
	if traffic.Snapshot().Rows[0].V4.Valid {
		t.Fatal("counter reset not invalidated")
	}
	traffic.Reset()
	set(2)
	traffic.Sample(context.Background(), hosts)
	if traffic.Snapshot().Rows[0].V6.Valid {
		t.Fatal("explicit reset not invalidated")
	}
	before := len(f.calls)
	traffic.Off()
	if traffic.Snapshot().Enabled || len(f.calls) != before {
		t.Fatal("off performed external calls")
	}
	for _, c := range f.calls {
		if c.name == "nlbw" && fmt.Sprint(c.args) != "[-c json -g mac,family]" {
			t.Fatal("nlbw mutated", c)
		}
	}
}
