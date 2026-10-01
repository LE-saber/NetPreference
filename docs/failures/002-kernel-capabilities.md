# Failure 002: local kernel integration preflight

Date: 2026-10-01. Stage: live nftables / conntrack / namespace verification.

Executed `unshare -n true`; exit status 1: `unshare: unshare failed: Operation not permitted`. `nft` is also absent. The development container does not grant CAP_NET_ADMIN or CAP_SYS_ADMIN.

Impact: loopback UDP/TCP DNS behavior, race tests, mocked activation/restore, package construction and inspection pass locally, but they cannot prove real nftables packet interception or conntrack recovery. This is an environment block, not evidence of a router failure.

Recovery: provide an isolated root-only network-namespace integration test and run it on the repository's GitHub Actions Ubuntu runner where those capabilities are available. Do not touch a production router or report target ImmortalWrt verification without evidence. Source and a real x86_64 .ipk have already been built in the development container; work continues.
