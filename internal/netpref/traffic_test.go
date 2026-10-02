package netpref

import (
	"context"
	"testing"
	"time"
)

func TestTrafficSamplingResetOffAndReadOnly(t *testing.T) {
	f := &fakeRunner{}
	traffic := NewTraffic(f)
	hosts := []Host{{MAC: "02:00:00:00:00:01"}}
	set := func(v int) {
		f.counters = fmt.Sprintf(`{"nftables":[{"counter":{"name":"m020000000001_4_up","bytes":%d}},{"counter":{"name":"m020000000001_4_down","bytes":%d}},{"counter":{"name":"m020000000001_6_up","bytes":%d}},{"counter":{"name":"m020000000001_6_down","bytes":%d}}]}`, v, v*2, v*3, v*4)
	}
	set(100)
	traffic.Sample(context.Background(), hosts)
	s := traffic.Snapshot()
	if s.Rows[0].V4.Valid || !s.TotalsAvailable || s.Rows[0].Totals.V4.Up != 0 || s.Rows[0].Totals.V6.Down != 0 {
		t.Fatal(s)
	}
	time.Sleep(5 * time.Millisecond)
	set(200)
	traffic.Sample(context.Background(), hosts)
	s = traffic.Snapshot()
	if !s.Rows[0].V4.Valid || !s.Rows[0].V6.Valid || s.Rows[0].V4.Up <= 0 || s.Rows[0].IPv6Ratio != 0.7 {
		t.Fatal(s)
	}
	if s.Rows[0].Totals.V4.Up != 100 || s.Rows[0].Totals.V4.Down != 200 || s.Rows[0].Totals.V6.Up != 300 || s.Rows[0].Totals.V6.Down != 400 {
		t.Fatal("unexpected session totals", s.Rows[0].Totals)
	}
	time.Sleep(5 * time.Millisecond)
	set(250)
	traffic.Sample(context.Background(), hosts)
	s = traffic.Snapshot()
	if s.Rows[0].Totals.V4.Up != 150 || s.Rows[0].Totals.V4.Down != 300 || s.Rows[0].Totals.V6.Up != 450 || s.Rows[0].Totals.V6.Down != 600 {
		t.Fatal("session totals did not accumulate nft deltas", s.Rows[0].Totals)
	}
	set(1)
	traffic.Sample(context.Background(), hosts)
	s = traffic.Snapshot()
	if s.Rows[0].V4.Valid {
		t.Fatal("counter reset not invalidated")
	}
	if s.Rows[0].Totals.V4.Up != 150 || s.Rows[0].Totals.V6.Down != 600 {
		t.Fatal("counter reset should not erase prior diagnostic totals", s.Rows[0].Totals)
	}
	traffic.Reset()
	set(2)
	traffic.Sample(context.Background(), hosts)
	s = traffic.Snapshot()
	if s.Rows[0].V6.Valid || s.Rows[0].Totals.V4.Up != 0 || s.Rows[0].Totals.V6.Down != 0 {
		t.Fatal("explicit reset must clear diagnostic totals and rate baseline", s)
	}
	before := len(f.calls)
	traffic.Off()
	if traffic.Snapshot().Enabled || len(f.calls) != before {
		t.Fatal("off performed external calls")
	}
	for _, c := range f.calls {
		if c.name == "nlbw" {
			t.Fatal("traffic monitoring must not depend on nlbwmon", c)
		}
	}
}
