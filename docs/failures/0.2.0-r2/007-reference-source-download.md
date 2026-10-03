# 007 — 容器直接下载 LuCI 参考源码失败

- 操作：container.download 下载已通过 web 读取的官方 `https://openwrt.github.io/luci/jsapi/uci.js.html` 到容器，计划补充实际 UCI JavaScript 实现验证。
- 实际结果：`ERROR: download failed`，没有获得参考源码文件，不作为测试证据。
- 影响：不影响已在容器完成的 Go race、流量回归、前端模型与后端预校验合同测试；但不能把这些 API doubles 说成真实目标 LuCI 验证。
- 恢复方案：直接在已有官方 ImmortalWrt rootfs 测试阶段读取目标镜像自带 `uci.js`，验证自动命名节、增删、选择器切换、排序和保存，再用目标 `/sbin/uci` 与本插件 `check_config` 校验回读结果。该镜像由现有已固定 SDK/rootfs 流程获取，不再重复容器直连下载。
