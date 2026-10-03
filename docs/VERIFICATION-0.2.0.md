# NetPreference 0.2.0 验证报告

日期：2026-10-03。目标：ImmortalWrt 24.10.4 x86_64；包版本 `0.2.0-r1`。分支：`feat/0.2.0-policy-profiles`。验收入口：[PR #2](https://github.com/LE-saber/NetPreference/pull/2)。

## 最终产物与历史证据的区别

开发过程中首次官方构建已成功，但延长 fuzz 又发现查询名重复规范化问题，已修复并加入保留的回归种子。因此 **run 37087556392 / 618915a 的 IPK 只是历史证据，不是最终推荐安装包**。最终交付必须使用包含 `normalization_test.go`、种子 `1b80e8a98011dc15` 和幂等 `normalizeQueryName` 的后续成功 SDK 构建。

最终 PR 的验收清单固定列出实际成功的 run / source SHA / IPK SHA256；每份 SDK artifact 自带 `build-info.json`、`SHA256SUMS`、`source.tar.gz`、`outcomes.json` 和完整日志。不把排队状态或仅存在 IPK 文件当作验证通过。文档不为了写入自己的新 commit SHA 而无限产生重复构建。

## 1. 核心验证

`bash scripts/check.sh` 包含 Go race、vet、DNS wire fuzz、UCI fuzz、**30 秒双 worker DomainSet 差分 fuzz**、8 项包测试、5 项 workflow 守卫和 LuCI JavaScript API 契约。

修复最终双尾点反例后，本地独立延长 fuzz 完整通过 **610275 次执行**；新增规范化幂等性与直接/集合双尾点一致性测试通过。重新执行的 race 单测全部通过，本地 Go 1.23.2 核心覆盖率 **80.5%**，DNS/UCI fuzz、包、workflow、LuCI、Python/shell 语法检查也分步通过。一次完整本地命令在 90 秒工具预算耗尽时被截断，单独记录失败 011；不把那次被截断的命令记为整套完成，最终整套结果以 Actions 为准。

覆盖范围：旧 r6 无迁移；匹配边界、大小写和单一根域点；DomainSet 最具体成员；Profile 隔离；显式 0/false 与缺省继承；设备有效协议族；无效引用及超限；最具体单条策略；DNS action 与时序分层；A/AAAA 各自的动作、上游和探测；并发 reload 与旧在途探测；wire label 不可伪装成点分域名。

LuCI JavaScript 是真实 Node.js 执行，但 `form/rpc/uci` 为 API doubles。它检查命名引用、选择器、继承/零值、旧动作、仅提交本插件的保存路径及错误处理。**没有将它称为真实浏览器或指定 LuCI 主题下的视觉验收。**

## 2. 真实 DNS 与内核行为

本地/CI socket tests 同时执行 UDP/TCP，检查不同域名 A/B、对称 IPv4/IPv6 偏好、主动探测关闭、缺少首选记录时保留另一族答复、模式切换和缓存隔离。

历史分支 run [37088022877](https://github.com/LE-saber/NetPreference/actions/runs/37088022877) 的 verify job `111102399564` 已成功穿过真实 nft 重定向测量：

| 情况 | 观测 |
|---|---|
| 未命中域名，设备 IPv6 默认 B=100ms | A 答复约 101ms，IPv4 地址保留 |
| `*.v4.test`，IPv4 偏好 A=90/B=180 | AAAA 约 181ms，IPv4/IPv6 × UDP/TCP 四组通过 |
| `fast.test`，IPv6 偏好 B=45 | A 约 46ms，四组通过 |
| work 切换 travel 普通双栈 | 同域名 AAAA 从约 181ms 降至约 0.69ms，delayed 不再增加 |
| 无效 Profile reload | 返回错误，仍保留当前策略及 NAT66 哨兵 |
| 常驻 NXDOMAIN | 模式切换后仍被阻断，不被偏好规则解除 |

上述时延是具体实验观测，不是外网延迟服务保证。最终修复后的流水线会再次执行相同内核测试，不能拿历史 run 替代最终 head 的检查。

`sudo python3 tests/netns.py` 在 disposable runner 网络命名空间运行真实双栈 nft、conntrack、转发计数及 NAT66。未选择设备始终使用原 DNS。显式 Restore 后复用原 UDP 源端口返回原 DNS；SIGKILL 后独立 guard 恢复。

曾发现旧测试仅等待 nft 表消失，过早复用尚未清理的 DNAT tuple。修复后同时等表和同一旧 tuple 消失，仍保留 15 秒总上限与一次性 DNS 断言，不通过重试掩盖失败。修复 run 的日志为 `RECOVERY nft and cached DNAT cleared in 3.0756352890000045 seconds`，之后 IPv4/IPv6 查询正常。该观测不是所有环境下的 3 秒保证。

边界：UCI/ip inventory 部分为 fixtures；测试内核为 Actions Linux，不是目标 6.6.110 整机。未修改主机外部防火墙或用户路由器。

## 3. 官方 SDK、包格式和真实 opkg

SDK 固定为 `immortalwrt-sdk-24.10.4-x86-64_gcc-13.3.0_musl.Linux-x86_64.tar.zst`，SHA256 为 `4f5de9da8674d4acdb6bd2afe594ea079c9983690d9e2eb1661a99b8fcd7d270`。官方 packages feed 实际 commit 写入每份 build-info，不用本地打包器产物冒充官方 SDK 包。

首次 SDK run [37087556392](https://github.com/LE-saber/NetPreference/actions/runs/37087556392) 已证明编译、静态 linux/amd64 二进制检查和官方 rootfs 升级链路可运行；该包因后续 fuzz 修复不再作为最终交付。最终 run 必须重新完成全部步骤。

`tests/rootfs_install.sh` 对实际 SDK IPK 在官方 ImmortalWrt 24.10.4 rootfs 用真实 opkg 执行：新装/版本检查/移除；从固定 main SHA 编译的真实 r6 安装；写入用户修改的旧配置；普通升级至 0.2.0-r1；确认旧配置逐字节不变；卸载仍保留修改后的 conffile。

opkg 把新默认配置写到 `netpreference-opkg` 的提示是预期 conffile 保护，不应使用 force-maintainer 覆盖用户配置。实验跳过在线服务 hooks，为缺失的非核心依赖设置仅存在于隔离 rootfs 的状态 fixture，未使用 force-depends；不等同生产 procd/rpcd/代理整机安装生命周期验收。

## 4. Actions 与发布门禁

相关代码提交在所有 feature branch 跑完整验证；PR 单独跑完整验证；只有 verify 成功才调用 SDK。SDK workflow 默认只读；主 workflow release 必须满足 main push、verify 成功、SDK/rootfs 成功，且不覆盖已有 Release。开发分支不创建正式 Release，本轮没有合并 main。

GitHub concurrency 可能自动替换旧 pending run，不能将取消的排队任务误称产品失败。应核对最终 commit 的成功检查，以及 SDK artifact 的各阶段 outcomes。

## 5. 失败记录与验证边界

`docs/failures/0.2.0/001` 至 `011` 每项独立提交。实际定位并修复空通配扩大、查询名去空白、重复尾点规范化、SIGKILL 测试完成条件；其余为容器网络/下载门禁、连接器路径、命令预算和附加归档检查路径问题。保留两个差分 fuzz 反例并将域名 fuzz 从 3 秒提高到 30 秒。

没有独立第三方审查代理；实现者完成核心工作与复核，容器和 Actions 执行真实测试。未执行目标实体路由器升级、真实 LuCI 浏览器、现场 HomeProxy/SmartDNS 联调、长期大规模压力测试或外网应用协议族统计。

DNS 偏好仍受客户端缓存、Happy Eyeballs、DoH/DoT、VPN、HTTPS/SVCB 与 IPv6 可达性影响。流量是插件会话观察，offload 可能低估；不会自动关闭 offload。共享 Profile/DomainSet 编辑会影响其引用者，目标机验收宜先在单台非关键设备上进行。
