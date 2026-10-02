#!/usr/bin/env python3
"""Build a deterministic OpenWrt 24.10 tar-format IPK, without a C toolchain.

Uses only Python/Go standard libraries. This is not a Debian .deb: the outer
archive is gzip/tar, matching OpenWrt scripts/ipkg-build on openwrt-24.10.
"""
from __future__ import annotations
import argparse
import gzip
import hashlib
import io
import json
import os
import re
from pathlib import Path
import struct
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parents[1]

def package_version() -> str:
    text = (ROOT / 'openwrt/luci-app-netpreference/Makefile').read_text()
    version = re.search(r'^PKG_VERSION:=(\S+)$', text, re.MULTILINE)
    release = re.search(r'^PKG_RELEASE:=(\S+)$', text, re.MULTILINE)
    if not version or not release:
        raise RuntimeError('missing PKG_VERSION/PKG_RELEASE in OpenWrt package Makefile')
    return f'{version.group(1)}-r{release.group(1)}'

VERSION = package_version()
EPOCH = int(os.environ.get('SOURCE_DATE_EPOCH', '1790812800'))
DEPS = 'luci-base, rpcd, uci, firewall4, nftables-json, ip-full, conntrack'

def archive(entries: dict[str, tuple[bytes, int]]) -> bytes:
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode='w', format=tarfile.GNU_FORMAT) as tf:
        directories: set[str] = set()
        for name in entries:
            parent = Path(name).parent
            while parent != Path('.'):
                directories.add(parent.as_posix())
                parent = parent.parent
        for name in sorted(directories, key=lambda p: (p.count('/'), p)):
            ti = tarfile.TarInfo('./' + name)
            ti.type = tarfile.DIRTYPE
            ti.size, ti.mode, ti.mtime = 0, 0o755, EPOCH
            ti.uid = ti.gid = 0
            ti.uname = ti.gname = ''
            tf.addfile(ti)
        for name, (data, mode) in sorted(entries.items()):
            ti = tarfile.TarInfo('./' + name)
            ti.size, ti.mode, ti.mtime = len(data), mode, EPOCH
            ti.uid = ti.gid = 0
            ti.uname = ti.gname = ''
            tf.addfile(ti, io.BytesIO(data))
    return gzip.compress(out.getvalue(), compresslevel=9, mtime=0)

def verify_elf(data: bytes) -> None:
    if data[:6] != b'\x7fELF\x02\x01' or struct.unpack_from('<H', data, 18)[0] != 62:
        raise ValueError('Expected a little-endian x86_64 ELF executable')
    phoff = struct.unpack_from('<Q', data, 32)[0]
    phsize, phnum = struct.unpack_from('<HH', data, 54)
    for i in range(phnum):
        if struct.unpack_from('<I', data, phoff + i * phsize)[0] == 3:
            raise ValueError('Dynamic ELF interpreter: build with CGO_ENABLED=0')

def build(out: Path, skip_build: bool = False) -> Path:
    out.mkdir(parents=True, exist_ok=True)
    binary = out / 'netpreference'
    if not skip_build:
        env = os.environ | {'CGO_ENABLED':'0', 'GOOS':'linux', 'GOARCH':'amd64', 'GOAMD64':'v1'}
        subprocess.run(['go','build','-trimpath','-buildvcs=false',
                        '-ldflags=-s -w -buildid=', '-o',str(binary), './cmd/netpreference'],
                       cwd=ROOT, env=env, check=True)
    data = binary.read_bytes()
    verify_elf(data)
    entries: dict[str, tuple[bytes, int]] = {}
    for p in sorted((ROOT / 'files').rglob('*')):
        if p.is_file():
            name = p.relative_to(ROOT / 'files').as_posix()
            mode = 0o755 if name.startswith(('etc/init.d/', 'usr/libexec/')) else 0o644
            entries[name] = (p.read_bytes(), mode)
    entries['usr/sbin/netpreference'] = (data, 0o755)
    entries['usr/share/netpreference/LICENSE'] = ((ROOT / 'LICENSE').read_bytes(), 0o644)
    goroot = Path(subprocess.check_output(['go','env','GOROOT'],text=True).strip())
    entries['usr/share/netpreference/LICENSE-Go'] = ((goroot/'LICENSE').read_bytes(),0o644)
    payload = archive(entries)
    control = (f'Package: luci-app-netpreference\nVersion: {VERSION}\n'
               'Architecture: x86_64\nMaintainer: LE-saber\nSection: luci\nPriority: optional\n'
               f'Depends: {DEPS}\n'
               'Source: https://github.com/LE-saber/NetPreference\nLicense: MIT\n'
               f'Installed-Size: {len(gzip.decompress(payload))}\n'
               'Description: Per-device dual-stack DNS policy, observation and safe recovery\n')
    controls = {'control': (control.encode(),0o644),
                'conffiles':(b'/etc/config/netpreference\n',0o644)}
    for name in ('postinst','prerm','postrm'):
        controls[name]=((ROOT/'packaging'/name).read_bytes(),0o755)
    package = archive({'debian-binary':(b'2.0\n',0o644),
                       'control.tar.gz':(archive(controls),0o644),
                       'data.tar.gz':(payload,0o644)})
    path=out/f'luci-app-netpreference_{VERSION}_x86_64.ipk'
    path.write_bytes(package)
    digest=hashlib.sha256(package).hexdigest()
    (out/'SHA256SUMS').write_text(f'{digest}  {path.name}\n')
    compiler=subprocess.check_output(['go','version'], text=True).strip()
    (out/'build-info.json').write_text(json.dumps({
        'package':path.name,'version':VERSION,'sha256':digest,'compiler':compiler,
        'source_date_epoch':EPOCH,'cgo':False,'goamd64':'v1','dependencies':DEPS,
        'target':'ImmortalWrt/OpenWrt 24.10 x86_64','router_validation':'not implied by package build'
    }, indent=2)+'\n')
    print(f'{path} ({len(package)} bytes)\nSHA256 {digest}\n{compiler}')
    return path

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--out',type=Path,default=ROOT/'dist')
    parser.add_argument('--skip-build',action='store_true',help='Package an existing out/netpreference binary')
    args=parser.parse_args()
    build(args.out.resolve(),args.skip_build)
