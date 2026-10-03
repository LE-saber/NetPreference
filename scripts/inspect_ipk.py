#!/usr/bin/env python3
"""Validate a direct- or official-SDK-built package before artifact publication."""
import argparse
import hashlib
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile

import build_ipk


def members(blob):
    with tarfile.open(fileobj=io.BytesIO(blob), mode='r:*') as tf:
        result={}
        for m in tf:
            name=m.name.removeprefix('./')
            p=Path(name)
            if p.is_absolute() or '..' in p.parts:raise ValueError(f'unsafe archive path: {name}')
            if m.isfile():result[name]=(tf.extractfile(m).read(),m.mode)
        return result


def inspect(path):
    outer=members(path.read_bytes())
    assert outer['debian-binary'][0].strip()==b'2.0'
    control=members(outer['control.tar.gz'][0]);data=members(outer['data.tar.gz'][0])
    fields=dict(line.split(': ',1) for line in control['control'][0].decode().splitlines() if ': ' in line and not line.startswith(' '))
    assert fields['Package']=='luci-app-netpreference',fields
    assert fields['Version']==build_ipk.VERSION,fields
    assert fields['Architecture']=='x86_64',fields
    assert b'/etc/config/netpreference' in control['conffiles'][0]
    binary=data['usr/sbin/netpreference'][0];build_ipk.verify_elf(binary)
    for suffix in ('overview.js','advanced.js'):
        assert data['www/luci-static/resources/view/netpreference/'+suffix][0]
    assert [name for name in data if name.startswith('etc/config/')]==['etc/config/netpreference']
    assert 'nlbwmon' not in fields.get('Depends','')
    assert data['usr/share/netpreference/LICENSE-Go'][0]
    with tempfile.TemporaryDirectory(prefix='netpreference-package-') as tmp:
        exe=Path(tmp)/'netpreference';exe.write_bytes(binary);exe.chmod(0o755)
        assert subprocess.check_output([str(exe),'version'],text=True).strip()==build_ipk.VERSION.split('-r')[0]
    result={'package':path.name,'version':fields['Version'],'architecture':fields['Architecture'],
            'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'binary':'static linux/amd64',
            'source_sha':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip()}
    print(json.dumps(result,indent=2))
    return result

if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('package',type=Path)
    args=parser.parse_args();inspect(args.package)
