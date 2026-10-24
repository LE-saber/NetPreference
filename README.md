# NetPreference

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md)

Per-device DNS policy and IPv4/IPv6 preference for **ImmortalWrt / OpenWrt**, with a Go service and LuCI interface. It controls selected devices through bounded A/AAAA response timing and explicit DNS rules, while leaving unselected devices on their existing DNS path.

**Release:** [v0.1.0 MVP](https://github.com/LE-saber/NetPreference/releases/tag/v0.1.0), package revision `0.1.0-r6`, targeting OpenWrt/ImmortalWrt 24.10 x86_64. The release identifies ImmortalWrt 24.10.4 as its tested target; individual router setups still need acceptance checks. The current LuCI interface is Simplified Chinese. These README translations do not change it.

## Capabilities

- Per-device dual-stack, IPv6 preference, IPv4 preference, custom timing, and DNS blocking, using discovered MAC and IPv4/IPv6 addresses.
- Device-specific or enrolled-device-wide rules for exact domains and subdomains, with A/AAAA/HTTPS/SVCB selectors. Actions include forwarding, static rewrite, NXDOMAIN, NODATA, REFUSED, sinkhole and address-family filtering.
- Optional IPv4/IPv6 upload/download rates, session totals and IPv6 share from **NetPreference's own nft counters**. No nlbwmon dependency. Totals reset when monitoring/service counters reset; offload may cause undercounting.
- Apply/Restore, a private baseline record, an owned nftables table, expiring activation sets, a separate watchdog, scoped conntrack cleanup, and restore-before-uninstall behavior.

“Global” rules affect only enabled, enrolled devices. DNS preference cannot force an IPv4-only application to use IPv6; this is not NAT64 or a bandwidth limiter.

## DNS path and boundaries

| Device | DNS path |
| --- | --- |
| Not enrolled | Existing resolver path, normally `dnsmasq :53` |
| Enrolled and identified | `NetPreference :1053` → original resolver, default `127.0.0.1:53` |
| Existing upstream example | `dnsmasq` → HomeProxy/sing-box → SmartDNS → upstream resolver |

Interception covers UDP/TCP port 53 on trusted LAN interfaces after matching MAC and source IP. It does not intercept router-originated DNS or rewrite existing HomeProxy, SmartDNS, dnsmasq, NAT66 or nlbwmon configuration. Newly discovered IPv6 privacy addresses retain the original path until identified. DoH, DoT, DNS inside VPNs and non-port-53 traffic are outside the interception scope. Earlier DNS/TPROXY rules and restricted guest-network INPUT rules can affect compatibility.

## Install and enable

Download `luci-app-netpreference_0.1.0-r6_x86_64.ipk` and `SHA256SUMS` from [v0.1.0](https://github.com/LE-saber/NetPreference/releases/tag/v0.1.0). On a matching router, place both in `/tmp`:

```sh
cd /tmp
sha256sum -c SHA256SUMS
opkg update
opkg install ./luci-app-netpreference_0.1.0-r6_x86_64.ipk
```

Dependencies: `luci-base`, `rpcd`, `uci`, `firewall4`, `nftables-json`, `ip-full`, `conntrack`. Use feeds matching the router firmware; do not force missing dependencies or mismatched kernel packages. NetPreference does not install or remove nlbwmon.

Open **LuCI → Services / 服务 → NetPreference**. Installation is idle with no enrolled devices. Keep upstream `127.0.0.1:53`, verify the LAN interface (default `br-lan`), add one noncritical test device, then save and apply. Use the Restore button to remove the plugin's interception.

## How timing works

**A** is the maximum wait for evidence of the preferred address type, measured from the arrival of the nonpreferred query. **B** is an additional delay applied only when positive preferred-address evidence exists. The total hold is bounded; an absent AAAA record does not permanently suppress valid A answers.

| Preset | A | B |
| --- | ---: | ---: |
| Gentle | 75 ms | 40 ms |
| Balanced | 120 ms | 80 ms |
| Strong | 250 ms | 150 ms |

Custom limits: `A ≤ 750 ms`, `B ≤ 500 ms`, `A+B ≤ 1000 ms`. IPv4 preference is symmetric. Active probes query the preferred DNS record; they do not test actual IPv6 reachability. Happy Eyeballs, application caches, encrypted DNS and HTTPS/SVCB hints can influence the outcome.

## Restore and remove

```sh
netpreference restore
/etc/init.d/netpreference stop
opkg remove luci-app-netpreference
```

`restore` also provides an offline recovery path when the daemon is unavailable. Normal service stop removes interception while preserving the configured intent for the next start. The removal hook restores owned state first and refuses removal if recovery fails.

Restore removes owned rules and scoped redirected DNS connections, rather than replacing whole older configurations. `/etc/netpreference/baseline.json` remains as a root-private audit record and can contain sensitive original configuration; do not upload it publicly. Recovery still depends on the kernel, nftables, conntrack, watchdog and original resolver. Simultaneous daemon/watchdog termination can leave existing conntrack sessions waiting for expiry; see [architecture and boundaries](docs/ARCHITECTURE.md).

## Development and evidence

Go 1.23+, Python 3 and Node.js are used by the development checks. The Go module has no third-party dependencies.

```sh
bash scripts/check.sh
python3 scripts/build_ipk.py
```

Outputs are written to `dist/`. The [SDK package Makefile](openwrt/luci-app-netpreference/Makefile) provides feed integration. Kernel/network-namespace checks (`sudo python3 tests/netns.py`) and rootfs packaging checks (`sudo bash tests/rootfs_install.sh`) belong in disposable Linux test environments with the required capabilities.

[Execution record](docs/EXECUTION.md) · [Verification ledger](docs/VERIFICATION.md) · [Failure records](docs/failures/)

These records describe earlier development stages; some refer to the earlier nlbwmon-based design. Current traffic behavior is defined by [traffic.go](internal/netpref/traffic.go) and the [LuCI view](files/www/luci-static/resources/view/netpreference/overview.js). No target-router acceptance testing was performed for this documentation update. Consult this PR’s [GitHub Actions checks](https://github.com/LE-saber/NetPreference/pull/3/checks) for automated runtime results.

MIT License: [LICENSE](LICENSE). The compiled Go standard library has its own [BSD license](docs/LICENSE-Go).
