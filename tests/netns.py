#!/usr/bin/env python3
"""Root-only, real nft/conntrack DNS behavioral lab in disposable namespaces.

No host firewall or production router is modified. The UCI and nlbw commands
are explicit fixtures: this proves Linux packet paths, not a complete router UI.
"""
from __future__ import annotations
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time

ROOT=Path(__file__).resolve().parents[1]
FIXTURE=ROOT/'tests/dns_fixture.py'
BINARY=ROOT/'dist/netpreference'


def run(*args,**kw):
    result = subprocess.run([str(x) for x in args], check=False, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kw)
    if result.returncode:
        raise RuntimeError(f"command {result.args!r} exited {result.returncode}\n"
                           f"stdout:\n{result.stdout}\nstderr:\n{result.stderr}")
    return result.stdout


def wait_for(fn,timeout=12):
    end=time.monotonic()+timeout
    last=None
    while time.monotonic()<end:
        try:
            value=fn()
            if value:return value
        except (Exception,) as exc:last=exc
        time.sleep(0.25)
    raise AssertionError(f'condition timed out: {last}')


def main():
    if os.geteuid()!=0:raise SystemExit('requires root; use sudo python3 tests/netns.py on a disposable Linux runner')
    for cmd in ('ip','nft','conntrack'):
        if not shutil.which(cmd):raise SystemExit(f'missing dependency: {cmd}')
    suffix=str(os.getpid());names=[f'np-r-{suffix}',f'np-a-{suffix}',f'np-b-{suffix}',f'np-w-{suffix}']
    router,first,second,wan=names
    created=[];processes=[];logs=[]
    temp=tempfile.TemporaryDirectory(prefix='netpreference-lab-');base=Path(temp.name)
    env=os.environ|{'NETPREFERENCE_STATE_DIR':str(base/'state'),'NETPREFERENCE_SOCKET':str(base/'run/control.sock'),'NP_TEST_CONFIG':str(base/'config')}
    bindir=base/'bin';bindir.mkdir();env['PATH']=str(bindir)+':'+os.environ['PATH']
    # These shims deliberately model only our two read APIs and own UCI writes.
    (bindir/'uci').write_text('''#!/usr/bin/env python3
import os,re,sys
from pathlib import Path
p=Path(os.environ['NP_TEST_CONFIG']);args=sys.argv[1:]
if args==['-q','export','netpreference']:print(p.read_text(),end='')
elif len(args)==2 and args[0]=='set' and args[1].startswith('netpreference.main.enabled='):
 text=p.read_text();value=args[1].split('=')[1];text=re.sub(r"option enabled '[01]'", "option enabled '"+value+"'",text,count=1);p.write_text(text)
elif args==['commit','netpreference']:pass
else:raise SystemExit('unexpected UCI call '+repr(args))
''');(bindir/'uci').chmod(0o755)
    (bindir/'nlbw').write_text('''#!/bin/sh
[ "$*" = '-c json -g mac,family' ] || exit 9
printf '%s\\n' '{"columns":["family","mac","rx_bytes","tx_bytes"],"data":[[4,"02:00:00:00:00:01",1000,2000],[6,"02:00:00:00:00:01",3000,4000]]}'
''');(bindir/'nlbw').chmod(0o755)
    cfg="""config global 'main'
 option enabled '0'
 option monitor '0'
 list interface 'br-lan'
 option upstream '127.0.0.1:53'
config device 'selected'
 option enabled '1'
 option name 'selected'
 option mac '02:00:00:00:00:01'
 option mode 'ipv6'
 option wait_ms '120'
 option delay_ms '100'
 option probe '1'
config rule 'static'
 option device '*'
 option domain 'rewrite.test'
 option action 'rewrite'
 list ipv4 '203.0.113.77'
 list ipv6 '2001:db8::77'
config rule 'block'
 option device '*'
 option domain '*.blocked.test'
 option action 'nxdomain'
config rule 'custom'
 option device '*'
 option domain 'fallback.test'
 option action 'forward'
 option upstream 'tcp://127.0.0.1:9'
"""
    (base/'config').write_text(cfg)
    def ns(n,*args,**kw):return run('ip','netns','exec',n,*args,**kw)
    def ctl(method):return json.loads(ns(router,BINARY,method,env=env))
    def start(n,*args):
        log=(base/f'log-{len(logs)}.txt').open('w+');logs.append(log)
        p=subprocess.Popen(['ip','netns','exec',n,*map(str,args)],env=env,stdout=log,stderr=log);processes.append(p);return p
    def query(n,host,typ,domain,proto='udp',port=0):
        result=json.loads(ns(n,sys.executable,FIXTURE,'query',host,typ,domain,proto,port))
        print('DNS',n,domain,json.dumps(result),flush=True);return result
    def expect(n,host,typ,domain,address=None,rcode=0,proto='udp',port=0):
        r=query(n,host,typ,domain,proto,port);assert r['rcode']==rcode,r
        if address is not None:assert address in r['addresses'],r
        return r
    try:
        for name in names:run('ip','netns','add',name);created.append(name);ns(name,'ip','link','set','lo','up')
        ns(router,'ip','link','add','br-lan','type','bridge');ns(router,'ip','link','set','br-lan','up')
        ns(router,'ip','addr','add','192.0.2.1/24','dev','br-lan');ns(router,'ip','-6','addr','add','fd42:1::1/64','dev','br-lan','nodad')
        for index,name in enumerate((first,second),1):
            ns(router,'ip','link','add',f'lan{index}','type','veth','peer','name',f'c{index}')
            ns(router,'ip','link','set',f'c{index}','netns',name)
            ns(router,'ip','link','set',f'lan{index}','master','br-lan');ns(router,'ip','link','set',f'lan{index}','up')
            ns(name,'ip','link','set',f'c{index}','name','eth0');mac=f'02:00:00:00:00:0{index}'
            ns(name,'ip','link','set','eth0','address',mac);ns(name,'ip','link','set','eth0','up')
            ns(name,'ip','addr','add',f'192.0.2.{index+1}/24','dev','eth0');ns(name,'ip','-6','addr','add',f'fd42:1::{index+1}/64','dev','eth0','nodad')
            ns(name,'ip','route','add','default','via','192.0.2.1');ns(name,'ip','-6','route','add','default','via','fd42:1::1')
            ns(router,'ip','neigh','replace',f'192.0.2.{index+1}','lladdr',mac,'nud','permanent','dev','br-lan')
            ns(router,'ip','-6','neigh','replace',f'fd42:1::{index+1}','lladdr',mac,'nud','permanent','dev','br-lan')
        ns(router,'ip','link','add','wan0','type','veth','peer','name','w0');ns(router,'ip','link','set','w0','netns',wan)
        ns(router,'ip','link','set','wan0','up');ns(wan,'ip','link','set','w0','up')
        ns(router,'ip','addr','add','198.51.100.1/24','dev','wan0');ns(wan,'ip','addr','add','198.51.100.2/24','dev','w0')
        ns(router,'ip','-6','addr','add','2001:db8:2::1/64','dev','wan0','nodad');ns(wan,'ip','-6','addr','add','2001:db8:2::2/64','dev','w0','nodad')
        ns(wan,'ip','route','add','192.0.2.0/24','via','198.51.100.1');ns(router,'sysctl','-qw','net.ipv4.ip_forward=1','net.ipv6.conf.all.forwarding=1')
        sentinel="""table inet fw4 {
 comment "existing firewall/NAT66 sentinel"
 chain srcnat {
  type nat hook postrouting priority 100; policy accept;
  oifname "wan0" meta nfproto ipv6 masquerade
 }
}
"""
        ns(router,'nft','-f','-',input=sentinel);before=ns(router,'nft','-j','list','table','inet','fw4')
        start(router,sys.executable,FIXTURE,'serve');start(wan,sys.executable,FIXTURE,'echo')
        daemon=start(router,BINARY,'serve');guard=start(router,BINARY,'guard')
        wait_for(lambda:ctl('status'))
        expect(first,'192.0.2.1',1,'rewrite.test','198.51.100.7')
        assert ctl('apply')['ok'];assert ctl('status')['active']
        for host in ('192.0.2.1','fd42:1::1'):
            for proto in ('udp','tcp'):
                expect(first,host,1,'rewrite.test','203.0.113.77',proto=proto)
                expect(first,host,28,'rewrite.test','2001:db8::77',proto=proto)
                expect(second,host,1,'rewrite.test','198.51.100.7',proto=proto)
        expect(first,'192.0.2.1',1,'x.blocked.test',rcode=3)
        expect(second,'192.0.2.1',1,'x.blocked.test','198.51.100.7')
        r=expect(first,'192.0.2.1',1,'preference.test','198.51.100.7');assert r['elapsed']>=0.07,r
        expect(first,'192.0.2.1',1,'fallback.test','198.51.100.7')
        expect(first,'192.0.2.1',1,'rewrite.test','203.0.113.77',port=42053)
        # A successful restore also cleans the already cached UDP DNAT tuple.
        assert ctl('restore')['ok'];expect(first,'192.0.2.1',1,'rewrite.test','198.51.100.7',port=42053)
        assert ns(router,'nft','-j','list','table','inet','fw4')==before
        assert not ctl('status')['active'];assert ctl('restore')['ok']
        assert ctl('apply')['ok']
        # Enable optional monitoring, send real forwarded IPv4 and NAT66 traffic.
        p=base/'config';p.write_text(p.read_text().replace("option monitor '0'","option monitor '1'"));assert ctl('reload')['ok']
        ns(first,sys.executable,FIXTURE,'traffic','198.51.100.2');ns(first,sys.executable,FIXTURE,'traffic','2001:db8:2::2')
        table=json.loads(ns(router,'nft','-j','list','table','inet','netpreference'))
        counters={r['counter']['name']:r['counter']['bytes'] for r in table['nftables'] if 'counter' in r}
        for family in (4,6):
            for direction in ('up','down'):assert counters[f'm020000000001_{family}_{direction}']>0,counters
        wait_for(lambda:ctl('traffic').get('totals_available'))
        assert ns(router,'nft','-j','list','table','inet','fw4')==before
        # SIGKILL cannot run defer/stop handlers: separate watchdog must recover.
        expect(first,'192.0.2.1',1,'rewrite.test','203.0.113.77',port=42054)
        daemon.kill();daemon.wait(timeout=5)
        def removed():return 'netpreference' not in ns(router,'nft','list','tables')
        wait_for(removed,15)
        expect(first,'192.0.2.1',1,'rewrite.test','198.51.100.7',port=42054)
        expect(first,'fd42:1::1',28,'rewrite.test','2001:db8::7')
        assert ns(router,'nft','-j','list','table','inet','fw4')==before
        print('PASS: real IPv4/IPv6 UDP/TCP per-device redirect, timing, rules, custom fallback, own-table restore, identical-tuple conntrack recovery, SIGKILL watchdog, forwarded counters and NAT66 preservation.',flush=True)
        print('LIMIT: UCI/nlbw are fixtures; host kernel is not the target ImmortalWrt kernel; LuCI/procd target acceptance is separate.',flush=True)
    finally:
        for proc in reversed(processes):
            if proc.poll() is None:
                proc.terminate()
                try:proc.wait(timeout=3)
                except subprocess.TimeoutExpired:proc.kill();proc.wait()
        for log in logs:
            log.flush();log.seek(0);text=log.read()
            if text:print('PROCESS LOG',text)
            log.close()
        for name in reversed(created):subprocess.run(['ip','netns','del',name],check=False)
        temp.cleanup()

if __name__=='__main__':main()
