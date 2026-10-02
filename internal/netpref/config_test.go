package netpref

import (
	"strings"
	"testing"
)

const testMAC = "02:00:00:00:00:01"
const otherMAC = "02:00:00:00:00:02"

func testConfig() Config {
	c := DefaultConfig()
	c.Enabled = true
	d := DefaultDevice()
	d.ID = "workstation"
	d.MAC = testMAC
	c.Devices = []Device{d}
	return c
}

const testUCI = `package netpreference
config global 'main'
 option enabled '0'
 option upstream '127.0.0.1:53'
 list interface 'br-lan'
config device 'workstation'
 option mac '02:00:00:00:00:01'
 option name 'Work PC'
 option mode 'ipv6'
 option wait_ms '120'
 option delay_ms '80'
`

func TestUCIAndDefaults(t *testing.T) {
	c, e := ParseUCI(testUCI)
	if e != nil {
		t.Fatal(e)
	}
	if c.Enabled || c.Devices[0].Name != "Work PC" || !c.Devices[0].Probe || c.TimeoutMS != 2000 {
		t.Fatalf("unexpected configuration %+v", c)
	}
}
func TestUCINoShellEvaluation(t *testing.T) {
	tokens, e := UCITokens(`option name 'one'\''two $(touch /tmp/never)'`)
	if e != nil || len(tokens) != 3 || tokens[2] != "one'two $(touch /tmp/never)" {
		t.Fatalf("%q %v", tokens, e)
	}
	bad := strings.Replace(testUCI, "br-lan", "br-lan; flush ruleset", 1)
	if _, e = ParseUCI(bad); e == nil {
		t.Fatal("nft injection accepted")
	}
}
func TestConfigRejectsUnsafeInputs(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"infinite", func(c *Config) { c.Devices[0].WaitMS = -1 }},
		{"excess-total", func(c *Config) { c.Devices[0].WaitMS = 750; c.Devices[0].DelayMS = 500 }},
		{"loop", func(c *Config) { c.Upstream = "127.0.0.1:1053" }},
		{"hostname", func(c *Config) { c.Upstream = "dns.example:53" }},
		{"bad-mac", func(c *Config) { c.Devices[0].MAC = "ff:ff:ff:ff:ff:ff" }},
		{"duplicate", func(c *Config) { d := c.Devices[0]; d.ID = "second"; c.Devices = append(c.Devices, d) }},
		{"bad-mode", func(c *Config) { c.Devices[0].Mode = "magic" }},
		{"foreign-rule", func(c *Config) { c.Rules = []Rule{{ID: "r", Device: otherMAC, Domain: "*", Action: "nodata"}} }},
		{"bad-domain", func(c *Config) { c.Rules = []Rule{{ID: "r", Domain: "evil.com; flush ruleset", Action: "nodata"}} }},
		{"wrong-family", func(c *Config) {
			c.Rules = []Rule{{ID: "r", Domain: "example.com", Action: "rewrite", IPv4: []string{"::1"}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig()
			tc.mut(&c)
			if e := c.Validate(); e == nil {
				t.Fatal("accepted unsafe config")
			}
		})
	}
}
func TestMalformedUCI(t *testing.T) {
	for _, s := range []string{"", "config global main\n option monitor yes", "config global main\nconfig global main", "config global other", "config global main\n option name 'unterminated"} {
		if _, e := ParseUCI(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestEndpoints(t *testing.T) {
	for _, s := range []string{"127.0.0.1:53", "udp://9.9.9.9:53", "tcp://[::1]:6053"} {
		if _, _, e := Endpoint(s); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{"http://8.8.8.8:53", "0.0.0.0:53", "[ff02::1]:53", "[fe80::1%br-lan]:53"} {
		if _, _, e := Endpoint(s); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
}
func FuzzUCITokens(f *testing.F) {
	f.Add(testUCI)
	f.Add("option name 'a'\\''b'")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 65536 {
			return
		}
		_, _ = UCITokens(s)
		_, _ = ParseUCI(s)
	})
}
