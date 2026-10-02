# Architecture and safety decisions

## 1. An additive per-client layer, not a replacement resolver

Only enrolled MAC+source-IP identities on explicitly trusted ingress interfaces
are redirected, for both IPv4/IPv6 and UDP/TCP port 53. NetPreference listens on
1053 and defaults to forwarding to 127.0.0.1:53. Thus dnsmasq -> HomeProxy:5333 ->
SmartDNS:6053 remains the original upstream chain. Router-originated output is
never intercepted. No DNS64/NAT64, Jool, TAYGA, NAT66/masq6 change, shared firewall
include, global dnsmasq override or nlbwmon configuration modification is made.

The standalone `inet netpreference` table is marked with the exact ownership
comment `NetPreference owned v1`. A foreign table of that name causes an explicit
error, never replacement. Apply uses one checked nft transaction, not a sequence
that temporarily deletes unrelated rules. Interception priority is -105, ahead
of conventional destination NAT at -100; site-specific earlier TPROXY/DNS hooks
must be checked on the actual router. This is not a claim of universal HomeProxy
hook compatibility. Default br-lan must permit LAN input to the router, including
1053. Guest zones with restrictive fw4 INPUT rules need a deliberate administrator
policy; this MVP will not add a broad firewall ACCEPT rule behind the user's back.

MAC identity comes from `ip -j neigh show` on trusted interfaces and subnet-checked
DHCP leases, including IPv6 privacy addresses. Exact MAC+IP tuples are placed in
nft concatenation sets. A newly changed address remains on the original DNS path
until discovery (nominally 3 seconds), rather than being redirected as an unknown
client and losing DNS. Routed clients hidden behind another router's MAC cannot
be separated. MAC randomization requires updating enrollment. This is an
administrative convenience, not cryptographic device identity/access control.

## 2. Bounded preference algorithm

A is a maximum preferred-address evidence wait from the nonpreferred query's
arrival, not an artificial delay added after every upstream lookup. B is an
additional nonpreferred reply delay only if a preferred A/AAAA address was
positively observed. A <= 750 ms, B <= 500 ms, A+B <= 1000 ms. Defaults 120/80;
presets 75/40, 120/80, 250/150. IPv4 mode is symmetric. Missing/negative preferred
records release the original reply without B; exhausted A also releases it.
Non-address, negative and error replies are not deliberately delayed. Valid IPv4
answers are never removed by a preference mode. Explicit domain family-filter
rules are separate, stronger administrator choices.

Optional active probing asks for the preferred record type even if the client
only asks for the other. Probes obey device/domain overrides and upstream policy,
are deduplicated, have a 32-inflight cap, and use an existence cache capped at 2048
entries/10 seconds. A probe discovers DNS data, not network reachability, and
cannot force an application to issue AAAA or connect over IPv6. Without probes,
parallel/recent same-device/name/upstream DNS answers provide the evidence.

Happy Eyeballs, client connection racing, application caches and HTTPS/SVCB hints
can defeat a DNS timing preference. IPv6 correctness still depends on the user's
existing connectivity. Infinite waits and deleting all A records by default were
rejected because they jeopardize stable IPv4. This plugin offers a measurable
bias, not a connection-family guarantee or a bandwidth limiter.

## 3. Policy ordering and DNS protocol

Whole-device REFUSED is first. Enrolled device-specific domain rules beat global
rules; longest/exact domain match then wins; ties retain UCI order. `*.example.com`
includes its apex, never `notexample.com`. Global means **all enrolled devices**,
not all LAN clients. Supported rules: forward/upstream override, A/AAAA static
rewrite, NXDOMAIN, NODATA, REFUSED, sinkhole 0.0.0.0/::, and A-only/AAAA-only filters.
Rules can target A, AAAA, HTTPS, SVCB or all query types. Enter IDNs as punycode.

Forwarding validates transaction ID and the complete question, uses bounded
connected UDP/TCP sockets, retries a truncated UDP response over TCP, and handles
TCP pipelining concurrently so an earlier A query does not block a later AAAA.
Client UDP response sizes honor EDNS (up to 4096 bytes), otherwise set TC for a
client TCP retry. Malformed compression/lengths, multiple questions and unsupported
transfer/authenticated-query forms are rejected. Limits: 256 global active slots,
32 per source IP, 64 messages per TCP connection, finite idle/query deadlines.
No general-purpose persistent DNS cache is added; original DNS caches remain.

Transparent answers retain DNSSEC flags. Local rewrites/block answers never claim
AD/authentication or authoritative ownership. Negative policy answers have no SOA
and are not given a fabricated negative-caching TTL. The TTL form field applies to
positive static/sinkhole addresses. Strict end-client DNSSEC validation can reject
local overrides; this layer cannot generate valid signatures for somebody else's
zone. DNS cookies/unknown additional records are passed through on normal
forwarding, not synthesized for local policy. DoH/DoT, VPN DNS and non-53 DNS are
outside scope; the plugin neither decrypts nor silently blocks them.

