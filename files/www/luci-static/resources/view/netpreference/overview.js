'use strict';
'require view';
'require form';
'require rpc';
'require uci';
'require ui';
'require poll';
'require dom';

var callStatus = rpc.declare({ object: 'netpreference', method: 'status', expect: {} });
var callDevices = rpc.declare({ object: 'netpreference', method: 'devices', expect: {} });
var callTraffic = rpc.declare({ object: 'netpreference', method: 'traffic', expect: {} });
var callApply = rpc.declare({ object: 'netpreference', method: 'apply', expect: {} });
var callRestore = rpc.declare({ object: 'netpreference', method: 'restore', expect: {} });
var callCommit = rpc.declare({ object: 'uci', method: 'commit', params: ['config'] });

function checked(result) {
 if (result && result.ok === false) throw new Error(result.error || '操作失败');
 return result;
}

function bytes(n) {
 n = Number(n || 0);
 var units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
 var i = 0;
 while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
 return n.toFixed(i ? 2 : 0) + ' ' + units[i];
}

function rate(r) {
 return r && r.valid ? bytes(r.upload_Bps) + '/s ↑  ' + bytes(r.download_Bps) + '/s ↓' : '等待有效采样';
}

function total(t, available) {
 return available ? bytes(t.upload_bytes) + ' ↑  ' + bytes(t.download_bytes) + ' ↓' : '不可用';
}

function card(label, value, note) {
 return E('div', {
  'style': 'min-width:150px;flex:1 1 150px;padding:14px 16px;border:1px solid var(--border-color-medium,#d9d9d9);border-radius:8px;background:var(--background-color-high,#fff);box-sizing:border-box;'
 }, [
  E('div', { 'style': 'font-size:12px;opacity:.72;margin-bottom:5px;' }, label),
  E('div', { 'style': 'font-size:22px;font-weight:600;line-height:1.25;' }, String(value)),
  note ? E('div', { 'style': 'font-size:12px;opacity:.68;margin-top:5px;line-height:1.45;' }, note) : ''
 ]);
}

function statusText(s) {
 if (s.active) return '策略已启用';
 if (s.desired) return '故障开放 / 等待 DNS 健康';
 return '空闲 / 未接管设备';
}

