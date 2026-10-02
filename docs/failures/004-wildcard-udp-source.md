# Failure 004: wildcard UDP reply source on a multihomed listener

Date: 2026-10-01. Stage: post-CI DNS transport audit.

Executed a new real-socket regression, `go test ./internal/netpref -run '^TestWildcardUDPReplySource$' -v`. Both wildcard binds (`0.0.0.0:0` and `[::]:0`) answered requests to 127.0.0.1 but timed out for a connected client querying 127.0.0.2. Exit status 1; approximately one second per failing subtest.

Root cause: ReadFromUDP/WriteToUDP lost the original local destination address. Linux chose the primary route's source address for replies. A connected DNS client correctly rejected that unexpected source. Multiple LAN addresses and IPv6 link-local destinations can encounter the same class of issue; the former single-address tests did not cover it.

Recovery in progress: receive destination/interface packet-info ancillary data and send replies using the same destination as their source, for IPv4 and IPv6. Retain the regression and extend the real namespace checks. This is a real pre-release implementation defect, not an environment limitation. No production router was accessed or changed.