Per-device/rule upstreams accept literal IP:port, optionally tcp:// or udp://.
Unspecified/multicast/hostname/zoned endpoints and port1053 are rejected. Transport
failure, SERVFAIL or REFUSED at an override falls back to the original global
upstream. This intentional availability preference is not strict resolver
isolation. A local block/rewrite is never undone by that upstream fallback.

## 4. Lifecycle, rollback and failure boundaries

Installation starts two idle procd instances but changes no client's DNS policy.
Explicit Apply validates desired UCI, LAN interfaces, original DNS health, live
UDP/TCP listener health and table ownership before first-use baseline capture and
nft activation. A root-private baseline records relevant original configuration
contents and hashes plus existing-component presence. It is forensic evidence;
Restore **never writes those snapshots over later user changes**.

Only `/etc/config/netpreference` is committed by the UI. The view deliberately
avoids global `uci.apply()`, which could apply unrelated pending network changes.
Successful activation persists only the plugin's own enabled flag. A persistence
failure rolls back owned routing, attempts its own enabled-flag rollback and
retains a private boot-inhibit marker when activation was previously disabled.

Restore disables the engine, removes only its marked table, clears only matching
DNS redirect conntrack entries, writes an inhibit marker and its own enabled=0.
Deletion filters include original dport53, reply source port1053 and a local reply
source address, both IPv4/IPv6 and UDP/TCP; never `conntrack -F`. A foreign or absent
ownership table is not proof to delete third-party flows. Stop/deactivate preserves
the desired boot setting; explicit Restore/removal disables it. Package upgrade
uses deactivate rather than erasing that desired setting. prerm refuses to remove
the recovery binary if recovery fails. Baseline evidence is retained on uninstall.
No existing HomeProxy/SmartDNS/dnsmasq/nlbwmon package/service/database is removed.

The manager renews a 15-second nft selected-MAC lease. Upstream/self-health failure
clears the lease and routes subsequent DNS on the original path, while the engine
forwards surviving redirected sessions unchanged. A separate procd watchdog tests
the control heartbeat and actual UDP/TCP listener and removes owned redirects on
daemon failure. procd respawn and retry restore healthy desired operation. A
firewall reload that removes the separate table is reconciled by the manager.

This is layered fail-open recovery, **not zero-interruption or unconditional DNS
availability**. Cleanup depends on working nft/conntrack and kernel privileges;
a lost in-flight query may need retry. Killing both daemon and watchdog leaves
existing conntrack DNAT sessions until conntrack expiry even after the 15-second
lease prevents new redirects. An externally deleted ownership table also removes
our immediate ownership proof for targeted conntrack cleanup. Under those rare
conditions use the documented recovery checks, never a blanket conntrack flush.
The shared upstream failing cannot be repaired by a DNS preference plugin.

## 5. Observation rather than invasive accounting

Optional software-path nft forward counters provide v4/v6 upload/download byte
rates from consecutive 3-second samples. MAC matches identify upload; discovered
destination-IP sets identify download. Counter resets/first samples are invalid,
not negative or imaginary rates. Only routed traffic is counted, not LAN-to-LAN
bridged traffic or all traffic to the router itself. Flow/hardware offload can
bypass these counters; offload is never changed automatically.

Cumulative totals read `nlbw -c json -g mac,family`, summing rx_bytes as download
and tx_bytes as upload. Totals refer to nlbwmon's **active accounting period**, not
lifetime totals, and are limited by its existing collection/configuration. No
`nlbw commit`, reset, enable, disable or package removal is done. When monitoring
is off, this plugin performs no nlbw sampling and installs no traffic counters;
normal DNS/neighbor/watchdog functions remain. Missing nlbwmon shows unavailable
totals rather than invented zero coverage. IPv6 share uses these period totals.

## 6. Verification and primary references

See VERIFICATION.md and the exact CI artifact logs for implemented vs tested
boundaries. Mock/API-contract tests are not independent review or target-router
acceptance. No outside implementation agent was available; Pro wrote and
integrated core, tests and UI, with tools performing actual builds/execution.

- DNS and Happy Eyeballs: https://www.rfc-editor.org/rfc/rfc8305.html
- TCP pipelining/reordering: https://www.rfc-editor.org/rfc/rfc7766.html
- nlbwmon fields/client: https://github.com/jow-/nlbwmon/blob/master/client.c
- fw4 generated-table model: https://github.com/openwrt/firewall4
- conntrack selectors: https://netfilter.org/projects/conntrack-tools/conntrack-manpage.html
- rpcd executable plugins: https://openwrt.org/docs/techref/rpcd
- LuCI scoped save vs global apply: https://github.com/openwrt/luci/blob/openwrt-24.10/modules/luci-base/htdocs/luci-static/resources/uci.js
- OpenWrt IPK archive layout: https://github.com/openwrt/openwrt/blob/openwrt-24.10/scripts/ipkg-build
- OpenWrt Go packaging: https://github.com/openwrt/packages/blob/openwrt-24.10/lang/golang/golang-package.mk
