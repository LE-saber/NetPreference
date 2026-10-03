# 0.2.0-r2: usable configuration and observable traffic

## 1. Configure without internal IDs

1. In the domain-sets tab, add a name (for example AI services) and paste
   `openai.com`, `anthropic.com`, `claude.ai`, separated by newlines, whitespace,
   or commas. New inputs include the apex and all subdomains. Capitalization,
   trailing dots and pasted HTTP(S) URLs are normalized. IDNs use punycode.
2. In mode sets, create Daily and add unnamed rows. Choose a saved set or enter
   a domain, then choose dual stack, IPv6 first, IPv4 first or custom A/B.
   Standard modes keep optional timing overrides collapsed; custom opens them.
3. Save, then select Daily on the device page. Unmatched names use that device's
   own global mode/A/B. Blank timings inherit; zero is an intentional value.
4. More-specific domain matches still win. Equal specificity uses row order;
   arrows reorder rows. DNS actions remain separate and retain precedence.
   Existing DNS actions can be edited inside the collapsed compatibility area.
5. Renaming a library item keeps its references. Deleting a referenced set or
   profile is blocked with a readable error. No manual stable IDs are required.

The editor preflights the complete staged UCI config with a bounded, read-only
`check_config` RPC before scoped UCI save/commit. Bad A+B, references or values
cannot be committed by these views. It never executes UCI text as shell code.
Unchanged legacy exact patterns keep their old semantics; use the visible
include-subdomains button to opt in. This avoids silently broadening old blocks.

## 2. Traffic investigation and repair

Compared immutable r6 `38a69cb` with r1 `8b03a9a`: traffic.go, firewall.go and
platform.go were identical, and runtime's previous delta was validation counts.
No on-router nft/RPC diagnostic dump was supplied. Therefore a uniquely
r1-introduced arithmetic bug or the user's single exact root cause is NOT proven.
The following failures were actually reproduced and repaired:

- Apply advertised an enabled monitor as off before its first tick.
- Missing named counters looked like available zero totals instead of errors.
- Profile-only Apply recreated the table and cleared diagnostic session totals.
- DNS health failure returned before traffic sampling and left stale values.

FORWARD-only observation also excludes local proxy INPUT/OUTPUT paths. r2 uses
counter-only LAN PREROUTING (-310) upload and POSTROUTING (310) download chains,
scoped to trusted interfaces and discovered device MAC/IP identities. Existing
DNS redirect, expiring activation sets, watchdog, conntrack and ownership checks
are retained. No mark, routing, NAT66, proxy or shared UCI settings are changed.

The runtime only refreshes its sets for preference changes. Necessary topology
rebuilds preserve completed totals and invalidate the rate baseline. Errors,
missing counters and warming-up are explicit; IPv4/IPv6 availability is separate.
DNS fail-open continues read-only samples. Missing intervals are not estimated.
Monitoring disable or process restart intentionally starts a new session.

These are software-path, client-facing observations, including traffic to the
router. They do NOT report which family HomeProxy used for its WAN connection,
and flow/hardware offload can undercount. No automatic offload disable or
nlbwmon reconfiguration is performed.

## 3. Upgrade and recovery

Use the official SDK `luci-app-netpreference_0.2.0-r2_x86_64.ipk` after confirming
its SHA256. Ordinary `opkg install` upgrades r6/r1; do not uninstall first and do
not force dependencies. Refresh the LuCI page after upgrading to replace cached
JavaScript. Existing `/etc/config/netpreference` remains a conffile; no wholesale
configuration migration or overwrite is introduced. Restore affects only owned
NetPreference resources, not HomeProxy/SmartDNS/NAT66/nlbwmon.

## 4. Evidence boundaries

Tests include Go race and real DNS wire behavior, deterministic four-direction
sampling, unix control/CLI RPC, generated UI-to-backend inheritance, Chromium
DOM interactions, actual kernel namespace forwarding/local-proxy packet paths,
and official target uci.js/native UCI plus opkg upgrade tests. See the separate
verification report for actual outcomes, source SHA, artifacts and exclusions.
The kernel lab does not run the user's HomeProxy or firmware kernel; browser
hosting and target-uci ubus transport use declared fixtures. No live user-router
acceptance or real proxy WAN-family measurement is claimed.
