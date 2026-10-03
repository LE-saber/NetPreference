# NetPreference 0.2.0 验证报告

日期：2026-10-03。目标：ImmortalWrt 24.10.4 x86_64；包版本 `0.2.0-r1`。分支：`feat/0.2.0-policy-profiles`。评审入口：[PR #2](https://github.com/LE-saber/NetPreference/pull/2)。

本报告固定记录已经执行的证据，不把尚在排队/运行的后续 CI 记为成功。最终 PR 检查及交付产物来源以 PR 验收清单、Actions 和 `build-info.json` 中精确 SHA 为准。实现者直接复核，并非独立第三方审查。

## 1. 源码与官方构建证据

| 项目 | 实际结果 |
|---|---|
| 原 main / r6 基线 | `38a69cbb529eb8fc1b820fa00d7cc439306112f4`，未合并或修改 |
| 核心实现提交 | `29fb90feacf73da962073afbc43e2351ce592989` |
| workflow 实现/首次全链路源 SHA | `618915a8b1471f7732f26831896feee489e6bc8a` |
| 首次完整分支流水线 | [37087556392](https://github.com/LE-saber/NetPreference/actions/runs/37087556392)，verify、SDK 编译、SDK 包检查、官方 rootfs 升级全部成功 |
| 对应官方 IPK SHA256 | `be9cc1a139d67a11a53aabcb9c75cc7b4bc4fb2d8d1a29672e979aabdc9d4091` |
| SDK artifact | `netpreference-immortalwrt-sdk-37087556392`，ID `11261083207`；下载 ZIP SHA256 `1b63d3322222ace7d41f793c50a2fa59276b45a91ebba5c0628d4b95265d242a` |
| 恢复测试完成屏障修复 | `ccc761c54565d913553611cbb7f07f17a4264cf4`，仅修改测试，无产品源码改动 |
| 修复后完整 verify | [37088022877](https://github.com/LE-saber/NetPreference/actions/runs/37088022877)，verify job `111102399564` 成功，包含真实内核实验 |
| 修复后 verify artifact | ID `11260759023`；ZIP SHA256 `a160815bb4366647b73d65db337616c74829795abf31786de05946dccac4e064` |

上述首份官方包不是本地手工打包结果。已从其源码快照逐字节对比所有 23 个产品源文件/安装文件（排除测试），与后续工作树完全一致。后续 SDK 构建的源码时间戳和归档会变化，因此不同 run 的 IPK 哈希可能不同；应当使用各自同一 artifact 内的 SHA256SUMS，不能混用。

官方 SDK：`immortalwrt-sdk-24.10.4-x86-64_gcc-13.3.0_musl.Linux-x86_64.tar.zst`。

SDK 下载 SHA256：`4f5de9da8674d4acdb6bd2afe594ea079c9983690d9e2eb1661a99b8fcd7d270`。

首次构建 packages feed commit：`83e0be6d3ebb2da9cc406085d147efd55fa9a12a`。

`build-info.json` 实际报告：架构 `x86_64`、版本 `0.2.0-r1`、二进制 `static linux/amd64`；已运行二进制 `version` 得到 `0.2.0`。额外只读 fixture 冒烟验证官方二进制能够解析 r6 和新 Profile 配置，拒绝悬空 Profile 引用。此检查没有写入 nft 或访问生产路由器。

## 2. 核心与 UI 契约

入口 `bash scripts/check.sh`，全部通过。它执行 `go test -race`、`go vet`，以及各自限时的 DNS wire、UCI tokenizer、DomainSet 差分 fuzz。覆盖率本地 Go 1.23.2 为 **80.6%**，CI Go 1.27.1 为 **81.1%**，统计对象为 `internal/netpref`；不把 CLI 的覆盖率算成相同数字。

回归范围包括：旧 r6 无需迁移；大小写、尾点、精确/后缀边界；集合命中具体度；Profile 隔离；零值与 false 继承；设备有效协议族继承；悬空引用、过限、选择器冲突；最具体单条策略；阻断与时序分层；A/AAAA 各自的上游及探测；并发 reload；旧在途探测不能污染新模式；wire label 不得伪装成点分或 Unicode 折叠后的另一个域名。

8 项包测试和 5 项 workflow 守卫通过。LuCI JavaScript 在真实 Node.js 中执行，但 `form/rpc/uci` 是 API doubles；验证命名引用、选择器、继承/零值、旧 DNS action、保存作用域及错误显示。**这不是浏览器视觉验收，也不证明指定 LuCI 主题下的每个交互细节。**

## 3. 真实 DNS 行为，不只是静态判断

本地/CI socket tests 同时执行 UDP/TCP。修复后 Linux namespace run `37088022877` 还将同样行为穿过真实 nft 重定向；下列是该日志的观测值，不是配置上限承诺。

| 情况 | 实际测量与断言 |
|---|---|
| 未命中域名继续使用设备 IPv6 默认，B=100ms | A 答复约 101ms，IPv4 地址保留 |
| `*.v4.test` 使用 IPv4 偏好，A=90/B=180 | AAAA 答复约 181ms；IPv4/IPv6 传输 × UDP/TCP 四组均通过 |
| `fast.test` 使用 IPv6 偏好，B=45 | A 答复约 46ms；四组传输均通过 |
| 从 work 切换到 travel 普通双栈 | 同一域名 AAAA 从约 181ms 变为约 0.69ms，delayed 计数不再增加 |
| 无效 Profile reload | 返回错误；当前策略、拦截、NAT66 哨兵保持 |
| 常驻 NXDOMAIN 与模式切换 | 切换后仍返回 NXDOMAIN，没有被双栈/偏好规则解除 |
| 无 AAAA 或首选证据为否定 | 保留可用 A；不无限等待；单元/socket 测试通过 |
| 主动探测关闭、旧探测延迟完成 | 有限等待及配置隔离测试通过 |

这些证明 DNS 答复和时序，不证明客户端最终一定建立 IPv6 连接，也不测试外网 IPv6 可达性。

## 4. nft、恢复与计数器

入口 `sudo python3 tests/netns.py`，仅使用 disposable runner 的网络命名空间。创建现有 fw4/NAT66 哨兵，在每阶段检查其内容没有被插件修改；真实转发 IPv4 与 NAT66 IPv6 数据，确认插件自身两族上/下行计数增加。未选择设备仍得到原 DNS 答案。

显式 Restore 后，复用原 UDP 源端口的 DNS 查询返回原 DNS；SIGKILL 不能调用正常 stop handler，由独立 guard 完成恢复。曾发现测试误把 nft 表消失当作 conntrack 清理也结束；修复后同时观察这两个完成条件，保留 15 秒上限，随后只做一次同一原端口查询，不重试来掩盖失败。

修复后日志：`RECOVERY nft and cached DNAT cleared in 3.0756352890000045 seconds`，随后 IPv4/IPv6 原 DNS 查询均通过。不能将这一次观测当作所有设备/负载下的严格 3 秒服务保证。

边界：UCI/ip inventory 部分使用 fixtures；内核是 Actions runner Linux，不是目标 6.6.110 整机。该测试不修改主机外部防火墙或用户路由器。

## 5. 真实 opkg 新装与 r6 升级

官方 SDK job 在下载并核验官方 ImmortalWrt 24.10.4 rootfs 后执行 `tests/rootfs_install.sh`：

1. 实际安装新 SDK IPK，读取 opkg 元信息和版本，删除。
2. 从原 main 固定 SHA 编译的真实 `0.1.0-r6` 包安装成功；不是给新二进制改版本标签。
3. 写入包含真实旧字段/规则的用户修改配置，然后普通 `opkg install` 升级至 `0.2.0-r1`。
4. 验证 `/etc/config/netpreference` **逐字节没有变化**；新 parser 能读取旧配置。卸载后用户修改的 conffile 仍保留。

opkg 提示新默认配置放入 `netpreference-opkg` 是预期的 conffile 保护；退出成功且旧文件校验通过，不应建议用户用 force-maintainer 覆盖它。

实验使用真实 opkg/rootfs，但为避免服务跑进宿主，跳过在线安装 hooks，并为缺失的非核心依赖使用限定在隔离 rootfs 内的状态 fixture。没有使用 force-depends。生产 procd、rpcd、LuCI 和代理的整机安装生命周期仍需现场验收。

## 6. CI 发布边界

feature 分支相关代码 push、PR、手动验证均执行完整测试。SDK reusable workflow 没有发布权限；主 workflow 的 release 需要 verify 和 sdk 成功，且事件必须是 `push`、ref 必须是 `refs/heads/main`。开发分支没有创建正式 Release，也没有合并 main。旧 `v0.1.0` 不被覆盖。

每个 artifact 都保存实际 phase outcomes。看到一个 IPK 文件或 artifact 不等于整个 run 成功；使用前必须确认 verify 与 SDK/rootfs 都通过。PR 更新可能使尚未执行的旧 pending run 被自动替换，这与产品断言失败不同。

## 7. 逐次失败记录及剩余风险

`docs/failures/0.2.0/001` 至 `009` 均有独立提交。工程中实际发现并修复：空通配域名误扩展、查询名错误去空白、SIGKILL 测试恢复完成屏障。其他记录是受限容器/连接器传输、命令时限与附加归档检查路径问题；没有隐瞒产品测试失败，也不将工具 400 混称 SDK 编译错误。

未执行：用户实体路由器升级、LuCI 浏览器操作、特定 HomeProxy/SmartDNS 整机联调、长期大规模压力实验和外网真实应用协议族统计。保持现有共享配置及 NAT66 的代码边界和隔离实验证据不等于上述现场验证已经完成。

DNS 偏好仍受客户端缓存、Happy Eyeballs、DoH/DoT、VPN DNS、HTTPS/SVCB 与 IPv6 可达性影响。流量是插件会话观察，offload 可能低估；不会为统计自动关闭 offload。Profile/DomainSet 共享编辑会影响引用者，应先在单台非关键设备上做目标机验收。
