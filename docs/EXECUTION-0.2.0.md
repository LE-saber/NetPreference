# NetPreference 0.2.0 主执行计划

基线：`main@38a69cbb529eb8fc1b820fa00d7cc439306112f4`，现有包 `0.1.0-r6`。工作分支：`feat/0.2.0-policy-profiles`。目标包：`0.2.0-r1`。日期：2026-10-03。

执行与集成负责人：本轮助手直接实现；容器与 GitHub Actions 是实际执行工具，不虚称独立代理或独立审查。授权仅限该仓库开发分支、测试和构建；不合并 main、不发布开发 Release、不接入生产路由器。

## 1. 取得可信基线与执行环境

1.1 已通过 GitHub 连接器核实 main SHA、包版本和写权限，从准确 SHA 建立开发分支。读取 `internal/netpref/{config,policy,runtime}.go`、LuCI 页面、打包脚本和两份 workflow，确认现有 DNS action、恢复、计数器和升级边界。
1.2 容器不能直接联网，已通过只读 Actions source snapshot 下载完整源码并核验双层 SHA256；临时 `.github/workflows/source-snapshot.yml` 在最终交付前删除。
1.3 用 Go 1.23.2 / Python 3 / Node.js 执行 `scripts/check.sh`。首次整条命令超时已单独提交失败记录；race 单测覆盖率 76.9%，分步继续后的 DNS/UCI fuzz、8 项打包测试、LuCI API 契约测试均通过。
1.4 新增文件前先保留本地 git 基线。任何导致工作中断的错误单独写入 `docs/failures/0.2.0/` 并提交；测试断言失败必须修复后重测，不将预计成功视为成功。

## 2. 数据模型、继承与匹配核心（亲自实现）

2.1 修改 `internal/netpref/config.go`，新增 `profiles.go`：保留 device 的所有旧字段作为设备默认；新增可选 `device.profile`、命名 `profile`、扁平命名 `domain_set` 和归属于 Profile 的 `policy`。旧 `rule` 保留并可选引用 Profile/DomainSet。
2.2 Profile 只组织高级域名覆盖和可选 DNS 动作，不覆盖设备默认。无 Profile / 未匹配域名仍使用设备自身默认。域名规则必须直接域名与命名集合二选一；集合不递归。引用使用稳定 UCI section ID，显示名称可修改。
2.3 时序覆盖字段区分“未设置”与显式 0 / false。每个域名只选一条最具体的高级规则，缺省字段直接继承设备，而非叠加多个规则。所有实际继承组合仍满足 A<=750、B<=500、A+B<=1000 ms。
2.4 匹配保持 `*.example.com` 包含根域的旧语义，按标签边界匹配；集合按命中的最具体成员计分。DNS action 与时序覆盖分层解析，时序规则不能解除已有阻断；整设备 block 最高。旧动作之间仍先设备范围、后域名具体度，完全同分才使用 Profile 范围和配置顺序。
2.5 修改 `policy.go`：每个请求取得不可变配置快照；A/AAAA 分别解析自身 DNS action 和上游，探测不能借用另一类型的专用上游或绕过阻断。证据缓存隔离不同配置/Profile，防止切换后旧探测污染新策略。有效 IPv4 不因偏好被删除，无无限等待。
2.6 失败路径：无效引用、冲突选择器或继承超限在 Apply 的网络修改前拒绝；原有效运行配置保持。禁止修改 `firewall.go` 的所有权/恢复协议及共享 DNS、NAT66 配置。

## 3. LuCI 与兼容升级

3.1 整理 `files/www/luci-static/resources/view/netpreference/overview.js`：保留概览、流量、恢复和设备默认，增加每设备 Profile 选择；时序参数保留在编辑设备的高级部分。
3.2 新建高级配置页面及菜单子页面：命名 Profiles、域名集、域名偏好覆盖和原 DNS action 分区编辑；直接域名/集合二选一，继承值与显式 0 清晰显示。普通页面无需展开所有高级表格。
3.3 仅保存/提交 netpreference 包，不调用全局 uci.apply；高级模式可先保存再由设备切换。补前端必需引用/范围校验，后端仍是权威验证。
3.4 不做整文件迁移或重建旧配置；继续使用 conffiles 保护。新增旧 r6 UCI fixture，验证所有原字段、动作与 A/B 原样解析，新增空字段自动兼容。
3.5 同步 Go Version、SDK Makefile、打包/安装说明；修复测试按目录第一个 IPK 误选旧版本的问题。

## 4. 可复现验证

4.1 扩展 Go 测试：域名边界、大小写/尾点、集合最具体成员、Profile 隔离、未匹配回退、零值继承、复合时限、无效引用、动作组合及旧配置。
4.2 通过真实本地 UDP/TCP DNS fixture 测量不同域名的 IPv4/IPv6 偏好和不同 A/B；验证 no-AAAA、禁止探测、对称 IPv4、Profile 切换和旧 in-flight 探测隔离。执行 `go test -race`、`go vet`、现有与新增 fuzz。
4.3 扩展 `tests/luci_contract.js` 覆盖新页面、Profile 选择和 own-package-only 保存；扩展 `tests/test_package.py` 检查版本、菜单与新文件。
4.4 GitHub Actions 实际运行 `tests/netns.py`，覆盖双栈拦截、Profile 切换、恢复、conntrack、SIGKILL fail-open 及 NAT66 保留；容器无 nft 时不伪称本地内核测试通过。
4.5 官方 ImmortalWrt rootfs 中通过真实 opkg 检查新装/旧 r6 升级/卸载和原配置字节保留。该实验跳过生产服务启动，不等价于用户路由器实机验收。

## 5. Actions、SDK 与交付

5.1 重构 `ci.yml` 的旧 feat/mvp / bootstrap 专属逻辑：feature branches、PR、main 执行完整验证；SDK workflow 作为可复用构建被调用。默认只读权限，失败记录仅在合适的开发分支独立提交，PR 不写目标分支。
5.2 使用现有固定 ImmortalWrt 24.10.4 SDK URL 与 SHA256，通过官方 Go feed 编译静态 x86_64 IPK；核验包元信息、文件、版本和 checksum，保存完整日志与源码快照。
5.3 Release 从开发构建彻底分离：仅 main push 且验证与 SDK 均成功后可发布；开发分支/PR/手动开发构建只产出 Actions artifacts。
5.4 将测试后的源代码写回开发分支；核对远端 head 与测试对应 SHA，取得实际成功的 Actions run、官方 IPK、SHA256 和可审阅 PR。不得把已排队等同构建成功。
5.5 更新本计划完成状态，新增验证报告与中文使用示例，交付实际包和源码/PR链接；明确未执行的目标路由器 LuCI/HomeProxy/SmartDNS 现场验收边界。

## 初始非目标

不实现 NAT64、强制 IPv6、DoH/DoT 接管、连接层优先保证、复杂 Profile 继承链或递归域名集。不恢复 nlbwmon 累计依赖；保留 r6 自有 nft 会话统计。真正影响安全或升级的失败必须修复或明确阻断，不以删测试规避。

后续完成状态和证据将在交付前追加。
