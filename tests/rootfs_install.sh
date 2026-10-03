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
mkdir -p root/tmp root/dev root/var/lock
[ -e root/dev/null ] || mknod root/dev/null c 1 3
VERSION=$(sed -n 's/^PKG_VERSION:=//p' "$ROOT/openwrt/luci-app-netpreference/Makefile")
REV=$(sed -n 's/^PKG_RELEASE:=//p' "$ROOT/openwrt/luci-app-netpreference/Makefile")
PACKAGE=${NETPREFERENCE_IPK:-"$ROOT/dist/luci-app-netpreference_${VERSION}-r${REV}_x86_64.ipk"}
LEGACY=${NETPREFERENCE_LEGACY_IPK:-"$ROOT/dist-legacy/luci-app-netpreference_0.1.0-r6_x86_64.ipk"}
test -s "$PACKAGE"
test -s "$LEGACY"
cp "$LEGACY" root/tmp/legacy.ipk
cp "$PACKAGE" root/tmp/netpreference.ipk
# The minimal target rootfs omits a few runtime tools that are present on the
# real target router. opkg filters a local package candidate before force-depends
# can apply, so register isolated placeholder status entries for those missing
# dependencies. They are never executed and exist only inside this disposable
# rootfs package-format lab.
STATUS=root/usr/lib/opkg/status
for dep in ip-full conntrack; do
 if ! grep -q "^Package: $dep$" "$STATUS"; then
  cat >>"$STATUS" <<EOF

Package: $dep
Version: 0-test
Architecture: x86_64
Status: install ok installed
EOF
  mkdir -p root/usr/lib/opkg/info
  : >"root/usr/lib/opkg/info/$dep.list"
 fi
done
chroot root /bin/opkg print-architecture
# New installation is idle and removable.
IPKG_INSTROOT=/ chroot root /bin/opkg install /tmp/netpreference.ipk
chroot root /usr/sbin/netpreference version
IPKG_INSTROOT=/ chroot root /bin/opkg status luci-app-netpreference
grep -q "option enabled '0'" root/etc/config/netpreference
test -x root/usr/libexec/rpcd/netpreference
test -s root/usr/share/rpcd/acl.d/luci-app-netpreference.json
node "$ROOT/tests/luci_target_uci.js" "$WORK/root"
IPKG_INSTROOT=/ chroot root /bin/opkg remove luci-app-netpreference
test ! -e root/usr/sbin/netpreference
# Install the true r6 binary, persist a real legacy config, then perform an ordinary upgrade.
IPKG_INSTROOT=/ chroot root /bin/opkg install /tmp/legacy.ipk
test "$(chroot root /usr/sbin/netpreference version)" = 0.1.0
cp "$ROOT/tests/fixtures/legacy-r6.uci" root/etc/config/netpreference
before=$(sha256sum root/etc/config/netpreference | cut -d ' ' -f 1)
IPKG_INSTROOT=/ chroot root /bin/opkg install /tmp/netpreference.ipk
test "$(chroot root /usr/sbin/netpreference version)" = "$VERSION"
test "$(sha256sum root/etc/config/netpreference | cut -d ' ' -f 1)" = "$before"
IPKG_INSTROOT=/ chroot root /bin/opkg status luci-app-netpreference | grep -F "Version: ${VERSION}-r${REV}"
test -s root/www/luci-static/resources/view/netpreference/advanced.js
IPKG_INSTROOT=/ chroot root /bin/opkg remove luci-app-netpreference
test ! -e root/usr/sbin/netpreference
# opkg must retain the user-modified conffile rather than erase it on remove.
test "$(sha256sum root/etc/config/netpreference | cut -d ' ' -f 1)" = "$before"
printf '%s\n' 'PASS: ImmortalWrt 24.10.4 opkg new install, remove, actual r6 -> current upgrade, byte-identical legacy config, and modified-conffile retention. Offline package hooks skipped; runtime lifecycle is exercised in the isolated kernel lab.'
