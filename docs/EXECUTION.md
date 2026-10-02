# Master Execution Plan: NetPreference 0.1.0 MVP

Baseline: empty public repository, initialized main at 4f26051595894a862182b28c2d89fa29de487619. Work branch: feat/mvp. Owner/integrator: Pro. No external implementation agent is available; container tools execute builds/tests. No router deployment is authorized or performed by this session.

## 1. Establish baseline and boundaries
1.1 Inspect repository metadata/files using GitHub; create a reviewable feature branch. Done.
1.2 Check compiler, network and kernel capabilities. Container git clone failed (failure 001); use zero third-party Go dependencies and connector writes. Never modify HomeProxy, SmartDNS, dnsmasq, nlbwmon or NAT66 configuration.
1.3 Research primary sources for nlbw JSON, fw4 ownership, nft timeout sets, DNS wire protocol and Happy Eyeballs. Record implementation references in docs/ARCHITECTURE.md.

## 2. Implement the DNS vertical slice (Pro direct core work)
2.1 Create internal/netpref/config.go: strict UCI parser, bounded timing/upstream/rule validation and immutable configuration snapshots. Reject malformed configurations before side effects.
2.2 Create dns.go, policy.go, server.go: checked DNS wire parsing, UDP/TCP forwarding and fallback, matching transaction/question, bounded A/AAAA evidence wait, optional active probes, deterministic device/domain policy and unsigned local answers. Keep ordinary IPv4 answers intact.
2.3 Verify with real loopback UDP/TCP upstream servers, timing measurements, malformed packets, upstream failure, AAAA absence, precedence and concurrency/race tests. Add fuzz targets.

## 3. Implement safe router control (Pro direct high-risk work)
3.1 Create platform.go and firewall.go: discover DHCP/neighbours, own only inet/netpreference with an ownership comment, intercept only selected MACs on allowlisted LAN interfaces, UDP/TCP port 53 only; leave output/NAT66/fw4 unchanged. Expiring activation lease plus external watchdog prevents permanent interception after crashes.
3.2 Create runtime.go: validate and health-check before activation, first-use private baseline, transactional nft batch, rollback on apply failure, idempotent restore, stale redirected conntrack cleanup limited to proxy port. Never restore whole user configuration files.
3.3 Create init/rpcd/ACL: root-only local control, limited method names, procd respawn and watchdog, stop/uninstall recovery, no shell evaluation of user values.
3.4 Test with mocked command runner locally and real network namespaces/nft in CI when available. Block activation if ownership, interface, port or health checks fail. Record environment-blocked checks honestly.

## 4. Implement observation and LuCI
4.1 Create traffic.go: read existing nlbwmon JSON without commit/reset/reconfigure; expose current accounting-period v4/v6 totals. Add optional nft software-path live-rate counters, with first-sample/reset validity and offload caveats. Disable all plugin polling/counters when monitoring is off.
4.2 Create LuCI configuration, device and rule forms, Apply/Restore/status and traffic table; identify clients by MAC across IP changes. Provide explicit A/B and preset semantics, limits and encrypted-DNS caveats.
4.3 Verify JavaScript/shell/JSON syntax, RPC input handling and UI contracts. Do not claim actual LuCI browser/router validation from syntax checks.

## 5. Package, verify and deliver
5.1 Create reproducible standard OpenWrt tar.gz .ipk builder for static linux/amd64 binary, control scripts and conffiles; also provide OpenWrt package Makefile. Build and inspect package contents, architecture and modes.
5.2 Run go test -race, go vet, build, fuzz smoke, package inspection and behavioral integration; record commands/results. Run network-namespace tests and opkg installation in network-capable CI if accessible.
5.3 Commit each task-interrupting failure independently under docs/failures as it occurs; fix rather than abandon. Commit source/tests/docs to feat/mvp, open PR, attach real built artifact and checksums. Final report differentiates self-tested MVP from target-router acceptance and lists residual risks.

Implementation refinements are allowed within these boundaries. No unlimited waits, no NAT64/Jool/TAYGA, no decryption of DoH/DoT, no claim that DNS timing forces application connection-family choice. No shared service removal or automatic whole-config rollback.
