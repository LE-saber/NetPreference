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
 if (result && result.ok === false) throw new Error(result.error || _('Operation failed'));
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
 return r && r.valid ? bytes(r.upload_Bps) + '/s \u2191  ' + bytes(r.download_Bps) + '/s \u2193' : _('Waiting for two valid samples');
}
function total(t, available) {
 return available ? bytes(t.upload_bytes) + ' \u2191  ' + bytes(t.download_bytes) + ' \u2193' : _('Unavailable');
}

return view.extend({
 load: function() {
  return Promise.all([uci.load('netpreference'), callDevices().catch(function() { return { devices: [] }; })]);
 },
 render: function(data) {
  var self = this, known = data[1].devices || [];
  var m = this.map = new form.Map('netpreference', _('NetPreference'),
   _('Only enrolled devices are redirected. The existing dnsmasq \u2192 HomeProxy \u2192 SmartDNS chain remains the default upstream. DNS preference influences answer timing; it does not force an application to use IPv6 or IPv4.'));
  var g = m.section(form.NamedSection, 'main', 'global', _('General'));
  var o = g.option(form.DynamicList, 'interface', _('Trusted LAN interfaces'));
  o.default = 'br-lan'; o.rmempty = false;
  o = g.option(form.Value, 'upstream', _('Original DNS chain upstream'));
  o.default = '127.0.0.1:53'; o.rmempty = false;
  o.description = _('Literal IP:port, tcp://IP:port or udp://IP:port. IPv6 requires brackets. Port 1053 is reserved.');
  o = g.option(form.Value, 'timeout_ms', _('Upstream timeout (ms)'));
  o.default = '2000'; o.datatype = 'range(100,5000)'; o.rmempty = false;
  o = g.option(form.Flag, 'monitor', _('Enable traffic observation'));
  o.default = '0'; o.rmempty = false;
  o.description = _('Reads the existing nlbwmon database without resetting it. Live rates use optional nft counters and can undercount with flow/hardware offload. No offload or nlbwmon settings are changed.');

  var d = m.section(form.GridSection, 'device', _('Devices'));
  d.addremove = true; d.anonymous = true; d.sortable = true;
  o = d.option(form.Flag, 'enabled', _('Enabled')); o.default = '1'; o.rmempty = false;
  o = d.option(form.Value, 'name', _('Name')); o.datatype = 'maxlength(80)';
  o = d.option(form.Value, 'mac', _('Device MAC')); o.datatype = 'macaddr'; o.rmempty = false;
  known.forEach(function(h) { o.value(h.mac, (h.name || h.mac) + ' (' + (h.ipv4 || []).join(', ') + ')'); });
  o = d.option(form.ListValue, 'mode', _('Policy')); o.default = 'dual'; o.rmempty = false;
  [['dual', _('Ordinary dual stack')], ['ipv6', _('Prefer IPv6')], ['ipv4', _('Prefer IPv4')], ['custom', _('Custom preference')], ['block', _('Block DNS (REFUSED)')]].forEach(function(v) { o.value(v[0], v[1]); });
  o = d.option(form.ListValue, 'prefer', _('Custom preferred family'));
  o.value('ipv6', 'IPv6'); o.value('ipv4', 'IPv4'); o.default = 'ipv6'; o.depends('mode', 'custom'); o.modalonly = true;
  o = d.option(form.ListValue, '_preset', _('Timing preset')); o.modalonly = true;
  o.value('soft', _('Gentle: A=75, B=40 ms')); o.value('balanced', _('Balanced: A=120, B=80 ms'));
  o.value('strong', _('Strong: A=250, B=150 ms')); o.value('custom', _('Custom values below'));
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
  o = d.option(form.Value, 'wait_ms', _('A: preferred-family evidence wait (ms)'));
  o.datatype = 'range(0,750)'; o.default = '120'; o.rmempty = false; o.modalonly = true;
  o.description = _('Maximum wait from query arrival for a preferred-family answer. Missing or negative AAAA never suppresses a valid IPv4 answer.');
  o = d.option(form.Value, 'delay_ms', _('B: additional delay when preferred evidence is positive (ms)'));
  o.datatype = 'range(0,500)'; o.default = '80'; o.rmempty = false; o.modalonly = true;
  o.description = _('A+B must not exceed 1000 ms. Unlimited waits are rejected.');
  o = d.option(form.Flag, 'probe', _('Actively probe preferred record type'));
  o.default = '1'; o.rmempty = false; o.modalonly = true;
  o.description = _('IPv6 mode probes AAAA; IPv4 mode probes A. Without probing, only parallel/recent queries provide evidence. A probe does not make an IPv4-only application use IPv6.');
  o = d.option(form.Value, 'upstream', _('Per-device upstream')); o.modalonly = true;
  o.placeholder = '127.0.0.1:53'; o.description = _('Optional. Transport failure, SERVFAIL or REFUSED falls back to the original DNS chain.');

  var r = m.section(form.GridSection, 'rule', _('Domain rules'));
  r.addremove = true; r.anonymous = true; r.sortable = true;
  r.description = _('Global means all enrolled devices only. Device scope beats global scope, then longest domain/exact match wins; ties use list order. *.example.com includes the apex. Whole-device blocking wins over domain rules.');
  o = r.option(form.Flag, 'enabled', _('Enabled')); o.default = '1'; o.rmempty = false;
  o = r.option(form.Value, 'device', _('Scope (MAC or *)')); o.default = '*'; o.rmempty = false;
  o.value('*', _('All enrolled devices'));
  uci.sections('netpreference', 'device', function(s) { if (s.mac) o.value(s.mac, s.name || s.mac); });
  o = r.option(form.Value, 'domain', _('Domain')); o.placeholder = '*.example.com'; o.rmempty = false;
  o = r.option(form.ListValue, 'qtype', _('Query type')); o.default = '*'; o.rmempty = false;
  ['*', 'A', 'AAAA', 'HTTPS', 'SVCB'].forEach(function(v) { o.value(v); });
  o = r.option(form.ListValue, 'action', _('Action')); o.default = 'nxdomain'; o.rmempty = false;
  [['forward', _('Forward / upstream override')], ['rewrite', _('Static address override')], ['nxdomain', 'NXDOMAIN'], ['nodata', 'NODATA'], ['refused', 'REFUSED'], ['sinkhole', _('Sinkhole 0.0.0.0 / ::')], ['ipv4_only', _('NODATA for AAAA')], ['ipv6_only', _('NODATA for A')]].forEach(function(v) { o.value(v[0], v[1]); });
  o = r.option(form.DynamicList, 'ipv4', _('Static IPv4 addresses')); o.datatype = 'ip4addr'; o.depends('action', 'rewrite'); o.modalonly = true;
  o = r.option(form.DynamicList, 'ipv6', _('Static IPv6 addresses')); o.datatype = 'ip6addr'; o.depends('action', 'rewrite'); o.modalonly = true;
  o = r.option(form.Value, 'upstream', _('Rule upstream')); o.depends('action', 'forward'); o.modalonly = true;
  o = r.option(form.Value, 'ttl', _('Override TTL (seconds)')); o.datatype = 'range(0,86400)'; o.default = '60'; o.rmempty = false; o.modalonly = true;

  this.statusBox = E('div', { 'class': 'cbi-section' }, _('Loading status...'));
  this.trafficBox = E('div', { 'class': 'cbi-section' });
  var restore = E('button', { 'class': 'cbi-button cbi-button-reset', 'click': ui.createHandlerFn(this, function() {
   return callRestore().then(checked).then(function() {
    uci.unload('netpreference');
    return uci.load('netpreference');
   }).then(function() { return self.refresh(); }).catch(function(err) { ui.addNotification(null, E('p', {}, err.message || String(err)), 'error'); });
  }) }, _('Restore original DNS / disable plugin'));
  poll.add(function() { return self.refresh(); }, 3);
  return m.render().then(function(node) {
   self.refresh();
   return E('div', {}, [self.statusBox, E('div', { 'class': 'cbi-section' }, [restore,
    E('p', {}, _('Restore removes only NetPreference-owned rules. It does not overwrite saved HomeProxy, SmartDNS, firewall or nlbwmon configuration. DoH/DoT, VPN DNS and client caches are outside this DNS-port policy.'))]), node, self.trafficBox]);
  });
 },
 refresh: function() {
  var self = this;
  return Promise.all([callStatus(), callTraffic()]).then(function(res) {
   var s = checked(res[0]), t = checked(res[1]);
   dom.content(self.statusBox, [E('h3', {}, 'NetPreference ' + (s.version || '')),
    E('p', {}, (s.active ? _('Active') : s.desired ? _('Fail-open / waiting for healthy DNS') : _('Idle / restored')) +
     ' | ' + _('Selected devices') + ': ' + (s.selected_devices || 0) + ' | DNS: ' + (s.queries || 0) + ' | ' + _('Fallbacks') + ': ' + (s.fallbacks || 0)),
    E('p', {}, s.error || s.discovery_warning || '')]);
   if (!t.enabled) { dom.content(self.trafficBox, E('p', {}, _('Traffic observation is off. Existing nlbwmon continues unchanged.'))); return; }
   var table = E('table', { 'class': 'table' }, E('tr', { 'class': 'tr table-titles' },
    [_('Device'), _('IPv4 live'), _('IPv6 live'), _('IPv4 cumulative'), _('IPv6 cumulative'), _('IPv6 share')].map(function(h) { return E('th', { 'class': 'th' }, h); })));
   (t.rows || []).forEach(function(row) {
    var h = row.host, ip = (h.ipv4 || []).concat(h.ipv6 || []).join(', ');
    table.appendChild(E('tr', { 'class': 'tr' }, [
     E('td', { 'class': 'td', 'title': ip }, [E('strong', {}, h.name || h.mac), E('br'), E('small', {}, h.mac)]),
     E('td', { 'class': 'td' }, rate(row.ipv4_rate)), E('td', { 'class': 'td' }, rate(row.ipv6_rate)),
     E('td', { 'class': 'td' }, total(row.totals.ipv4, t.totals_available)), E('td', { 'class': 'td' }, total(row.totals.ipv6, t.totals_available)),
     E('td', { 'class': 'td' }, t.totals_available ? (100 * row.ipv6_ratio).toFixed(1) + '%' : '\u2014')
    ]));
   });
   dom.content(self.trafficBox, [E('h3', {}, _('Traffic observation')), E('p', {}, _('Cumulative = the active nlbwmon accounting period, not lifetime usage. Live rates = routed software-path bytes over the sampling window.')),
    E('p', {}, (t.caveat || '') + ' ' + (t.error || '')), table]);
  }).catch(function(err) { dom.content(self.statusBox, E('p', {}, _('Service unavailable: ') + (err.message || String(err)))); });
 },
 handleSaveApply: function() {
  var self = this;
  return this.map.save().then(function() { return callCommit('netpreference'); }).then(function(code) {
   if (typeof code === 'number' && code !== 0) throw new Error(_('UCI commit failed: ') + code);
   return callApply();
  }).then(checked).then(function() { return self.refresh(); }).catch(function(err) {
   ui.addNotification(null, E('p', {}, err.message || String(err)), 'error');
  });
 },
 handleSave: null
});
