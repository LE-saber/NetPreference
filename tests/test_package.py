#!/usr/bin/env python3
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
SPEC=importlib.util.spec_from_file_location('build_ipk',ROOT/'scripts/build_ipk.py')
builder=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(builder)

def unpack(data):
    with tarfile.open(fileobj=io.BytesIO(data),mode='r:gz') as t:
        result={}
        for m in t.getmembers():
            assert m.name.startswith('./') and '..' not in Path(m.name).parts, m.name
            assert m.uid==0 and m.gid==0
            if m.isdir():
                assert m.mode == 0o755, m.name
                continue
            assert m.isfile(), m.name
            result[m.name[2:]]=(t.extractfile(m).read(),m.mode)
        return result

class PackageTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.path=ROOT/'dist'/f'luci-app-netpreference_{builder.VERSION}_x86_64.ipk'
        cls.outer=unpack(cls.path.read_bytes())
        cls.control=unpack(cls.outer['control.tar.gz'][0])
        cls.files=unpack(cls.outer['data.tar.gz'][0])
    def test_archive_contract(self):
        self.assertEqual(set(self.outer),{'debian-binary','control.tar.gz','data.tar.gz'})
        self.assertEqual(self.outer['debian-binary'][0],b'2.0\n')
        control=self.control['control'][0].decode()
        for line in ['Architecture: x86_64','Package: luci-app-netpreference',f'Version: {builder.VERSION}']:
            self.assertIn(line,control)
        self.assertNotIn('Depends: nlbwmon',control)
        self.assertEqual(self.control['conffiles'][0],b'/etc/config/netpreference\n')
    def test_data_archive_has_parent_directories(self):
        with tarfile.open(fileobj=io.BytesIO(self.outer['data.tar.gz'][0]),mode='r:gz') as tf:
            dirs={m.name[2:].rstrip('/') for m in tf.getmembers() if m.isdir()}
        for name in self.files:
            parent=Path(name).parent
            while parent != Path('.'):
                self.assertIn(parent.as_posix(),dirs,name)
                parent=parent.parent

    def test_static_binary_and_permissions(self):
        data,mode=self.files['usr/sbin/netpreference'];builder.verify_elf(data)
        self.assertEqual(mode,0o755)
        for name in ['etc/init.d/netpreference','usr/libexec/rpcd/netpreference']:
            self.assertEqual(self.files[name][1],0o755)
        self.assertEqual(subprocess.check_output([ROOT/'dist/netpreference','version'],text=True).strip(),builder.VERSION.split('-r')[0])
    def test_no_shared_config_files(self):
        configs=[p for p in self.files if p.startswith('etc/config/')]
        self.assertEqual(configs,['etc/config/netpreference'])
        self.assertIn("option enabled '0'",self.files['etc/config/netpreference'][0].decode())
        for name in ('postinst','prerm','postrm'):
            data,mode=self.control[name];self.assertEqual(mode,0o755)
            subprocess.run(['sh','-n'],input=data,check=True)
            # Image-builder staging must never operate on the host's services.
            subprocess.run(['sh','-c',data.decode(),'script','remove'],env={'IPKG_INSTROOT':'/staging','PATH':'/usr/bin:/bin'},check=True)
    def test_luci_acl_and_syntax(self):
        acl=json.loads(self.files['usr/share/rpcd/acl.d/luci-app-netpreference.json'][0])
        text=json.dumps(acl);self.assertNotIn('file',text);self.assertNotIn('"*"',text)
        self.assertIn('netpreference',text)
        for view in ('overview', 'advanced'):
            subprocess.run(['node','--check',ROOT/f'files/www/luci-static/resources/view/netpreference/{view}.js'],check=True)
        menu=json.loads(self.files['usr/share/luci/menu.d/luci-app-netpreference.json'][0])
        self.assertEqual(menu['admin/services/netpreference']['action']['type'],'firstchild')
        self.assertIn('admin/services/netpreference/advanced',menu)
        methods=json.loads(subprocess.check_output([ROOT/'dist/netpreference','rpc','list']))
        self.assertEqual(set(methods),{'apply','restore','validate','status','devices','traffic'})
        bad=subprocess.run([ROOT/'dist/netpreference','rpc','call','apply'],input=b'{"command":"rm"}',stdout=subprocess.PIPE)
        self.assertNotEqual(bad.returncode,0)
    def test_openwrt_prerm_does_not_preempt_default_service_stop(self):
        mk=(ROOT/'openwrt/luci-app-netpreference/Makefile').read_text()
        start=mk.index('define Package/luci-app-netpreference/prerm')
        end=mk.index('endef',start)
        prerm=mk[start:end]
        self.assertNotIn('ubus call service delete',prerm)
        self.assertNotIn('/etc/init.d/netpreference stop',prerm)
        self.assertIn('/usr/sbin/netpreference deactivate',prerm)
        self.assertIn('/usr/sbin/netpreference restore',prerm)

    def test_checksums(self):
        self.assertEqual((ROOT/'dist/SHA256SUMS').read_text().split()[0],hashlib.sha256(self.path.read_bytes()).hexdigest())
    def test_deterministic_builder(self):
        before=self.path.read_bytes();builder.build(ROOT/'dist',skip_build=True)
        self.assertEqual(before,self.path.read_bytes())

if __name__=='__main__':unittest.main(verbosity=2)
