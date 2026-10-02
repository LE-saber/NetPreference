include $(TOPDIR)/rules.mk

PKG_NAME:=luci-app-netpreference
PKG_VERSION:=0.1.0
PKG_RELEASE:=5
PKG_LICENSE:=MIT
PKG_MAINTAINER:=LE-saber
PKG_BUILD_DEPENDS:=golang/host
PKG_BUILD_PARALLEL:=1
GO_PKG:=github.com/LE-saber/NetPreference
GO_PKG_BUILD_PKG:=$(GO_PKG)/cmd/netpreference
GO_PKG_LDFLAGS:=-s -w
GO_PKG_TAGS:=netgo,osusergo
GO_PKG_INSTALL_BIN_PATH:=/usr/sbin
NETPREFERENCE_SRC:=$(CURDIR)/../..

include $(INCLUDE_DIR)/package.mk
include $(TOPDIR)/feeds/packages/lang/golang/golang-package.mk

GO_PKG_TARGET_VARS:=$(filter-out CGO_ENABLED=1,$(GO_PKG_TARGET_VARS)) CGO_ENABLED=0
GO_PKG_DEFAULT_LDFLAGS:=-buildid '$(SOURCE_DATE_EPOCH)' -linkmode internal

define Package/luci-app-netpreference
 SECTION:=luci
 CATEGORY:=LuCI
 SUBMENU:=3. Applications
 TITLE:=Per-device DNS policy and safe dual-stack preference
 DEPENDS:=@x86_64 +luci-base +rpcd +uci +firewall4 +nftables-json +ip-full +conntrack
endef

define Package/luci-app-netpreference/conffiles
/etc/config/netpreference
endef

define Build/Prepare
	mkdir -p $(PKG_BUILD_DIR)
	$(CP) $(NETPREFERENCE_SRC)/go.mod $(NETPREFERENCE_SRC)/cmd $(NETPREFERENCE_SRC)/internal $(PKG_BUILD_DIR)/
endef

define Package/luci-app-netpreference/install
	$(call GoPackage/Package/Install/Bin,$(1))
	$(CP) $(NETPREFERENCE_SRC)/files/* $(1)/
	chmod 0755 $(1)/etc/init.d/netpreference $(1)/usr/libexec/rpcd/netpreference
	$(INSTALL_DIR) $(1)/usr/share/netpreference
	$(INSTALL_DATA) $(NETPREFERENCE_SRC)/LICENSE $(1)/usr/share/netpreference/LICENSE
endef

define Package/luci-app-netpreference/postinst
#!/bin/sh
set -eu
[ -n "$${IPKG_INSTROOT:-}" ] && exit 0
# Install is idle: the shipped UCI configuration has no selected devices and
# enabled=0. Never change any upstream, firewall, nlbwmon or NAT66 configuration.
/etc/init.d/netpreference enable
if ubus call service list '{"name":"netpreference"}' 2>/dev/null | grep -q '"netpreference"'; then
 /etc/init.d/netpreference restart
else
 /etc/init.d/netpreference start
fi
/etc/init.d/rpcd reload
rm -f /tmp/luci-indexcache
exit 0
endef
define Package/luci-app-netpreference/prerm
#!/bin/sh
set -eu
[ -n "$${IPKG_INSTROOT:-}" ] && exit 0
# Do not remove the only recovery binary while redirects may still be live.
case "$${1:-remove}" in
 upgrade|deconfigure) /usr/sbin/netpreference deactivate ;;
 *) /usr/sbin/netpreference restore ;;
esac
case "$${1:-remove}" in upgrade|deconfigure) ;; *) /etc/init.d/netpreference disable ;; esac
exit 0
endef
define Package/luci-app-netpreference/postrm
#!/bin/sh
[ -n "$${IPKG_INSTROOT:-}" ] && exit 0
rm -f /tmp/luci-indexcache
/etc/init.d/rpcd reload || true
# Keep /etc/netpreference/baseline.json and user conffiles as forensic evidence.
# Never remove pre-existing DNS/proxy/monitor packages or their databases.
exit 0
endef

$(eval $(call GoBinPackage,luci-app-netpreference))
$(eval $(call BuildPackage,luci-app-netpreference))