return view.extend({
 load: function() {
  return Promise.all([uci.load('netpreference'), callDevices().catch(function() { return { devices: [] }; })]);
 },

 render: function(data) {
  var self = this, known = data[1].devices || [];
  var m = this.map = new form.Map('netpreference', '策略配置',
   '按设备控制 IPv6 / IPv4 DNS 偏好与域名策略。只有加入 NetPreference 的设备才会被接管；未加入设备继续使用原有 dnsmasq → HomeProxy → SmartDNS 链路。DNS 偏好只影响解析结果的时序，不会强制应用程序使用某个协议族。');

  var g = m.section(form.NamedSection, 'main', 'global', '基础设置');
  g.description = '这里控制 NetPreference 自身的监听、上游和流量观察，不会修改 HomeProxy、SmartDNS、NAT66 或 nlbwmon 的原始配置。';

  var o = g.option(form.DynamicList, 'interface', '可信 LAN 接口');
  o.default = 'br-lan'; o.rmempty = false;
  o.description = '只对来自这些 LAN 接口、且已加入策略的设备执行 DNS 重定向。';

  o = g.option(form.Value, 'upstream', '原始 DNS 链上游');
  o.default = '127.0.0.1:53'; o.rmempty = false;
  o.description = '支持 IP:端口、tcp://IP:端口 或 udp://IP:端口；IPv6 地址需要使用方括号。1053 端口由 NetPreference 保留。';

  o = g.option(form.Value, 'timeout_ms', '上游超时（毫秒）');
  o.default = '2000'; o.datatype = 'range(100,5000)'; o.rmempty = false;

  o = g.option(form.Flag, 'monitor', '启用流量观察');
  o.default = '0'; o.rmempty = false;
  o.description = '累计流量读取现有 nlbwmon 数据库且不会清零；实时速率使用可选 nft 计数器。启用流量分流/硬件卸载时，实时速率可能低估。';

  var d = m.section(form.GridSection, 'device', '设备策略');
  d.addremove = true; d.anonymous = true; d.sortable = true;
  d.description = '为指定设备设置双栈、IPv6 优先、IPv4 优先、自定义偏好或 DNS 阻断。高级时序参数在编辑设备时显示。';

  o = d.option(form.Flag, 'enabled', '启用'); o.default = '1'; o.rmempty = false;
  o = d.option(form.Value, 'name', '设备名称'); o.datatype = 'maxlength(80)';
  o = d.option(form.Value, 'mac', '设备 MAC'); o.datatype = 'macaddr'; o.rmempty = false;
  known.forEach(function(h) {
   var ips = (h.ipv4 || []).concat(h.ipv6 || []).join(', ');
   o.value(h.mac, (h.name || h.mac) + (ips ? ' · ' + ips : ''));
  });

  o = d.option(form.ListValue, 'mode', '策略模式'); o.default = 'dual'; o.rmempty = false;
  [['dual', '普通双栈'], ['ipv6', 'IPv6 优先'], ['ipv4', 'IPv4 优先'], ['custom', '自定义偏好'], ['block', '阻断 DNS（REFUSED）']].forEach(function(v) { o.value(v[0], v[1]); });

  o = d.option(form.ListValue, 'prefer', '自定义优先协议族');
  o.value('ipv6', 'IPv6'); o.value('ipv4', 'IPv4'); o.default = 'ipv6'; o.depends('mode', 'custom'); o.modalonly = true;

  o = d.option(form.ListValue, '_preset', '偏好强度预设'); o.modalonly = true;
  o.value('soft', '温和：A=75 ms，B=40 ms');
  o.value('balanced', '均衡：A=120 ms，B=80 ms');
  o.value('strong', '强：A=250 ms，B=150 ms');
  o.value('custom', '使用下方自定义值');
  o.cfgvalue = function(sid) {
   var a = String(uci.get('netpreference', sid, 'wait_ms') || '120');
   var b = String(uci.get('netpreference', sid, 'delay_ms') || '80');
   return a === '75' && b === '40' ? 'soft' : a === '120' && b === '80' ? 'balanced' : a === '250' && b === '150' ? 'strong' : 'custom';
  };
  o.write = function() {}; o.remove = function() {};
  o.onchange = function(ev, sid, value) {
   var p = { soft: [75, 40], balanced: [120, 80], strong: [250, 150] }[value];
   if (!p) return;
   ['wait_ms', 'delay_ms'].forEach(function(key, ix) {
    var opt = m.lookupOption(key, sid)[0], el = opt && opt.getUIElement(sid);
    if (el) el.setValue(String(p[ix]));
   });
  };

  o = d.option(form.Value, 'wait_ms', 'A：等待优先协议族证据（毫秒）');
  o.datatype = 'range(0,750)'; o.default = '120'; o.rmempty = false; o.modalonly = true;
  o.description = '从收到查询开始，最多等待优先协议族结果的时间。若 AAAA 不存在或明确失败，不会因此压制有效的 IPv4 结果。';

  o = d.option(form.Value, 'delay_ms', 'B：确认优先协议族可用后额外延迟（毫秒）');
  o.datatype = 'range(0,500)'; o.default = '80'; o.rmempty = false; o.modalonly = true;
  o.description = 'A+B 总和不能超过 1000 ms；不允许无限等待。';

  o = d.option(form.Flag, 'probe', '主动探测优先记录类型');
  o.default = '1'; o.rmempty = false; o.modalonly = true;
  o.description = 'IPv6 优先时主动探测 AAAA，IPv4 优先时主动探测 A。关闭后，只利用并行或近期查询提供的证据。主动探测不会让只支持 IPv4 的应用自动改用 IPv6。';

  o = d.option(form.Value, 'upstream', '设备专用上游 DNS'); o.modalonly = true;
  o.placeholder = '127.0.0.1:53';
  o.description = '可选。传输失败、SERVFAIL 或 REFUSED 时会回退到原始 DNS 链。';

  var r = m.section(form.GridSection, 'rule', '域名规则');
  r.addremove = true; r.anonymous = true; r.sortable = true;
  r.description = '“全部已加入设备”只作用于 NetPreference 管理的设备。设备级规则优先于全局规则；随后按最长域名/精确匹配决定，仍相同时按列表顺序。*.example.com 包含根域 example.com 。
  o = r.option(form.Flag, 'enabled', '启用'); o.default = '1'; o.rmempty = false;
  o = r.option(form.Value, 'device', '作用舃围（MAC 或 *）'); o.default = '*'; o.rmempty = false;
  o.value('*', '全部已加入设备�);
  uci.sections('netpreference', 'device', function(s) { if (s.mac) op.value(s.mac, s.name || s.mac); });

  o = r.option(form.Value, 'domain', '域名'); o.placeholder = '*.example.com'; o.rmempty = false;
  o = r.option(form.ListValue, 'qtype', '查询类型'); o.default = '*'; o.rmempty = false;
  ['*', 'A', 'AAAA', 'HTTPS', 'SVCB'].forEach(function(v) { o.value(v); });

  o = r.option(form.ListValue, 'action', '动作'); o.default = 'nxdomain'; o.rmempty = false;
  [['forward', '转发 / 指定上游'], ['rewrite', '静态地址重写'], ['nxdomain', '返回 NXDOMAIN'], ['nodata', '返回 NODATA'], ['refused', '返回 REFUSED'], ['sinkhole', '黑洞到 0.0.0.0 / ::'], ['ipv4_only', '仅 IPv4：AAAA 返回 NODATA'], ['ipv6_only', '仅 IPv6：A 返回 NODATA']].forEach(function(v) { o.value(v[0], v[1]); });

  o = r.option(form.DynamicList, 'ipv4', '静态 IPv4 地址'); o.datatype = 'ip4addr'; o.depends('action', 'rewrite'); o.modalonly = true;
  o = r.option(form.DynamicList, 'ipv6', '静态 IPv6 地址'); o.datatype = 'ip6addr'; o.depends('action', 'rewrite'); o.modalonly = true;
  o = r.option(form.Value, 'upstream', '规则专用上游 DNS'); o.depends('action', 'forward'); o.modalonly = true;
  o = r.option(form.Value, 'ttl', '覆盖 TTL（秒）'); o.datatype = 'range(0,86400)'; o.default = '60'; o.rmempty = false; o.modalonly = true;

  this.statusBox = E('div', { 'class': 'cbi-section', 'style': 'margin-bottom:1em;' }, '正在读取运行状态…');
  this.trafficBox = E('div', { 'class': 'cbi-section' });

  var restore = E('button', {
   'class': 'cbi-button cbi-button-reset',
   'click': ui.createHandlerFn(this, function() {
    return callRestore().then(checked).then(function() {
     uci.unload('netpreference');
     return uci.load('netpreference');
    }).then(function() { return self.refresh(); }).catch(function(err) {
     ui.addNotification(null, E('p', {}, err.message || String(err)), 'error');
    });
   })
  }, '恢复原始 DNS / 停用插件');

  poll.add(function() { return self.refresh(); }, 3);

  return m.render().then(function(node) {
   self.refresh();
   return E('div', {}, [
    E('div', { 'class': 'cbi-section' }, [
     E('h2', {}, 'NetPreference 网络偏好'),
     E('p', {}, '按设备管理 DNS 双栈偏好、域名规则与 IPv4 / IPv6 流量观察。默认状态不会接管任何设备。')
    ]),
    self.statusBox,
    E('div', { 'class': 'cbi-section' }, [
     E('h3', {}, '安全恢复'),
     restore,
     E('p', { 'style': 'margin-top:.8em;opacity:.75;' }, '恢复操作只删除 NetPreference 自己创建的规则，不会覆盖 HomeProxy、SmartDNS、防火墙或 nlbwmon 的现有配置。DoH/DoT、VPN 内 DNS 与客户端自身缓存不属于本插件的 53 端口策略范围。')
    ]),
    node,
    self.trafficBox
   ]);
  });
 },

 refresh: function() {
  var self = this;
  return Promise.all([callStatus(), callTraffic()]).then(function(res) {
   var s = checked(res[0]), t = checked(res[1]);
   var state = statusText(s);
   var stateNote = s.active ? '已选设备的 DNS 策略正在工作' : s.desired ? '为避免断网，当前保持故障开放' : '后台服务运行，但没有启用策略';

   dom.content(self.statusBox, [
    E('div', { 'style': 'display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;margin-bottom:12px;' }, [
     E('div', {}, [
      E('h3', { 'style': 'margin:0;' }, '运行概览'),
      E('div', { 'style': 'opacity:.7;margin-top:4px;' }, 'NetPreference ' + (s.version || ''))
     ]),
     E('div', { 'style': 'font-weight:600;' }, state)
    ]),
    E('div', { 'style': 'display:flex;gap:10px;flex-wrap:wrap;' }, [
     card('运行状态', state, stateNote),
     card('已选设备', s.selected_devices || 0, '当前受策略管理的设备数'),
     card('DNS 查询', s.queries || 0, 'NetPreference 已处理查询'),
     card('回退次数', s.fallbacks || 0, '回退到原始 DNS 链')
    ]),
    (s.error || s.discovery_warning) ? E('div', {
     'class': 'alert-message warning',
     'style': 'margin-top:12px;'
    }, s.error || s.discovery_warning) : ''
   ]);

   if (!t.enabled) {
    dom.content(self.trafficBox, [
     E('h3', {}, '流量观察'),
     E('p', {}, '流量观察当前关闭。系统原有 nlbwmon 会继续正常运行，不会被 NetPreference 停止或重置。')
    ]);
    return;
   }

   var table = E('table', { 'class': 'table' }, E('tr', { 'class': 'tr table-titles' },
    ['设备', 'IPv4 实时', 'IPv6 实时', 'IPv4 累计', 'IPv6 累计', 'IPv6 占比'].map(function(h) {
     return E('th', { 'class': 'th' }, h);
    })));

   (t.rows || []).forEach(function(row) {
    var h = row.host, ip = (h.ipv4 || []).concat(h.ipv6 || []).join(', ');
    table.appendChild(E('tr', { 'class': 'tr' }, [
     E('td', { 'class': 'td', 'title': ip }, [E('strong', {}, h.name || h.mac), E('br'), E('small', {}, h.mac)]),
     E('td', { 'class': 'td' }, rate(row.ipv4_rate)),
     E('td', { 'class': 'td' }, rate(row.ipv6_rate)),
     E('td', { 'class': 'td' }, total(row.totals.ipv4, t.totals_available)),
     E('td', { 'class': 'td' }, total(row.totals.ipv6, t.totals_available)),
     E('td', { 'class': 'td' }, t.totals_available ? (100 * row.ipv6_ratio).toFixed(1) + '%' : '—')
    ]));
   });

   var body = [
    E('h3', {}, '流量观察'),
    E('p', {}, '累计流量表示当前 nlbwmon 统计周期，不是设备终身累计；实时速率表示采样窗口内经过路由软件路径的字节数。')
   ];
   if (t.caveat || t.error) body.push(E('p', { 'style': 'opacity:.75;' }, (t.caveat || '') + ' ' + (t.error || '')));
   if ((t.rows || []).length) body.push(table);
   else body.push(E('p', {}, '暂时没有可显示的设备流量数据。'));
   dom.content(self.trafficBox, body);
  }).catch(function(err) {
   dom.content(self.statusBox, E('div', { 'class': 'alert-message error' }, '服务不可用：' + (err.message || String(err))));
  });
 },

 handleSaveApply: function() {
  var self = this;
  return this.map.save().then(function() { return callCommit('netpreference'); }).then(function(code) {
   if (typeof code === 'number' && code !== 0) throw new Error('UCI 提交失败：' + code);
   return callApply();
  }).then(checked).then(function() {
   ui.addNotification(null, E('p', {}, 'NetPreference 配置已保存并应用。'), 'info');
   return self.refresh();
  }).catch(function(err) {
   ui.addNotification(null, E('p', {}, err.message || String(err)), 'error');
  });
 },

 handleSave: null
});
