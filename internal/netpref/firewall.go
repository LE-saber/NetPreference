package netpref

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

const TableName = "netpreference"
const OwnerComment = "NetPreference owned v1"

func counterPrefix(mac string) string { return "m" + strings.ReplaceAll(mac, ":", "") }
func quoteList(items []string) string {
	v := make([]string, len(items))
	for i, s := range items {
		v[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(v, ", ")
}
func selected(c *Config) []string {
	var v []string
	if c.Enabled {
		for _, d := range c.Devices {
			if d.Enabled {
				v = append(v, d.MAC)
			}
		}
	}
	sort.Strings(v)
	return v
}
func nftSet(b *strings.Builder, name, typ string, values []string, timed bool) {
	fmt.Fprintf(b, " set %s { type %s; ", name, typ)
	if timed {
		b.WriteString("flags timeout; timeout 15s; ")
	}
	if len(values) > 0 {
		fmt.Fprintf(b, "elements = { %s }; ", strings.Join(values, ", "))
	}
	b.WriteString("}\n")
}

// Bind neighbour identity to the original ingress MAC. A new privacy address
// keeps the original DNS path until discovery, rather than being REFUSED.
func identityPairs(c *Config, hosts []Host, v6 bool) []string {
	allowed := map[string]bool{}
	for _, mac := range selected(c) {
		allowed[mac] = true
	}
	var pairs []string
	for _, h := range hosts {
		if !allowed[h.MAC] {
			continue
		}
		ips := h.IPv4
		if v6 {
			ips = h.IPv6
		}
		for _, ip := range ips {
			pairs = append(pairs, h.MAC+" . "+ip)
		}
	}
	sort.Strings(pairs)
	return pairs
}
func BuildNFT(c *Config, hosts []Host, replace bool) string {
	var b strings.Builder
	if replace {
		b.WriteString("delete table inet netpreference\n")
	}
	fmt.Fprintf(&b, "table inet netpreference {\n comment %q\n", OwnerComment)
	nftSet(&b, "np_devices", "ether_addr", selected(c), true)
	nftSet(&b, "clients4", "ether_addr . ipv4_addr", identityPairs(c, hosts, false), false)
	nftSet(&b, "clients6", "ether_addr . ipv6_addr", identityPairs(c, hosts, true), false)
	if c.Monitor {
		for _, h := range hosts {
			p := counterPrefix(h.MAC)
			nftSet(&b, p+"_v4", "ipv4_addr", h.IPv4, false)
			nftSet(&b, p+"_v6", "ipv6_addr", h.IPv6, false)
			for _, fam := range []string{"4", "6"} {
				for _, dir := range []string{"up", "down"} {
					fmt.Fprintf(&b, " counter %s_%s_%s { }\n", p, fam, dir)
				}
			}
		}
	}
	fmt.Fprintf(&b, " chain dns_redirect { type nat hook prerouting priority -105; policy accept;\n iifname { %s } ether saddr @np_devices ether saddr . ip saddr @clients4 meta l4proto { tcp, udp } th dport 53 redirect to :1053\n iifname { %s } ether saddr @np_devices ether saddr . ip6 saddr @clients6 meta l4proto { tcp, udp } th dport 53 redirect to :1053\n }\n", quoteList(c.Interfaces), quoteList(c.Interfaces))
	trusted := append([]string{"lo"}, c.Interfaces...)
	fmt.Fprintf(&b, " chain listener_guard { type filter hook input priority -5; policy accept;\n iifname != { %s } meta l4proto { tcp, udp } th dport 1053 drop\n }\n", quoteList(trusted))
	if c.Monitor {
		b.WriteString(" chain observe { type filter hook forward priority -10; policy accept;\n")
		for _, h := range hosts {
			p := counterPrefix(h.MAC)
			for _, f := range []struct{ id, proto string }{{"4", "ip"}, {"6", "ip6"}} {
				fmt.Fprintf(&b, " iifname { %s } ether saddr %s meta nfproto ipv%s counter name %s_%s_up\n", quoteList(c.Interfaces), h.MAC, f.id, p, f.id)
				fmt.Fprintf(&b, " oifname { %s } %s daddr @%s_v%s counter name %s_%s_down\n", quoteList(c.Interfaces), f.proto, p, f.id, p, f.id)
			}
		}
		b.WriteString(" }\n")
	}
	b.WriteString("}\n")
	return b.String()
}
func RefreshNFT(c *Config, hosts []Host, healthy bool) string {
	var b strings.Builder
	b.WriteString("flush set inet netpreference np_devices\n")
	if healthy {
		if macs := selected(c); len(macs) > 0 {
			fmt.Fprintf(&b, "add element inet netpreference np_devices { %s }\n", strings.Join(macs, ", "))
		}
	}
	for _, fam := range []struct {
		name string
		v6   bool
	}{{"clients4", false}, {"clients6", true}} {
		fmt.Fprintf(&b, "flush set inet netpreference %s\n", fam.name)
		if pairs := identityPairs(c, hosts, fam.v6); len(pairs) > 0 {
			fmt.Fprintf(&b, "add element inet netpreference %s { %s }\n", fam.name, strings.Join(pairs, ", "))
		}
	}
	if c.Monitor {
		for _, h := range hosts {
			p := counterPrefix(h.MAC)
			for _, f := range []struct {
				id  string
				ips []string
			}{{"4", h.IPv4}, {"6", h.IPv6}} {
				fmt.Fprintf(&b, "flush set inet netpreference %s_v%s\n", p, f.id)
				if len(f.ips) > 0 {
					fmt.Fprintf(&b, "add element inet netpreference %s_v%s { %s }\n", p, f.id, strings.Join(f.ips, ", "))
				}
			}
		}
	}
	return b.String()
}

type Firewall struct{ Runner Runner }

// Inspect must distinguish absence from permission/syntax failure. Never treat
// an arbitrary nft command error as permission to overwrite an existing table.
func (f Firewall) Inspect(ctx context.Context) (exists bool, err error) {
	b, e := f.Runner.Run(ctx, "nft", []string{"-j", "list", "tables"}, nil)
	if e != nil {
		return false, e
	}
	var tables struct {
		NFTables []struct {
			Table *struct{ Family, Name string } `json:"table"`
		} `json:"nftables"`
	}
	if e = json.Unmarshal(b, &tables); e != nil {
		return false, e
	}
	for _, v := range tables.NFTables {
		if v.Table != nil && v.Table.Family == "inet" && v.Table.Name == TableName {
			exists = true
		}
	}
	if !exists {
		return false, nil
	}
	b, e = f.Runner.Run(ctx, "nft", []string{"-j", "list", "table", "inet", TableName}, nil)
	if e != nil {
		return true, e
	}
	var details struct {
		NFTables []struct {
			Table *struct{ Family, Name, Comment string } `json:"table"`
		} `json:"nftables"`
	}
	if e = json.Unmarshal(b, &details); e != nil {
		return true, e
	}
	for _, v := range details.NFTables {
		if v.Table != nil && v.Table.Family == "inet" && v.Table.Name == TableName && v.Table.Comment == OwnerComment {
			return true, nil
		}
	}
	return true, errors.New("refusing to change foreign inet/netpreference table: ownership marker missing")
}
func (f Firewall) Apply(ctx context.Context, c *Config, hosts []Host) error {
	exists, e := f.Inspect(ctx)
	if e != nil {
		return e
	}
	batch := []byte(BuildNFT(c, hosts, exists))
	if _, e = f.Runner.Run(ctx, "nft", []string{"-c", "-f", "-"}, batch); e != nil {
		return e
	}
	_, e = f.Runner.Run(ctx, "nft", []string{"-f", "-"}, batch)
	return e
}
func (f Firewall) Refresh(ctx context.Context, c *Config, hosts []Host, healthy bool) error {
	exists, e := f.Inspect(ctx)
	if e != nil {
		return e
	}
	if !exists {
		return errors.New("owned table missing")
	}
	_, e = f.Runner.Run(ctx, "nft", []string{"-f", "-"}, []byte(RefreshNFT(c, hosts, healthy)))
	return e
}
func (f Firewall) Remove(ctx context.Context) error {
	exists, err := f.Inspect(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	} // no ownership proof: never delete third-party flows
	if _, err = f.Runner.Run(ctx, "nft", []string{"delete", "table", "inet", TableName}, nil); err != nil {
		return err
	}
	return f.CleanConntrack(ctx)
}
func (f Firewall) CleanConntrack(ctx context.Context) error {
	// In addition to both DNS/proxy ports, constrain reply source to a local
	// address. A user DNAT to a remote host:1053 is NOT one of our connections.
	interfaces, err := net.Interfaces()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, in := range interfaces {
		addrs, err := in.Addrs()
		if err != nil {
			return err
		}
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil || seen[ip.String()] {
				continue
			}
			seen[ip.String()] = true
			fam := "ipv6"
			if ip.To4() != nil {
				fam = "ipv4"
			}
			for _, proto := range []string{"udp", "tcp"} {
				out, err := f.Runner.Run(ctx, "conntrack", []string{"-D", "-f", fam, "-p", proto, "--dport", "53", "--reply-src", ip.String(), "--reply-port-src", "1053"}, nil)
				if err != nil && !strings.Contains(string(out), "0 flow entries") {
					return err
				}
			}
		}
	}
	return nil
}
func sameHosts(a, b []Host) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].MAC != b[i].MAC {
			return false
		}
	}
	return true
}
