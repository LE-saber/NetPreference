'use strict';
// Run against uci.js extracted from the exact official target rootfs. The ubus
// transport is a bounded in-memory fixture; persistence is also round-tripped
// through the target /sbin/uci and the installed SDK binary in a temp config dir.
const assert=require('assert'),fs=require('fs'),path=require('path'),cp=require('child_process');
const root=path.resolve(process.argv[2]),project=path.resolve(__dirname,'..');
const src=fs.readFileSync(path.join(root,'www/luci-static/resources/uci.js'),'utf8');
let db={main:{'.type':'global','.name':'main','.index':0,enabled:'1',interface:['br-lan']},
 pc:{'.type':'device','.name':'pc','.index':1,enabled:'1',mac:'02:00:00:00:00:01',mode:'ipv4',wait_ms:'120',delay_ms:'80'}};
const clone=x=>JSON.parse(JSON.stringify(x));
const rpc={declare(spec){return async(...args)=>{
 const [config,sid,opts]=args;assert.equal(config,'netpreference');
 switch(spec.method){
 case 'get':return clone(db);
 case 'add':{const [,type,name,values]=args;assert(name);assert(!db[name]);db[name]={'.type':type,'.name':name,'.index':Object.keys(db).length,...clone(values)};return name;}
 case 'set':Object.assign(db[sid],clone(opts));return 0;
 case 'delete':if(!opts)delete db[sid];else opts.forEach(k=>delete db[sid][k]);return 0;
 case 'order':args[1].forEach((id,i)=>db[id]['.index']=i);return 0;
 default:throw new Error('unexpected target UCI method '+spec.method);
 }
};}};
const baseclass={extend:x=>x};
const document={dispatchEvent(){}},CustomEvent=function(type){this.type=type;};
// Used by older supported LuCI implementations, not by generated stable IDs.
if(!String.prototype.format)String.prototype.format=function(...args){let i=0;return this.replace(/%06x|%s|%d/g,m=>m==='%06x'?Math.floor(args[i++]).toString(16).padStart(6,'0'):String(args[i++]));};
const L={bind:(fn,self,...args)=>fn.bind(self,...args),toArray:x=>Array.isArray(x)?x:[x]};
const uci=new Function('baseclass','rpc','document','CustomEvent','L',src)(baseclass,rpc,document,CustomEvent,L);uci.__init__();
const lib=new Function('baseclass',fs.readFileSync(path.join(project,'files/www/luci-static/resources/netpreference/library.js'),'utf8'))(baseclass);
(async()=>{
 await uci.load('netpreference');
 let m=lib.load(uci),s=lib.newSet(uci,m);s.name='AI services';s.text='openai.com\nclaude.ai';
 let p=lib.newProfile(uci,m);p.name='Daily';
 let r=lib.newRow(uci,m,p);r.input='youtube.com';r.mode='ipv4';r.wait_ms='0';r.delay_ms='0';r.probe='0';
 const id=r.id;
 let other=lib.newRow(uci,m,p);other.kind='set';other.domain_set=s.id;
 lib.stage(uci,m);uci.set('netpreference','pc','profile',p.id);
 let pre=lib.serialize(uci);assert(pre.includes('*.youtube.com'));
 await uci.save();lib.order(uci,m);await uci.save();
 assert(db[s.id]&&db[p.id]&&db[id],'named references changed on save');
 assert.equal(db[id].wait_ms,'0');assert.equal(db[id].probe,'0');
 // Old domain deletion must disappear from both preflight text and native UCI.
 m=lib.load(uci);p=m.profiles[0];s=m.sets[0];p.rows[0].kind='set';p.rows[0].domain_set=s.id;
 p.rows.reverse();lib.stage(uci,m);pre=lib.serialize(uci);
 assert(!pre.includes('youtube.com'),'deleted selector leaked into validation');
 await uci.save();lib.order(uci,m);await uci.save();assert.equal(db[id].domain,undefined);assert.equal(db[id].domain_set,s.id);
 let rows=uci.sections('netpreference','policy');assert.equal(rows[0]['.name'],other.id);
 // Remove one rule and rename the set without renaming its stable reference.
 m=lib.load(uci);m.profiles[0].rows.pop();m.sets[0].name='Renamed';lib.stage(uci,m);await uci.save();lib.order(uci,m);await uci.save();
 assert(!db[id]);assert.equal(db[other.id].domain_set,s.id);
 const text=lib.serialize(uci);
 fs.mkdirSync(path.join(root,'tmp/np-ui-config'),{recursive:true});
 cp.execFileSync('chroot',[root,'/sbin/uci','-c','/tmp/np-ui-config','import','netpreference'],{input:text});
 cp.execFileSync('chroot',[root,'/sbin/uci','-c','/tmp/np-ui-config','commit','netpreference']);
 const exported=cp.execFileSync('chroot',[root,'/sbin/uci','-c','/tmp/np-ui-config','export','netpreference'],{encoding:'utf8'});
 const validation=JSON.parse(cp.execFileSync('chroot',[root,'/usr/sbin/netpreference','rpc','call','check_config'],{input:JSON.stringify({config:exported}),encoding:'utf8'}));
 assert(validation.valid);assert(exported.includes("'*.openai.com'"));
 fs.mkdirSync(path.join(project,'dist-sdk/target-luci'),{recursive:true});
 fs.writeFileSync(path.join(project,'dist-sdk/target-luci/uci.js'),src);
 fs.writeFileSync(path.join(project,'dist-sdk/target-luci/roundtrip.uci'),exported);
 console.log('PASS: official target uci.js create/rename/selector-delete/reorder/remove/save, native target uci import/export and installed SDK parser. Ubus transport fixture, no live router web session.');
})().catch(err=>{console.error(err);process.exit(1);});
