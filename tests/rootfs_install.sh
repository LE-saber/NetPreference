#!/bin/bash
# Userspace/package-manager check only. Never boots a router or edits host UCI.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d /tmp/netpreference-rootfs-XXXXXX)
trap 'rm -rf "$WORK"' EXIT
cd "$WORK"
BASE=https://downloads.immortalwrt.org/releases/24.10.4/targets/x86/64
IMAGE=immortalwrt-24.10.4-x86-64-rootfs.tar.gz
curl --fail --location --retry 1 --connect-timeout 20 --max-time 120 "$BASE/sha256sums" -o sha256sums
curl --fail --location --retry 1 --connect-timeout 20 --max-time 120 "$BASE/$IMAGE" -o "$IMAGE"
awk -v name="$IMAGE" '$2 == name || $2 == "*"name { print }' sha256sums > image.sha256
[ -s image.sha256 ]
sha256sum -c image.sha256
mkdir root
tar -xzf "$IMAGE" -C root
mkdir -p root/tmp root/dev
[ -e root/dev/null ] || mknod root/dev/null c 1 3
PACKAGE=$(find "$ROOT/dist" -maxdepth 1 -name '*.ipk' -print -quit)
cp "$PACKAGE" root/tmp/netpreference.ipk
# The target rootfs may lack LuCI/dependencies. Force-depends is confined to this
# disposable offline package-format test, never recommended on the real router.
IPKG_INSTROOT=/ chroot root /bin/opkg --force-depends install /tmp/netpreference.ipk
chroot root /usr/sbin/netpreference version
IPKG_INSTROOT=/ chroot root /bin/opkg status luci-app-netpreference
grep -q "option enabled '0'" root/etc/config/netpreference
test -x root/usr/libexec/rpcd/netpreference
test -s root/usr/share/rpcd/acl.d/luci-app-netpreference.json
IPKG_INSTROOT=/ chroot root /bin/opkg --force-depends remove luci-app-netpreference
test ! -e root/usr/sbin/netpreference
printf '%s\n' 'PASS: ImmortalWrt 24.10.4 rootfs opkg install/version/remove. Offline scripts skipped by design; no target-kernel or LuCI browser claim.'
