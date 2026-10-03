'use strict';
const assert=require('assert'),fs=require('fs'),path=require('path'),cp=require('child_process');
const {create}=require('./luci_harness');
const ROOT=path.join(__dirname,'..'), BASE=path.join(ROOT,'files/www/luci-static/resources');
function E(tag,attrs,children){return {tag,attrs:attrs||{},children:children||[],appendChild(c){if(!Array.isArray(this.children))this.children=[this.children];this.children.push(c);}};}
function validate(text){
 const res=cp.spawnSync(path.join(ROOT,'dist/netpreference'),['rpc','call','check_config'],{input:JSON.stringify({config:text}),encoding:'utf8'});
 if(res.error)throw res.error;
 return JSON.parse(res.stdout);
}
const h=create(E,validate),lib=h.module(fs.readFileSync(path.join(BASE,'netpreference/library.js'),'utf8'));
function view(name){return h.loadView(fs.readFileSync(path.join(BASE,'view/netpreference',name+'.js'),'utf8'),lib);}
function methods(){return h.calls.map(c=>c[1]);}
(async()=>{
 const basic=view('overview');await basic.render(await basic.load());await basic.refresh();
 assert(JSON.stringify(basic.trafficBox).includes('70.0%'));
 assert(!basic.map.sections.some(s=>s.sectiontype==='rule'));
 assert.deepEqual(h.option(basic,'device','profile').choices.find(c=>c[0]==='work'),['work','Work']);
 assert(h.option(basic,'device','mode').choices.some(c=>c[0]==='block'));
 assert(h.option(basic,'device','prefer').retain);
 h.option(basic,'device','_preset').onchange(null,'pc','strong');
 assert.equal(h.values['pc/wait_ms'],'250');assert.equal(h.values['pc/delay_ms'],'150');
 h.calls.length=0;await basic.handleSaveApply();
 assert.deepEqual(methods().slice(0,5),['save','check_config','save','commit','apply']);
 const status=JSON.stringify(basic.statusBox);
 h.responses.traffic=new Error('counter RPC unavailable');await basic.refresh();
 assert.equal(JSON.stringify(basic.statusBox),status,'traffic RPC must not falsify DNS service status');
 assert(JSON.stringify(basic.trafficBox).includes('counter RPC unavailable'));
 h.responses.traffic={enabled:true,state:'unavailable',totals_available:false,error:'missing counters',rows:[]};await basic.refresh();
 assert(JSON.stringify(basic.trafficBox).includes('missing counters'));
 h.responses.traffic={enabled:false};await basic.refresh();
 assert(JSON.stringify(basic.trafficBox).includes('NetPreference'));

 const advanced=view('advanced');await advanced.load();await advanced.render();
 assert.deepEqual(advanced.map.sections.map(s=>s.sectiontype),['rule'],'only optional DNS actions use a backend grid');
 assert(!JSON.stringify(advanced.editor).includes('stable ID'));
 const actions=h.option(advanced,'rule','action').choices.map(v=>v[0]);
 for(const action of ['rewrite','nxdomain','nodata','refused','sinkhole','forward','ipv4_only','ipv6_only'])assert(actions.includes(action));
 for(const k of ['ipv4','ipv6','upstream'])assert(h.option(advanced,'rule',k).retain);
 let model=advanced.model;
 const originalRule=JSON.stringify(h.sections.block);
 // Round-trip old r1 records without silently broadening exact patterns.
 h.calls.length=0;await advanced.handleSave();
 assert.deepEqual(methods(),['save','check_config','save','commit']);
 assert.deepEqual(h.sections.media.domain,['*.media.test','exact.test']);
 assert.equal(h.sections.work.description,'Retain this note');
 assert.equal(h.sections.bias.wait_ms,'0');assert.equal(h.sections.bias.probe,'0');
 assert.equal(JSON.stringify(h.sections.block),originalRule);
 model=advanced.model;
 const set=lib.newSet(h.uci,model);set.name='AI\u670d\u52a1';set.text=' OpenAI.com \nhttps://anthropic.com/docs\nclaude.ai\uff0copenai.com';
 const profile=lib.newProfile(h.uci,model);profile.name='\u65e5\u5e38\u6a21\u5f0f';
 let row=lib.newRow(h.uci,model,profile);row.kind='set';row.domain_set=set.id;row.mode='ipv6';
 row=lib.newRow(h.uci,model,profile);row.input='youtube.com';row.mode='ipv4';row.wait_ms='0';row.delay_ms='160';
 row=lib.newRow(h.uci,model,profile);row.input='games.test';row.mode='custom';row.prefer='ipv6';row.wait_ms='40';row.delay_ms='90';row.probe='0';
 row=lib.newRow(h.uci,model,profile);row.input='example.com';row.mode='dual';
 h.uci.set('netpreference','pc','profile',profile.id);
 h.calls.length=0;await advanced.handleSaveApply();
 assert.deepEqual(methods(),['save','check_config','save','commit','apply']);
 assert.deepEqual(h.sections[set.id].domain,['*.openai.com','*.anthropic.com','*.claude.ai']);
 const config=lib.serialize(h.uci);
 fs.mkdirSync(path.join(ROOT,'dist'),{recursive:true});fs.writeFileSync(path.join(ROOT,'dist/ux-generated.uci'),config);
 assert(!config.includes("option 'wait_ms' ''"),'unset inheritance is not a literal empty option');
 assert.equal(h.sections.pc.mode,'ipv4','profile must not overwrite device global mode');
 assert.equal(h.sections.pc.wait_ms,'120');
 // Rename both libraries, change an existing direct selector to a set, then
 // round-trip: no name is used as an identifier and no stale selector remains.
 model=advanced.model;model.sets.find(s=>s.id===set.id).name='Renamed AI';
 let p=model.profiles.find(p=>p.id===profile.id);p.name='Renamed daily';
 p.rows[1].kind='set';p.rows[1].domain_set=set.id;
 h.calls.length=0;await advanced.handleSave();
 assert(methods().includes('commit'));assert.equal(h.sections.pc.profile,profile.id);
 assert.equal(h.sections[p.rows[1].id].domain,undefined);
 assert.equal(h.sections[p.rows[1].id].domain_set,set.id);
 // Backend, not JavaScript heuristics, is authoritative before persistence.
 model=advanced.model;p=model.profiles.find(p=>p.id===profile.id);
 p.rows[0].wait_ms='750';p.rows[0].delay_ms='500';
 h.calls.length=0;await advanced.handleSaveApply();
 assert(methods().includes('check_config'));assert(!methods().includes('commit'));assert(!methods().includes('apply'));assert(!methods().includes('save',1));
 p.rows[0].wait_ms='';p.rows[0].delay_ms='';
 const doomed=lib.newProfile(h.uci,model);doomed.name='Temporary';const invalid=lib.newRow(h.uci,model,doomed);invalid.input='a.test';invalid.wait_ms='9999';
 await advanced.handleSave();
 model.profiles=model.profiles.filter(p=>p!==doomed);
 h.calls.length=0;await advanced.handleSave();
 assert(methods().includes('commit'),'removing a rejected new profile must remove its staged sections too');
 assert(!h.sections[doomed.id]);assert(!h.sections[invalid.id]);
 // Referenced library deletion is blocked before any persistent write.
 model=advanced.model;model.profiles=model.profiles.filter(p=>p.id!==profile.id);
 h.calls.length=0;await advanced.handleSave();assert(!methods().includes('commit'));
 assert.equal(JSON.stringify(h.sections.block),originalRule);
 assert(!h.calls.some(x=>x[0]==='uci'&&x[1]==='apply'));
 console.log('PASS: actual views + pure model + backend preflight; generated stable IDs, named sets, unnamed rules, inheritance, old actions, scoped persistence, full traffic UI contract. API doubles, not target-router acceptance.');
})().catch(e=>{console.error(e);process.exit(1);});
