# NetPreference 0.2.0 主执行计划与完成记录

基线：`main@38a69cbb529eb8fc1b820fa00d7cc439306112f4`，旧包 `0.1.0-r6`。工作分支：`feat/0.2.0-policy-profiles`。新包：`0.2.0-r1`。日期：2026-10-03。

执行与集成负责人：本轮助手直接实现；容器与 GitHub Actions 是实际执行工具，没有另行分派独立代理。授权边界：该仓库开发分支、测试和构建；不合并 main、不发布开发 Release、不接入生产路由器。可复核证据见 [验证报告](VERIFICATION-0.2.0.md)，具体使用见 [中文说明](PROFILES-0.2.0.md)。

## 1. 可信基线与环境 — 已完成

1.1 通过 GitHub 连接器读取最新 main SHA、包版本和写权限，从准确 SHA 建立开发分支。读取 `internal/netpref/{config,policy,runtime,firewall}.go`、LuCI 页面、SDK Makefile 和两份 workflow，确认 DNS action、恢复、计数器与升级边界。
1.2 容器无法直接联网，以只读 Actions source snapshot 取得完整源码并核验 SHA256。实施后再通过校验和保护的临时传输任务提交源码。临时 `.github/workflows/source-snapshot.yml` 和 `.development/` 已从交付树删除。
1.3 运行 `bash scripts/check.sh` 建立旧版本基线。首次冷构建整条命令超过执行时限，单独提交失败记录；分步继续后的 race 单测、fuzz、打包和 LuCI 契约通过。旧核心覆盖率 76.9%。
1.4 保留本地原始快照，远端分支为交付权威。每次实际工作中断均写入 `docs/failures/0.2.0/` 并独立提交；没有以删除断言或跳过失败阶段冒充通过。

## 2. 数据模型、继承与匹配 — 已实现并验证，核心由本轮直接完成

2.1 修改 `config.go`，新增 `profiles.go`。旧 device 字段作为默认不变；新增可选 `device.profile`、命名 `profile`、扁平 `domain_set`、归属于 Profile 的 `policy`。旧 `rule` 可选引用 Profile/DomainSet，原字段兼容。
2.2 Profile 仅组织域名覆盖与可选 DNS 动作，不替换设备默认。域名选择器要求直接域名和集合二选一；集合不递归；引用稳定 UCI section ID，显示名称可独立修改。
2.3 A/B/probe 使用可空字段区分继承、显式 0、显式 false。只选最具体的一条策略，未填字段直接继承设备，不叠加其他规则。检查所有继承组合：A<=750、B<=500、A+B<=1000 ms。
2.4 保留 `*.example.com` 包含根域的旧语义，集合以实际命中的最具体成员计分；同分按配置顺序。配置名规范化与 DNS 查询名分开，拒绝 `*.`；wire label 转义防止点、空白、非 ASCII 字节伪装成另一个名字。
2.5 DNS action 与时序分层。动作先设备范围、后域名具体度，同分时 Profile 范围及列表顺序裁决；整设备 block 最高。偏好不能解除阻断，本地否定/静态响应立即返回。
2.6 修改 `policy.go`：不可变深拷贝配置快照；A/AAAA 分别解析自身动作及上游；主动探测不能借用另一类型的专用上游或绕过阻断。缓存隔离配置/Profile，旧在途探测不能污染切换后的策略；有效 IPv4 不因偏好而被删除，无无限等待。
2.7 失败处理：无效引用、选择器冲突、时限超标在 Apply 网络修改前拒绝，保留当前有效运行配置。没有修改 `firewall.go` 所有权/恢复协议或共享 DNS/NAT66 配置。
2.8 复核入口：`profiles_test.go`、`profile_behavior_test.go`、保存的差分 fuzz 反例，以及真实 UDP/TCP 与 namespace 日志。

## 3. LuCI 与升级 — 已实现，自动化边界见报告

