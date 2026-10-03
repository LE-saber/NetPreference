'use strict';
'require view';
'require form';
'require rpc';
'require uci';
'require ui';
'require dom';
'require netpreference.library as library';

var callApply = rpc.declare({object:'netpreference',method:'apply',expect:{}});
var callCheck = rpc.declare({object:'netpreference',method:'check_config',params:['config'],expect:{}});
var callCommit = rpc.declare({object:'uci',method:'commit',params:['config']});
function checked(r) { if (!r || r.ok===false) throw new Error(r && r.error || 'Request failed');return r; }
function reference(section, key, title, type, optional) {
 var o = section.option(form.ListValue, key, title);
 o.rmempty = optional; o.optional = optional;
 // Rebuild on each render, including after adding a named section in this form.
 o.load = function(sid) {
  this.keylist = []; this.vallist = [];
  this.value('', optional ? '不指定' : '请选择');
  var self = this;
  uci.sections('netpreference', type, function(s) {
   self.value(s['.name'], s.name || '未命名');
  });
  return form.ListValue.prototype.load.call(this, sid);
 };
 o.validate = function(sid, value) {
  if (!value) return optional ? true : '必须选择一项';
  var target = uci.get('netpreference', value);
  return target && target['.type'] === type ? true : '引用不存在，请先创建并保存';
 };
 return o;
}
function domainValid(value) {
 value = (value || '').trim().toLowerCase();
 if (value === '*.') return false;
 value = value.replace(/\.$/, '');
 if (value === '*') return true;
 value = value.replace(/^\*\./, '');
 return value.length > 0 && value.length <= 253 && value.split('.').every(function(label) {
  return /^[a-z0-9_-]{1,63}$/.test(label);
 });
}
function selector(section) {
 // Both fields stay visible: no hidden dependency can delete an old selector.
 var o = section.option(form.Value, 'domain', '直接填写域名');
 o.placeholder = '*.example.com'; o.optional = true;
 o.description = '与域名集二选一。*.example.com 包含根域；* 匹配所有域名。国际化域名请使用 punycode。';
 o.validate = function(sid, value) { return !value || domainValid(value) ? true : '域名格式无效'; };
 reference(section, 'domain_set', '或选择命名域名集', 'domain_set', true);
}
function enabled(section) { var o=section.option(form.Flag,'enabled','启用');o.default='1';o.rmempty=false; }

/* EDITOR */
function button(label,handler,attrs) {
 return E('button',Object.assign({type:'button','class':'cbi-button',click:handler},attrs||{}),label);
}
function textbox(value,handler,attrs) {
 return E('input',Object.assign({type:'text','class':'cbi-input-text',value:value,input:function(e){handler(e.target.value);}},attrs||{}));
}
function select(value,choices,handler,attrs) {
 return E('select',Object.assign({'class':'cbi-input-select',change:function(e){handler(e.target.value);}},attrs||{}),choices.map(function(c){return E('option',{value:c[0],selected:c[0]===value ? '' : null},c[1]);}));
}
function field(label,node) { return E('label',{'class':'np-field'},[E('span',{},label),node]); }
function notice(err) {ui.addNotification(null,E('p',{},err.message||String(err)),'error');}
var modes=[['dual','普通双栈'],['ipv6','IPv6 优先'],['ipv4','IPv4 优先'],['custom','自定义 A/B']];

