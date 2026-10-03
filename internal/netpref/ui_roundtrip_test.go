package netpref

import (
	"os"
	"testing"
)

// The input is emitted by the real front-end model during its contract test,
// then validated by the CLI. CI runs this explicitly after generating it.
func TestGeneratedUILibraryPreference(t *testing.T) {
	path := os.Getenv("NETPREFERENCE_UI_CONFIG")
	if path == "" {
		t.Skip("run after Node contract with NETPREFERENCE_UI_CONFIG=dist/ux-generated.uci")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseUCI(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	d := &c.Devices[0]
	if d.Mode != "ipv4" || d.WaitMS != 120 || d.DelayMS != 80 {
		t.Fatal("device defaults mutated", d)
	}
	for _, tc := range []struct {
		name, mode string
		a, b       int
		probe      bool
	}{
		{"openai.com", "ipv6", 120, 80, true}, {"api.openai.com", "ipv6", 120, 80, true},
		{"deep.api.openai.com", "ipv6", 120, 80, true}, {"notopenai.com", "ipv4", 120, 80, true},
		{"youtube.com", "ipv4", 0, 160, true}, {"www.youtube.com", "ipv4", 0, 160, true},
		{"games.test", "custom", 40, 90, false}, {"a.games.test", "custom", 40, 90, false},
		{"example.com", "dual", 120, 80, true}, {"sub.example.com", "dual", 120, 80, true},
		{"unmatched.test", "ipv4", 120, 80, true},
	} {
		p := ResolvePreference(&c, d, tc.name)
		if p.Mode != tc.mode || p.WaitMS != tc.a || p.DelayMS != tc.b || p.Probe != tc.probe {
			t.Errorf("%s: %+v", tc.name, p)
		}
	}
	if r := MatchRule(&c, d, Question{Name: "bad.test", Type: TypeA}); r == nil || r.Action != "nxdomain" {
		t.Fatal("legacy DNS action lost", r)
	}
}
