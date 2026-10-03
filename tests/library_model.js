'use strict';
const assert=require('assert'),fs=require('fs'),path=require('path');
const source=fs.readFileSync(path.join(__dirname,'../files/www/luci-static/resources/netpreference/library.js'),'utf8');
const lib=new Function('baseclass',source)({extend:x=>x});
for(const value of ['openai.com','OPENAI.COM.','*.openai.com','https://openai.com/chat','openai.com/path','.openai.com','openai\u3002com']){
 assert.equal(lib.domain(value),'*.openai.com',value);
 assert.equal(lib.domain(lib.domain(value)),lib.domain(value));
}
assert.equal(lib.domain('https://\u4f8b\u5b50.\u6d4b\u8bd5/path'),'*.xn--fsqu00a.xn--0zwm56d');
assert.deepEqual(lib.domains('a.test\uff0cb.test\na.test; c.test #not-a-comment-inline'.split(' #')[0]),['*.a.test','*.b.test','*.c.test']);
for(const value of ['', '*.','a..b','a.*.b','http://a:bad','http://user:pass@a','javascript://evil','foo bar','a\\b'])assert.throws(()=>lib.domain(value),undefined,value);
assert.throws(()=>lib.domains(''));
assert.throws(()=>lib.domains(Array.from({length:257},(_,i)=>`d${i}.test`).join('\n')));
// Real LuCI sections() may retain an option pending deletion, while get()
// returns the effective record. Serialization must use the latter.
const uci={sections(){return [{'.name':'p','.type':'policy',domain:'stale.test',domain_set:'set'}];},get(){return {'.name':'p','.type':'policy',domain_set:'set',name:"O'Reilly"};}};
const text=lib.serialize(uci);assert(!text.includes('stale.test'));assert(text.includes("O'\\''Reilly"));
console.log('PASS: forgiving apex/subdomain, URL, IDN, normalization, bounds and effective UCI serialization.');