3.1 `overview.js` 保留设备默认、A/B、概览、流量、Apply/Restore，增加每设备 Profile 下拉选择。
3.2 新增 `advanced.js` 与菜单子页，分为 Profiles、DomainSets、域名偏好、DNS 动作四区。未设置参数显示继承；高级表格不挤入普通设备页。旧规则移动到高级页，不重建、不丢弃。
3.3 保存/提交仅作用于 `netpreference`，不调用全局 `uci.apply()`。Save 可先保存模式库；Save & Apply 更新当前已选择的模式。前端先检查悬空引用与时限，后端独立验证；后端拒绝后旧运行策略保持，保存文件中的错误仍需修正。
3.4 不做整文件迁移。通过 conffiles 保留 `/etc/config/netpreference`；新增真实旧 r6 fixture，对旧字段和动作做回归。
3.5 版本、SDK Makefile、安装说明同步为 0.2.0-r1；包测试使用明确产物，避免选中目录中残留的旧版本。
3.6 验证：`node tests/luci_contract.js` 与 8 项打包测试通过；官方 rootfs 实际 opkg r6 升级后旧配置逐字节相同。未运行实体路由器 LuCI 浏览器验收，不将 API doubles 称为浏览器测试。

## 4. 集成与安全验证 — 已完成记录中的实际实验

4.1 `bash scripts/check.sh` 执行 Go race、vet、DNS/UCI/DomainSet 三类 fuzz、包内容测试、workflow 守卫与 LuCI 契约。新核心覆盖率本地 Go 1.23.2 为 80.6%，CI Go 1.27.1 为 81.1%，均只指 `internal/netpref`。
4.2 真实 UDP/TCP fixtures 检查长/短域名 A/B、两族对称偏好、主动探测关闭、无 AAAA 时保留 IPv4、切换 Profile、在途探测隔离及动作/上游组合。
4.3 `sudo python3 tests/netns.py` 在 disposable Linux runner 执行真实 IPv4/IPv6 nft 重定向、Profile 切换、无效 reload 保留原策略、指定端口 conntrack 恢复、SIGKILL watchdog、转发计数和 NAT66 哨兵保留。
4.4 PR 曾在最终 SIGKILL 用例失败：旧测试把删表当作 conntrack 清理也已完成。修复只增加对同一旧 DNAT tuple 消失的完成屏障，仍保留 15 秒上限及一次性复用原源端口的严格 DNS 断言。修复提交 `ccc761c54565d913553611cbb7f07f17a4264cf4` 的分支内核实验已通过，恢复完成约 3.08 秒，详见失败 008 和对应日志。
4.5 `bash scripts/build_legacy.sh` 从固定 r6 SHA 构建真实旧包；`tests/rootfs_install.sh` 用官方 24.10.4 rootfs 的真实 opkg 检查新装、删除、旧包安装、普通升级、配置逐字节保留、卸载保留修改后的 conffile。安装实验跳过生产服务 hooks，不等同现场生命周期验收。

## 5. CI、官方 SDK 与交付 — 实现及首个官方构建完成

5.1 两份 workflow 移除旧 feat/mvp 专属触发逻辑。相关代码提交在全部 feature 分支触发验证，PR 独立触发完整验证；通过后调用官方 SDK reusable workflow。
5.2 使用固定 ImmortalWrt 24.10.4 x86_64 SDK URL/SHA256，官方 packages Go helper 编译静态二进制，实际生成并检查 IPK；产物带 SHA256、源码 SHA、SDK/feed provenance、source.tar.gz 和阶段日志。
5.3 首次全链路分支 run `37087556392` 的 verify 与 SDK/rootfs 均成功，官方包已下载并核验。后续只有测试/文档变更；重新执行的分支/PR状态及最终交付包来源以 PR #2 的验收清单和每份产物 build-info.json 为准，不把队列中的 run 记为通过。
5.4 正式发布独立门禁：只有 main push 且 verify/SDK 均成功可创建 Release；已有 Release 不覆盖。开发分支/PR/手动 SDK 构建不发布。main 保持原始 SHA。
5.5 交付面：PR #2、官方 SDK IPK、SHA256、精确源码快照、中文使用说明、验证报告及 9 份逐次独立提交的失败记录。最终下载包与 SHA 必须来自实际成功的 SDK artifact，不使用本地打包器产物冒充。
5.6 未授权动作：不合并、不部署、不替用户修改运行中的 DNS/代理/NAT66。下一执行面是用户在目标路由器上升级并作现场验收；这不是未实现的代码。

## 非目标与剩余边界

不实现 NAT64、强制 IPv6、DoH/DoT 接管、连接层优先保证、递归域名集或复杂 Profile 继承链。保留 r6 插件自有 nft 会话统计，不恢复 nlbwmon 累计依赖。尚未执行目标实体路由器、其具体 LuCI 浏览器、procd 现场升级和 HomeProxy/SmartDNS 的整机联调；相关限制在交付报告中明确保留。