return view.extend({
 load:function(){return uci.load('netpreference');},
 render:function(){
  var self=this;
  this.model=library.load(uci);this.tab='profiles';this.editor=E('div');this.redraw();
  var m=this.map=new form.Map('netpreference','', '');
  var o;
  var r=m.section(form.GridSection,'rule','DNS 动作（兼容旧版规则）');
  r.anonymous=true;r.addremove=true;r.sortable=true;
  r.description='动作与时序分开匹配；偏好规则不会解除阻断。先设备范围，再域名具体度，完全同分时 模式集规则优先，最后按列表顺序。本地阻断和静态响应立即返回，不附加 A/B。';
  enabled(r);reference(r,'profile','仅在此模式集下生效','profile',true);
  o=r.option(form.Value,'device','设备范围（MAC 或 *）');o.default='*';o.rmempty=false;o.value('*','全部已加入设备');
  uci.sections('netpreference','device',function(s){if(s.mac)o.value(s.mac,s.name||s.mac);});
  selector(r);
  o=r.option(form.ListValue,'qtype','查询类型');o.default='*';o.rmempty=false;['*','A','AAAA','HTTPS','SVCB'].forEach(function(v){o.value(v);});
  o=r.option(form.ListValue,'action','DNS 动作');o.default='nxdomain';o.rmempty=false;
  [['forward','转发 / 指定上游'],['rewrite','静态地址重写'],['nxdomain','NXDOMAIN'],['nodata','NODATA'],['refused','REFUSED'],['sinkhole','0.0.0.0 / ::'],['ipv4_only','仅 IPv4（AAAA NODATA）'],['ipv6_only','仅 IPv6（A NODATA）']].forEach(function(v){o.value(v[0],v[1]);});
  o=r.option(form.DynamicList,'ipv4','静态 IPv4');o.datatype='ip4addr';o.depends('action','rewrite');o.modalonly=true;o.retain=true;
  o=r.option(form.DynamicList,'ipv6','静态 IPv6');o.datatype='ip6addr';o.depends('action','rewrite');o.modalonly=true;o.retain=true;
  o=r.option(form.Value,'upstream','规则上游 DNS');o.placeholder='127.0.0.1:53';o.depends('action','forward');o.modalonly=true;o.retain=true;
  o=r.option(form.Value,'ttl','TTL（秒）');o.datatype='range(0,86400)';o.default='60';o.rmempty=false;o.modalonly=true;
  return m.render().then(function(node){
   return E('div',{'class':'np-library'},[
    E('style',{},'.np-tabs,.np-row-main,.np-card-head,.np-actions{display:flex;gap:10px;flex-wrap:wrap;align-items:center}.np-tabs{margin:14px 0}.np-card{border:1px solid var(--border-color-medium,#ddd);border-radius:8px;padding:16px;margin:14px 0}.np-card-head{justify-content:space-between;margin-bottom:12px}.np-card-head input{flex:1;max-width:420px}.np-rule{background:var(--background-color-low,#f8f8f8);padding:12px;margin:12px 0;border-radius:6px}.np-field{display:flex;flex-direction:column;gap:6px;min-width:120px;margin:4px 0;flex:1}.np-field>span{font-size:12px;opacity:.8}.np-target{flex:2;min-width:240px}.np-target input{width:100%;margin-top:6px}.np-params{margin-top:10px}.np-params summary{cursor:pointer;font-size:12px}.np-params>div{display:flex;gap:12px;flex-wrap:wrap;margin-top:6px}.np-note{font-size:12px;opacity:.75;margin:8px 0}.np-card textarea{width:100%;min-height:140px;font-family:monospace;box-sizing:border-box}.np-actions{flex:0 0 auto}.np-actions button{min-width:34px}.np-empty{padding:18px;border:1px dashed #bbb;border-radius:6px}.np-dns-actions{margin:24px 0}.np-dns-actions>summary{cursor:pointer;font-weight:600}'),
    E('h2',{},'模式集与域名集'),
    E('p',{},'域名集保存常用域名；模式集将多条域名偏好打包。保存后，在设备页选择一个模式集即可切换。'),
    E('p',{'class':'alert-message notice'},'没有匹配的域名，始终使用该设备的全局默认模式和 A/B。新输入 openai.com 即包含根域及全部子域名，不需要填写 *.。'),
    self.editor,
    E('details',{'class':'np-dns-actions'},[E('summary',{},'更多：DNS 阻断、静态重写和指定上游'),E('p',{'class':'np-note'},'保留旧版 DNS 动作及匹配语义。新建的域名集请先保存，再在这里引用。'),node])
   ]);
  });
 },
 redraw:function(){
  var self=this, m=this.model, isSets=this.tab==='sets';
  var tabs=E('div',{'class':'np-tabs'},[
   button('模式集 ('+m.profiles.length+')',function(){self.tab='profiles';self.redraw();},{'class':'cbi-button '+(!isSets?'cbi-button-action':'')}),
   button('域名集 ('+m.sets.length+')',function(){self.tab='sets';self.redraw();},{'class':'cbi-button '+(isSets?'cbi-button-action':'')})
  ]);
  var items=isSets?m.sets:m.profiles;
  var cards=items.map(function(item){return isSets?self.renderSet(item):self.renderProfile(item);});
  if(!cards.length)cards.push(E('p',{'class':'np-empty'},isSets?'还没有域名集。添加名称和域名即可。':'还没有模式集。新建后可以直接添加域名偏好规则。'));
  dom.content(this.editor,[tabs,E('div',{},cards),button(isSets?'+ 添加域名集':'+ 添加模式集',function(){
   if(isSets)library.newSet(uci,m);else library.newProfile(uci,m);self.redraw();
  },{'class':'cbi-button cbi-button-add','data-action':isSets?'add-set':'add-profile'})]);
 },
 renderSet:function(s){
  var self=this;
  var legacy=!s.dirty && s.original.some(function(p){return p!=='*' && p.indexOf('*.')!==0;});
  return E('section',{'class':'np-card','data-set':s.id},[
   E('div',{'class':'np-card-head'},[
    textbox(s.name,function(v){s.name=v;},{placeholder:'域名集名称，例如 AI服务','aria-label':'域名集名称',maxlength:80}),
    button('删除域名集',function(){
     if(self.model.profiles.some(function(p){return p.rows.some(function(r){return r.kind==='set'&&r.domain_set===s.id;});}) || uci.sections('netpreference','rule').some(function(r){return r.domain_set===s.id;}))return notice(new Error('这个域名集仍在使用，请先移除引用'));
     self.model.sets=self.model.sets.filter(function(x){return x!==s;});self.redraw();
    })
   ]),
   E('textarea',{rows:6,placeholder:'openai.com\nanthropic.com\nclaude.ai','aria-label':'域名列表',input:function(e){s.text=e.target.value;s.dirty=true;}},s.text),
   E('p',{'class':'np-note'},'一行一个域名，也可用空格、逗号分隔或直接粘贴网址。自动去重并包含全部子域名。'),
   legacy?E('p',{'class':'np-note'},['此旧集合含精确域名；未编辑时保持旧语义。 ',button('改为包含子域名',function(){s.dirty=true;self.redraw();})]):''
  ]);
 },
 renderProfile:function(p){
  var self=this;
  return E('section',{'class':'np-card','data-profile':p.id},[
   E('div',{'class':'np-card-head'},[
    textbox(p.name,function(v){p.name=v;},{placeholder:'模式集名称，例如 日常模式','aria-label':'模式集名称',maxlength:80}),
    E('span',{},p.rows.length+' 条规则'),
    button('删除模式集',function(){
     if(uci.sections('netpreference').some(function(s){return (s['.type']==='device'||s['.type']==='rule')&&s.profile===p.id;}))return notice(new Error('请先在设备或 DNS 动作中取消使用此模式集'));
     self.model.profiles=self.model.profiles.filter(function(x){return x!==p;});self.redraw();
    })
   ]),
   E('div',{},p.rows.map(function(r,index){return self.renderRule(p,r,index);})),
   button('+ 添加规则',function(){library.newRow(uci,self.model,p);self.redraw();},{'class':'cbi-button cbi-button-add','data-action':'add-rule'}),
   E('p',{'class':'np-note'},'更具体的域名优先；同等具体时上方规则优先。仅使用一条匹配规则，不会叠加 A/B。')
  ]);
 },
 renderRule:function(p,r,index){
  var self=this;
  var choices=[['__direct__','输入域名']].concat(this.model.sets.map(function(s){return [s.id,s.name || '未命名域名集'];}));
  var target=E('div',{'class':'np-target'},[
   field('匹配对象',select(r.kind==='set'?r.domain_set:'__direct__',choices,function(v){
    r.kind=v==='__direct__'?'domain':'set';if(r.kind==='set')r.domain_set=v;else if(!r.original)r.dirty=true;self.redraw();
   },{'aria-label':'输入域名或选择域名集'})),
   r.kind==='domain'?textbox(r.input,function(v){r.input=v;r.dirty=true;},{placeholder:'example.com','aria-label':'直接域名'}):''
  ]);
  var timing=E('details',{'class':'np-params',open:r.mode==='custom'?'':null},[
   E('summary',{},'A/B：'+((r.wait_ms===''&&r.delay_ms==='')?'继承设备（点击自定义）':(r.wait_ms===''?'继承':r.wait_ms)+' / '+(r.delay_ms===''?'继承':r.delay_ms)+' ms')),
   E('div',{},[
    field('A：等待证据 (ms)',textbox(r.wait_ms,function(v){r.wait_ms=v;},{type:'number',min:0,max:750,step:1,placeholder:'继承设备','aria-label':'A'})),
    field('B：额外延迟 (ms)',textbox(r.delay_ms,function(v){r.delay_ms=v;},{type:'number',min:0,max:500,step:1,placeholder:'继承设备','aria-label':'B'})),
    r.mode==='custom'?field('优先协议族',select(r.prefer,[['','随设备'],['ipv6','IPv6'],['ipv4','IPv4']],function(v){r.prefer=v;},{'aria-label':'优先协议族'})):'',
    field('主动探测',select(r.probe,[['','继承设备'],['1','开启'],['0','关闭']],function(v){r.probe=v;}))
   ]),
   E('p',{'class':'np-note'},'留空继承设备；0 表示不等待 / 不额外延迟。A+B 不超过 1000 ms。普通双栈不附加延迟。')
  ]);
  return E('div',{'class':'np-rule'},[
   E('div',{'class':'np-row-main'},[
    target,
    field('偏好模式',select(r.mode,r.mode==='inherit'?[['inherit','继承设备（旧规则）']].concat(modes):modes,function(v){r.mode=v;self.redraw();},{'aria-label':'偏好模式'})),
    E('div',{'class':'np-actions'},[
     E('label',{},[E('input',{type:'checkbox',checked:r.enabled!=='0'?'':null,change:function(e){r.enabled=e.target.checked?'1':'0';}}),'启用']),
     button('↑',function(){p.rows.splice(index,1);p.rows.splice(index-1,0,r);self.redraw();},{disabled:index===0?'':null,title:'上移'}),
     button('↓',function(){p.rows.splice(index,1);p.rows.splice(index+1,0,r);self.redraw();},{disabled:index===p.rows.length-1?'':null,title:'下移'}),
     button('删除',function(){p.rows.splice(index,1);self.redraw();},{title:'删除规则'})
    ])
   ]),
   r.mode!=='dual'?timing:'',
   r.kind==='domain'&&!r.dirty&&r.original&&r.original!=='*'&&r.original.indexOf('*.')!==0?E('p',{'class':'np-note'},['旧规则保留精确匹配。 ',button('包含子域名',function(){r.dirty=true;self.redraw();})]):''
  ]);
 },
 saveLibrary:function(apply){
  var self=this;
  return this.map.save(function(){
   library.stage(uci,self.model);
   return callCheck(library.serialize(uci)).then(checked);
  }).then(function(){return callCommit('netpreference');}).then(function(code){
   if(typeof code==='number'&&code!==0)throw new Error('UCI commit failed: '+code);
   self.model=library.load(uci);self.redraw();
   return apply?callApply().then(checked):null;
  }).then(function(){ui.addNotification(null,E('p',{},apply?'模式库已保存并应用。':'模式库已保存。到设备页选择模式集并应用后生效。'),'info');}).catch(notice);
 },
 handleSave:function(){return this.saveLibrary(false);},
 handleSaveApply:function(){return this.saveLibrary(true);},
 handleReset:function(){
  var self=this;uci.unload('netpreference');
  return uci.load('netpreference').then(function(){self.model=library.load(uci);self.redraw();return self.map.reset();});
 }
});
