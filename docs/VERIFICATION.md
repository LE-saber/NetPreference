# Verification ledger: 0.1.0 MVP

## Evidence already executed locally

Environment: isolated Linux/amd64 development container, Go 1.23.2, Node 22.16.0,
Python 3. No direct GitHub DNS access, nft binary, CAP_NET_ADMIN or CAP_SYS_ADMIN.
No physical router and no independent testing agent were used.

- `go test -race -coverprofile=dist/coverage.out -v ./...`: PASS; internal/netpref
  statement coverage 76.6% in the initial full run. cmd lifecycle integration is
  covered separately by the namespace lab, not this percentage.
- Real loopback UDP/TCP forwarding, UDP truncation/TCP retry, concurrent pipelined
  A/AAAA reply order, IPv6 bias timing, IPv4 inverse timing, AAAA absence, finite
  no-probe waits, rule precedence, custom-upstream failure fallback: PASS.
- IPv6 preference timing observed approximately 100 ms additional hold in the
  explicit 100-ms-B case; no-AAAA case roughly 0.4 ms rather than an artificial
  A+B wait. These are test-container observations, not router performance promises.
- Mocked health/ownership/nft transaction/disk failure, own-table rollback,
  idempotent Restore, UCI write isolation, reboot inhibitor, restricted conntrack
  selectors, neighbor trust boundary, private socket and baseline permissions:
  PASS. Intentional error injection is a passing negative test, not a failed task.
- `go vet ./...`, gofmt check, static linux/amd64 build: PASS.
- Fuzz smoke: DNS 29,446 inputs, UCI 7 inputs in the first short runs; both PASS.
  This is smoke testing, not exhaustive protocol/security proof.
- Six package tests: deterministic tar-format .ipk, architecture, static ELF,
  executable modes, conffiles, idle installation config, shell syntax and offline
  script guard, checksums, scoped ACL/RPC: PASS.
- Actual LuCI JS exercised using API doubles: render/status/traffic/preset and
  own-UCI-only Save & Apply contract PASS. This is not a real browser screenshot
  or a target LuCI/rpcd end-to-end test.

Reproduce the full local suite: `bash scripts/check.sh`. Per-test logs and compiler
metadata are in `dist/logs`, `dist/coverage.out`, `dist/build-info.json` after a run.
Newly generated logs supersede the initial numeric smoke counts above.

## Kernel / target-userspace gates

`unshare -n true` locally failed with Operation not permitted (failure002).
The committed CI runs:

1. `sudo python3 tests/netns.py`: real Linux IPv4/IPv6 UDP/TCP interception,
   selected vs untouched client, DNS timing/rules/fallback, own-table restore,
   reused UDP tuple conntrack cleanup, SIGKILL watchdog recovery, real forwarded
   v4/v6 counter growth and NAT66 preservation. UCI/nlbw are explicit fixtures;
   this is not an ImmortalWrt kernel/procd/LuCI test.
2. `sudo bash tests/rootfs_install.sh`: checksum-verified official ImmortalWrt
   24.10.4 rootfs, real opkg install/version/remove in disposable chroot. Offline
   control scripts are skipped and missing dependencies tolerated only in this
   isolated format test. It is not a live target-router activation test.

At source-bootstrap time these CI gates are not yet claimed passed. Check the
workflow run logs and the final delivery note; any failure receives its own
committed document under `docs/failures`. A rootfs download/installation failure
is explicitly non-blocking for source/kernel development, not a silently passed
acceptance test. Each artifact contains its actual build toolchain and logs.

## Still requires the user's actual target environment

ImmortalWrt 24.10.4 / kernel 6.6.110 / LuCI 25.300 with the actual running HomeProxy,
SmartDNS, NAT66 and nlbwmon has not been accessed in this session. Confirm:

- Install dependencies from matching firmware feeds (never force mismatched
  kernel packages); idle install does not alter original DNS or network.
- Select one noncritical test client first, verify br-lan and MAC/IPv4/IPv6 mapping.
- Apply; test A/AAAA using both UDP and TCP from that client, and compare a client
  not enrolled. Confirm HomeProxy routing and existing NAT66 traffic.
- Test custom/static/block rules, missing AAAA, failing override upstream,
  existing DNSSEC/cache/DoH settings and application Happy Eyeballs behavior.
- Enable observation; compare period totals with existing nlbwmon; note offload
  undercount rather than disabling acceleration without permission.
- Restore, reapply, reboot, restart firewall, stop daemon and uninstall. Confirm
  newer user DNS changes remain, no owned table remains after restore/uninstall,
  existing nlbwmon is still running, and the original chain remains usable.

These are target acceptance tasks, not a request to run them on a critical
workstation without a recovery path. Unknown site-specific early TPROXY or guest
INPUT policies remain compatibility risks. The optional OpenWrt SDK Makefile is
provided but is not claimed SDK-compiled unless a separate SDK build is recorded.
