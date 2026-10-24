# NetPreference

[English](README.md) · [简体中文](README.zh-CN.md) · [繁體中文](README.zh-TW.md) · [日本語](README.ja.md)

面向 **ImmortalWrt / OpenWrt** 的逐裝置 DNS 策略與 IPv4/IPv6 偏好工具，包含 Go 服務與 LuCI 介面。透過有上限的 A/AAAA 回應時序及明確 DNS 規則管理已選裝置，未加入裝置仍使用原有 DNS 路徑。

**發布版本：**[v0.1.0 MVP](https://github.com/LE-saber/NetPreference/releases/tag/v0.1.0)，套件修訂 `0.1.0-r6`，目標為 OpenWrt/ImmortalWrt 24.10 x86_64。發布說明將 ImmortalWrt 24.10.4 列為測試目標，但個別路由器環境仍需現場驗收。目前 LuCI 使用簡體中文，README 翻譯不會更動程式介面。

## 功能

- 依裝置設定一般雙棧、IPv6 優先、IPv4 優先、自訂時序及 DNS 阻擋，使用探索到的 MAC 與 IPv4/IPv6 位址。
- 裝置專屬或全部已加入裝置的規則，支援精確網域、子網域及 A/AAAA/HTTPS/SVCB 類型；可轉送、指定靜態位址、回覆 NXDOMAIN/NODATA/REFUSED、sinkhole 或依協定族過濾。
- 選用 IPv4/IPv6 上下載速率、工作階段累計與 IPv6 比例，直接來自 **NetPreference 自己的 nft 計數器**，不依賴 nlbwmon。監控或服務計數器重設會清除累計，offload 可能低估流量。
- Apply/Restore、私有基準記錄、專屬 nftables 表、逾時啟用集合、獨立 watchdog、限定範圍的 conntrack 清理與解除安裝前恢復。

「全域」只影響啟用且已加入的裝置。DNS 偏好無法強制僅支援 IPv4 的程式改用 IPv6，也不是 NAT64 或頻寬限制器。

## DNS 路徑與範圍

| 裝置 | DNS 路徑 |
| --- | --- |
| 未加入 | 原有解析路徑，通常為 `dnsmasq :53` |
| 已加入且已辨識 | `NetPreference :1053` → 原解析器，預設 `127.0.0.1:53` |
| 既有上游範例 | `dnsmasq` → HomeProxy/sing-box → SmartDNS → 上游 |

僅在可信任 LAN 介面比對 MAC 與來源 IP 後，攔截 UDP/TCP 53。不攔截路由器自身 DNS，不覆寫 HomeProxy、SmartDNS、dnsmasq、NAT66 或 nlbwmon 設定。新的 IPv6 隱私位址在辨識前仍走原路徑。DoH、DoT、VPN 內 DNS 與非 53 埠流量不在攔截範圍；更早執行的 DNS/TPROXY 規則與訪客網路 INPUT 限制可能影響相容性。

## 安裝與啟用

從 [v0.1.0](https://github.com/LE-saber/NetPreference/releases/tag/v0.1.0) 下載 `luci-app-netpreference_0.1.0-r6_x86_64.ipk` 與 `SHA256SUMS`，放到相符路由器的 `/tmp`：

```sh
cd /tmp
sha256sum -c SHA256SUMS
opkg update
opkg install ./luci-app-netpreference_0.1.0-r6_x86_64.ipk
```

相依套件：`luci-base`、`rpcd`、`uci`、`firewall4`、`nftables-json`、`ip-full`、`conntrack`。使用符合韌體的軟體來源，不強行略過相依性或安裝不符的核心套件。NetPreference 不會安裝或移除 nlbwmon。

開啟 **LuCI → Services / 服務 → NetPreference**。安裝後預設閒置且未選擇任何裝置。保留上游 `127.0.0.1:53`，確認 LAN 介面（預設 `br-lan`），先新增一台非關鍵測試裝置後儲存套用。Restore 按鈕可移除本工具的攔截。

## 時序定義

**A**：從非優先查詢到達起，等待優先位址類型證據的最長時間。**B**：只有確認存在優先位址時才額外延遲。總等待有上限，AAAA 不存在時不會永久壓制有效的 A 回應。

| 預設 | A | B |
| --- | ---: | ---: |
| Gentle | 75 ms | 40 ms |
| Balanced | 120 ms | 80 ms |
| Strong | 250 ms | 150 ms |

自訂限制：`A ≤ 750 ms`、`B ≤ 500 ms`、`A+B ≤ 1000 ms`。IPv4 優先對稱處理。主動探測只查詢 DNS 記錄，不測試實際 IPv6 可達性；Happy Eyeballs、程式快取、加密 DNS 及 HTTPS/SVCB 提示均可能影響結果。

## 恢復與移除

```sh
netpreference restore
/etc/init.d/netpreference stop
opkg remove luci-app-netpreference
```

守護程式無法使用時，`restore` 也提供離線恢復入口。正常停止服務會撤銷攔截，但保留下次啟動時的設定意願。移除腳本先恢復自有狀態；失敗時拒絕繼續移除恢復程式。

恢復只刪除有所有權標記的規則與限定的 DNS 重新導向連線，不整份回寫舊設定。`/etc/netpreference/baseline.json` 保留為 root 私有稽核記錄，可能含敏感原始設定，不要公開上傳。恢復仍依賴核心、nftables、conntrack、watchdog 與原解析器；同時終止服務及 watchdog 時，既有 conntrack 連線可能仍需等待到期。詳見[架構與邊界](docs/ARCHITECTURE.md)。

## 開發與驗證記錄

開發檢查使用 Go 1.23+、Python 3 與 Node.js；Go 模組沒有第三方相依項目。

```sh
bash scripts/check.sh
python3 scripts/build_ipk.py
```

產物位於 `dist/`。[SDK Makefile](openwrt/luci-app-netpreference/Makefile) 提供 feed 整合。`sudo python3 tests/netns.py` 與 `sudo bash tests/rootfs_install.sh` 應在具所需權限、可丟棄的 Linux 測試環境執行。

[執行記錄](docs/EXECUTION.md) · [驗證台帳](docs/VERIFICATION.md) · [失敗記錄](docs/failures/)

既有記錄反映早期開發階段，部分仍描述 nlbwmon 舊設計。目前流量行為以 [traffic.go](internal/netpref/traffic.go) 與 [LuCI 頁面](files/www/luci-static/resources/view/netpreference/overview.js) 為準。本次文件更新未重新執行實機或執行階段測試。

MIT 授權：[LICENSE](LICENSE)。編入程式的 Go 標準函式庫另附 [BSD 授權](docs/LICENSE-Go)。
