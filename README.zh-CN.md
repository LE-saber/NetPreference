# NetPreference

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md)

面向 ImmortalWrt / OpenWrt 的按设备 DNS 策略 LuCI 插件。当前发布为 [v0.1.0 MVP](https://github.com/LE-saber/NetPreference/releases/tag/v0.1.0)，包修订 `0.1.0-r6`，面向 OpenWrt/ImmortalWrt 24.10 x86_64。Release 将 ImmortalWrt 24.10.4 列为测试目标；不同实机环境仍需验收。

**不是 IPv6 强制接管、NAT64 或限速器。** 它通过有限的 A/AAAA 响应时序偏好及明确的 DNS 规则管理已选择设备；未选择设备保留原有 DNS 路径。目标路由器的现场兼容性仍需实机验收，不能把单元测试或 Linux 网络命名空间测试视为整套路由器已验证。

## 已实现

- LuCI 设备管理：普通双栈、IPv6 优先、IPv4 优先、自定义偏好、整设备 DNS 阻断；支持 MAC 发现及同一设备的 IPv4/IPv6 地址。
- 域名规则：设备级／所有已选设备、精确域名／子域名、A/AAAA/HTTPS/SVCB 类型，支持 rewrite、指定地址、NXDOMAIN、NODATA、REFUSED、sinkhole、按类型过滤和上游覆盖。
- 可关闭的流量观察：IPv4/IPv6 上传、下载速率；实时速率、会话累计值与 IPv6 占比均来自 NetPreference 自己的 nft 计数器，不依赖 nlbwmon。**监控或服务计数器重置时累计值重新开始；硬件／软件 flow offload 可能导致低估，不是计费级统计。**
- Apply / Restore、首次修改前的私有状态记录、独立 nft 表、超时激活集合、独立 watchdog、限定范围的 conntrack 清理及卸载前恢复。
- 静态 Go 程序、LuCI 页面、rpcd/ACL、procd 服务、`.ipk` 构建器、单元／协议／故障／打包／网络命名空间测试及 CI。

“全局规则”仅覆盖**已启用并选择的设备**，不会为追求全局阻断而偷偷接管其他设备。

## 保留现有 DNS 链

| 设备 | DNS 路径 |
| --- | --- |
| 未选择 | 原有解析路径，通常为 `dnsmasq :53` |
| 已选择且已识别 | `NetPreference :1053` → 默认原上游 `127.0.0.1:53` |
| 原有上游示例 | `dnsmasq` → HomeProxy/sing-box → SmartDNS → 上游 |

只拦截可信 LAN 接口上已识别 MAC＋源 IP 的 UDP/TCP 53；不拦截路由器本机输出，不修改已有 HomeProxy、SmartDNS、dnsmasq、NAT66 或 nlbwmon 配置。新 IPv6 隐私地址尚未识别时先走原有 DNS，避免因身份缓存未刷新导致断网。其他软件更早执行的 DNS/TPROXY 规则、受限访客区 INPUT 策略，需要现场检查。

## 安装与启用

从 [v0.1.0 Release](https://github.com/LE-saber/NetPreference/releases/tag/v0.1.0) 下载 `luci-app-netpreference_0.1.0-r6_x86_64.ipk` 和 `SHA256SUMS`，将二者放到路由器 `/tmp`。Actions 构建附件可能使用不同修订号，以对应附件名称及测试记录为准；下载到包不代表所有现场验收已经通过。

```sh
# 在路由器上，使用与当前固件匹配的软件源。
cd /tmp
sha256sum -c SHA256SUMS
opkg update
opkg install ./luci-app-netpreference_0.1.0-r6_x86_64.ipk
```

依赖 `luci-base`、`rpcd`、`uci`、`firewall4`、`nftables-json`、`ip-full`、`conntrack`。不依赖、安装或删除 nlbwmon；其他监控服务可独立运行。不要在实机使用 `--force-depends` 或强行安装不匹配内核的软件包。

安装后进入 **LuCI → Services / 服务 → NetPreference**。安装默认空闲，不自动选择设备。保持默认上游 `127.0.0.1:53`，确认 LAN 接口（默认 `br-lan`），先添加一台非关键测试设备并点击 **Save & Apply**。界面提供恢复按钮。当前页面使用简体中文标签；文档四语言支持不改变程序界面。

## A/B 的实际定义

A：从非首选类型查询到达起，等待首选地址证据的最长时间。

B：只有确认存在首选地址时，才额外延迟非首选地址响应。总等待有上限；没有 AAAA 时保留并尽快返回有效 A，不无限等待、不默认删除 A。

| 预设 | A | B |
|---|---:|---:|
| Gentle | 75 ms | 40 ms |
| Balanced | 120 ms | 80 ms |
| Strong | 250 ms | 150 ms |

允许自定义 `A≤750 ms`、`B≤500 ms`、`A+B≤1000 ms`。IPv4 优先对称处理。主动探测查询首选记录类型，但**不代表探测 IPv6 网络可达性，也不能迫使只使用 IPv4 的应用改用 IPv6**。Happy Eyeballs、应用缓存、DoH/DoT、VPN DNS 和 HTTPS/SVCB 提示均可能影响结果。

## 恢复与卸载

```sh
# 正常或守护进程失效时均提供恢复入口。
netpreference restore

# 正常停止：撤销拦截，但保留下一次启动时的启用意愿。
/etc/init.d/netpreference stop

# prerm 会先恢复；恢复失败时拒绝继续移除恢复程序。
opkg remove luci-app-netpreference
```

恢复仅撤销本插件有所有权标记的规则与指定 DNS 重定向连接，不回写旧版整份配置。`/etc/netpreference/baseline.json` 保留为审计记录，权限为 root 私有；它可能包含原配置中的敏感内容，**不要上传至公开仓库**。正常日志和构建附件不包含现场快照。

恢复不是“任何故障下绝不丢一个 DNS 请求”的保证。watchdog、nft、conntrack、内核和原上游仍需可用；若守护进程和 watchdog 同时被杀，集合超时只保证新连接不再被拦截，旧 conntrack 会话仍可能等待到期。详见 [架构与安全边界](docs/ARCHITECTURE.md)。

## 开发、验证与交付边界

```sh
# Go >= 1.23、Python 3、Node.js；无第三方 Go 模块下载依赖。
bash scripts/check.sh

# 仅在可丢弃、有 root / nft / conntrack 的 Linux 测试环境中执行。
sudo python3 tests/netns.py

# 可选：官方 ImmortalWrt rootfs 中检查 opkg 安装/移除，不启动生产路由器。
sudo bash tests/rootfs_install.sh
```

直接打包：`python3 scripts/build_ipk.py`。产物位于 `dist/`，采用 OpenWrt 24.10 的 tar/gzip `.ipk` 布局及静态 linux/amd64 程序。`openwrt/luci-app-netpreference/Makefile` 提供 SDK/feed 集成入口；v0.1.0 Release 提供 SDK 构建的 x86_64 包；具体构建与测试状态以相应发布及工作流记录为准。

早期开发分支为 `feat/mvp`。执行计划见 [EXECUTION.md](docs/EXECUTION.md)，测试证据与未验证项见 [VERIFICATION.md](docs/VERIFICATION.md)，中断失败及恢复记录在 [docs/failures](docs/failures/)。

上述文档记录早期开发阶段，部分仍涉及 nlbwmon 旧方案。当前流量行为以 [traffic.go](internal/netpref/traffic.go) 和 [LuCI 页面](files/www/luci-static/resources/view/netpreference/overview.js) 为准。本次文档更新未进行目标路由器验收测试。自动化运行测试结果请查看此 PR 的 [GitHub Actions 检查](https://github.com/LE-saber/NetPreference/pull/3/checks)。

许可证：[MIT](LICENSE)；编译进程序的 Go 标准库另附 [BSD 许可证](docs/LICENSE-Go)。
