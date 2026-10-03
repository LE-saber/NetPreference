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
 option profile 'work'
 option mac '02:00:00:00:00:01'
 option mode 'ipv6'
 option wait_ms '120'
 option delay_ms '100'
 option probe '1'
config profile 'work'
 option name 'Work'
config profile 'travel'
 option name 'Travel'
config domain_set 'v4_domains'
 option name 'V4 domains'
 list domain '*.v4.test'
config policy 'v4_override'
 option profile 'work'
 option domain_set 'v4_domains'
 option mode 'ipv4'
 option wait_ms '90'
 option delay_ms '180'
config policy 'short_override'
 option profile 'work'
 option domain 'fast.test'
 option mode 'ipv6'
 option wait_ms '90'
 option delay_ms '45'
config policy 'travel_dual'
 option profile 'travel'
 option domain '*'
 option mode 'dual'
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
        ns(router,'ip','addr','add','192.0.2.254/24','dev','br-lan')
        ns(router,'ip','-6','addr','add','fe80::1/64','dev','br-lan','nodad')
        for index,name in enumerate((first,second),1):
            ns(router,'ip','link','add',f'lan{index}','type','veth','peer','name',f'c{index}')
            ns(router,'ip','link','set',f'c{index}','netns',name)
            ns(router,'ip','link','set',f'lan{index}','master','br-lan');ns(router,'ip','link','set',f'lan{index}','up')
            ns(name,'ip','link','set',f'c{index}','name','eth0');mac=f'02:00:00:00:00:0{index}'
            ns(name,'ip','link','set','eth0','address',mac);ns(name,'ip','link','set','eth0','up')
            ns(name,'ip','addr','add',f'192.0.2.{index+1}/24','dev','eth0');ns(name,'ip','-6','addr','add',f'fd42:1::{index+1}/64','dev','eth0','nodad')
            ns(name,'ip','-6','addr','add',f'fe80::{index+1}/64','dev','eth0','nodad')
            ns(router,'ip','-6','neigh','replace',f'fe80::{index+1}','lladdr',mac,'nud','permanent','dev','br-lan')
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
        dns_fixture=start(router,sys.executable,FIXTURE,'serve');start(wan,sys.executable,FIXTURE,'echo');start(router,sys.executable,FIXTURE,'echo')
        daemon=start(router,BINARY,'serve');guard=start(router,BINARY,'guard')
        wait_for(lambda:ctl('status'))
        expect(first,'192.0.2.1',1,'rewrite.test','198.51.100.7')
        assert ctl('apply')['ok'];assert ctl('status')['active']
        devices=ctl('devices')
        print('DEVICES',json.dumps(devices,sort_keys=True),flush=True)
        selected_host=next(h for h in devices['devices'] if h['mac']=='02:00:00:00:00:01')
        assert '192.0.2.2' in selected_host['ipv4'],selected_host

        # An IPv6 privacy/SLAAC address not yet present in the router neighbour
        # cache is intentionally fail-open. Exercise one packet to create
        # neighbour state, then require the manager loop to discover the address
        # and refresh clients6 before enforcing subsequent DNS interception.
        first_v6=query(first,'fd42:1::1',1,'rewrite.test')
        assert first_v6['addresses'] in (['198.51.100.7'],['203.0.113.77']),first_v6

        def v6_discovered():
            ds=ctl('devices').get('devices',[])
            h=next((x for x in ds if x['mac']=='02:00:00:00:00:01'),None)
            return h if h and 'fd42:1::2' in h.get('ipv6',[]) else None
        selected_host=wait_for(v6_discovered,12)
        print('DEVICES_AFTER_V6',json.dumps(selected_host,sort_keys=True),flush=True)
        print('ROUTE6',ns(first,'ip','-6','route','get','fd42:1::1').strip(),flush=True)
        print('CLIENTS6',ns(router,'nft','list','set','inet','netpreference','clients6').strip(),flush=True)
        wait_for(lambda:'fd42:1::2' in ns(router,'nft','-j','list','set','inet','netpreference','clients6'),12)
        for host in ('192.0.2.1','fd42:1::1'):
            for proto in ('udp','tcp'):
                expect(first,host,1,'rewrite.test','203.0.113.77',proto=proto)
                expect(first,host,28,'rewrite.test','2001:db8::77',proto=proto)
                expect(second,host,1,'rewrite.test','198.51.100.7',proto=proto)
        # Explicitly exercise an IPv4 alias immediately.
        for proto in ('udp', 'tcp'):
            expect(first,'192.0.2.254',1,'rewrite.test','203.0.113.77',proto=proto)
            expect(first,'192.0.2.254',28,'rewrite.test','2001:db8::77',proto=proto)

        # Link-local RDNSS has the same neighbour-learning boundary as a new
        # SLAAC/privacy address. The first packet may fail open; after the
        # manager discovers fe80::2 it must become subject to policy.
        first_ll=query(first,'fe80::1%eth0',1,'rewrite.test')
        assert first_ll['addresses'] in (['198.51.100.7'],['203.0.113.77']),first_ll

        def ll_discovered():
            ds=ctl('devices').get('devices',[])
            h=next((x for x in ds if x['mac']=='02:00:00:00:00:01'),None)
            return h if h and 'fe80::2' in h.get('ipv6',[]) else None
        selected_host=wait_for(ll_discovered,12)
        print('DEVICES_AFTER_LL',json.dumps(selected_host,sort_keys=True),flush=True)
        wait_for(lambda:'fe80::2' in ns(router,'nft','-j','list','set','inet','netpreference','clients6'),12)
        for proto in ('udp', 'tcp'):
            expect(first,'fe80::1%eth0',1,'rewrite.test','203.0.113.77',proto=proto)
            expect(first,'fe80::1%eth0',28,'rewrite.test','2001:db8::77',proto=proto)
        expect(first,'192.0.2.1',1,'x.blocked.test',rcode=3)
        expect(second,'192.0.2.1',1,'x.blocked.test','198.51.100.7')
        r=expect(first,'192.0.2.1',1,'preference.test','198.51.100.7');assert r['elapsed']>=0.07,r
        # Domain sets and domain-specific A/B apply on both DNS transports and IP families.
        for host in ('192.0.2.1','fd42:1::1'):
            for proto in ('udp','tcp'):
                r=expect(first,host,28,'deep.v4.test','2001:db8::7',proto=proto)
                assert 0.15 <= r['elapsed'] < 1.5,r
                r=expect(first,host,1,'fast.test','198.51.100.7',proto=proto)
                assert 0.03 <= r['elapsed'] < 1.5,r
        # Reject bad references without losing the active policy or touching shared NAT66.
        p=base/'config';saved=p.read_text()
        p.write_text(saved.replace("option profile 'work'","option profile 'missing'",1))
        failed=subprocess.run(['ip','netns','exec',router,str(BINARY),'reload'],env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        assert failed.returncode != 0,failed.stdout
        rejected=json.loads(failed.stdout);assert not rejected.get('ok',True),rejected
        assert ctl('status')['active']
        r=expect(first,'192.0.2.1',28,'deep.v4.test','2001:db8::7');assert r['elapsed']>=0.15,r
        p.write_text(saved.replace("option profile 'work'","option profile 'travel'",1))
        assert ctl('reload')['ok']
        delays=ctl('status')['delayed']
        expect(first,'192.0.2.1',28,'deep.v4.test','2001:db8::7')
        assert ctl('status')['delayed']==delays,'old profile remained after switch'
        expect(first,'192.0.2.1',1,'x.blocked.test',rcode=3)
        p.write_text(saved);assert ctl('reload')['ok']
        assert ns(router,'nft','-j','list','table','inet','fw4')==before
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
        def observed():
            snapshot=json.loads(ns(router,BINARY,'rpc','call','traffic',input='{}',env=env))
            row=next((r for r in snapshot.get('rows',[]) if r['host']['mac']=='02:00:00:00:00:01'),None)
            if row and all(row[f'ipv{f}_rate']['valid'] and row[f'ipv{f}_rate']['upload_Bps']>0 and row[f'ipv{f}_rate']['download_Bps']>0 and
                           row['totals'][f'ipv{f}']['upload_bytes']>=20000 and row['totals'][f'ipv{f}']['download_bytes']>=20000 for f in (4,6)):
                return snapshot
        snapshot=wait_for(observed,15)
        rpc=snapshot
        print('TRAFFIC_FULL_CHAIN',json.dumps(rpc,sort_keys=True),flush=True)
        (ROOT/'dist/kernel-traffic.json').write_text(json.dumps(rpc,indent=2))
        def rowof(snap):return next(r for r in snap['rows'] if r['host']['mac']=='02:00:00:00:00:01')
        def counts():
            data=json.loads(ns(router,'nft','-j','list','table','inet','netpreference'))
            return {r['counter']['name']:r['counter']['bytes'] for r in data['nftables'] if 'counter' in r}
        def nondecreasing(old,new):
            for family in ('ipv4','ipv6'):
                for direction in ('upload_bytes','download_bytes'):assert new['totals'][family][direction]>=old['totals'][family][direction],(old,new)
        assert 0<rowof(rpc)['ipv6_ratio']<1,rpc
        # Profile-only reload must not rebuild the raw counters or clear totals.
        before_counts=counts();before_row=rowof(ctl('traffic'))
        active_config=p.read_text()
        p.write_text(active_config.replace("option profile 'work'","option profile 'travel'",1));assert ctl('reload')['ok']
        after_counts=counts()
        for name,value in before_counts.items():assert after_counts[name]>=value,(name,value,after_counts)
        nondecreasing(before_row,rowof(ctl('traffic')))
        p.write_text(active_config);assert ctl('reload')['ok']
        # Traffic terminating in a local proxy takes INPUT/OUTPUT, not FORWARD.
        # No HomeProxy binary is installed in this lab: exercise its packet path.
        before_counts=counts()
        ns(first,sys.executable,FIXTURE,'traffic','192.0.2.1');ns(first,sys.executable,FIXTURE,'traffic','fd42:1::1')
        after_counts=counts()
        for family in (4,6):
            for direction in ('up','down'):
                name=f'm020000000001_{family}_{direction}'
                assert after_counts[name]-before_counts[name]>=20000,(name,before_counts,after_counts)
        # New inventory membership really rebuilds the table; keep past totals
        # while explicitly invalidating the rate generation.
        before_row=rowof(ctl('traffic'))
        p.write_text(active_config+"\nconfig device 'spare'\n option enabled '0'\n option mac '02:00:00:00:00:03'\n")
        assert ctl('reload')['ok'];nondecreasing(before_row,rowof(ctl('traffic')))
        p.write_text(active_config);assert ctl('reload')['ok']
        # Original DNS chain outage pauses DNS interception, not read-only
        # traffic observation. Echo traffic must continue accumulating.
        dns_fixture.terminate();dns_fixture.wait(timeout=5)
        wait_for(lambda:not ctl('status')['active'],15)
        before_row=rowof(ctl('traffic'))
        ns(first,sys.executable,FIXTURE,'traffic','192.0.2.1');ns(first,sys.executable,FIXTURE,'traffic','fd42:1::1')
        wait_for(lambda:all(rowof(ctl('traffic'))['totals'][f'ipv{f}']['download_bytes']>before_row['totals'][f'ipv{f}']['download_bytes'] for f in (4,6)),15)
        dns_fixture=start(router,sys.executable,FIXTURE,'serve')
        wait_for(lambda:ctl('status')['active'],15)
        print('PASS: nft -> sampler -> runtime -> CLI/RPC four-direction rates/totals/ratio; profile reload, host churn, local-proxy path and DNS fail-open sampling',flush=True)
        assert ns(router,'nft','-j','list','table','inet','fw4')==before
        # SIGKILL cannot run defer/stop handlers: separate watchdog must recover.
        expect(first,'192.0.2.1',1,'rewrite.test','203.0.113.77',port=42054)
        def stale_dns_mapping():
            return ns(router,'conntrack','-L','-f','ipv4','-p','udp',
                      '-s','192.0.2.2','--sport','42054','--dport','53',
                      '--reply-src','192.0.2.1','--reply-port-src','1053').strip()
        assert stale_dns_mapping(),'the SIGKILL fixture must have a cached DNAT tuple'
        killed_at=time.monotonic()
        daemon.kill();daemon.wait(timeout=5)
        # Removing the nft table and clearing old conntrack are separate kernel
        # operations. Observe both, with the original deadline, before making
        # one strict DNS query on the SAME old port. Never retry that query to
        # hide a failure, and never treat table removal alone as completion.
        def recovered():
            if 'netpreference' in ns(router,'nft','list','tables'):return False
            return not stale_dns_mapping()
        wait_for(recovered,15)
        print('RECOVERY nft and cached DNAT cleared in',time.monotonic()-killed_at,'seconds',flush=True)
        expect(first,'192.0.2.1',1,'rewrite.test','198.51.100.7',port=42054)
        expect(first,'fd42:1::1',28,'rewrite.test','2001:db8::7')
        assert ns(router,'nft','-j','list','table','inet','fw4')==before
        print('PASS: real IPv4/IPv6 UDP/TCP per-device redirect, domain profiles/A-B/switch/invalid-reference rollback, timing, rules, custom fallback, own-table restore, identical-tuple conntrack recovery, SIGKILL watchdog, forwarded counters and NAT66 preservation.',flush=True)
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
