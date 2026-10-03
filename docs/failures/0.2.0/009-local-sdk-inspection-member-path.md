# 009 — 附加本地 SDK 冒烟检查的归档成员名错误

- 日期：2026-10-03；分支 `feat/0.2.0-policy-profiles`。
- 已完成的构建：官方 SDK run `37087556392`，SDK 编译、包内容检查及真实 opkg r6 升级实验全部成功。
- 阶段：下载并校验官方产物后，附加执行其二进制的只读 CLI 检查。
- 实际失败：临时检查脚本按 `data.tar.gz` 精确读取外层归档成员，得到 `KeyError: filename 'data.tar.gz' not found`。官方 IPK 的实际成员含 `./` 前缀。失败的是新增本地检查脚本，不是包格式、SDK、生产代码或原有 `inspect_ipk.py`（原脚本已正确处理前缀）。
- 修复：复用规范化成员名的方式，匹配移除单个 `./` 前缀后的路径；不修改包字节，不重新打包冒充官方产物。
- 实际复测：官方 SDK 二进制 `version` 返回 `0.2.0`；`nft-preview` 分别成功解析真实 `legacy-r6.uci` 和 `profiles.uci`；悬空 Profile 引用返回非零及 `unknown profile`，未输出可应用的 nft 规则。
- 边界：UCI/ip 是只读 fixture，未执行 nft 写入；此附加检查不替代已经通过的 CI 内核实验和真实 opkg 实验。
- 官方包 SHA256：`be9cc1a139d67a11a53aabcb9c75cc7b4bc4fb2d8d1a29672e979aabdc9d4091`。
- 状态：已修复并通过，无产品代码变更，无 main 或路由器修改。
