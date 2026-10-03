#!/usr/bin/env python3
"""Static regression guards for CI routing/security, complemented by actual Actions runs."""
from pathlib import Path
import re
import unittest
ROOT=Path(__file__).resolve().parents[1]

class WorkflowTests(unittest.TestCase):
    def test_feature_pr_main_and_manual_verification(self):
        text=(ROOT/'.github/workflows/ci.yml').read_text()
        self.assertIn("branches: ['**']",text)
        self.assertIn('  pull_request:',text)
        self.assertIn('  workflow_dispatch:',text)
        self.assertNotIn('feat/mvp',text)
        self.assertNotIn('continue-on-error',text)
        self.assertNotIn('.bootstrap',text)
        self.assertIn('bash scripts/check.sh',text)
        self.assertIn('sudo python3 tests/netns.py',text)
    def test_release_requires_successful_main_push_and_both_jobs(self):
        text=(ROOT/'.github/workflows/ci.yml').read_text()
        release=text.split('\n  release:\n',1)[1]
        self.assertIn('needs: [verify, sdk]',release)
        self.assertIn("if: github.event_name == 'push' && github.ref == 'refs/heads/main'",release)
        self.assertNotIn('if: always()',release)
        self.assertEqual(text.count('gh release create'),1)
        self.assertIn('sha256sum -c SHA256SUMS',release)
    def test_reusable_sdk_cannot_publish(self):
        text=(ROOT/'.github/workflows/sdk-build.yml').read_text()
        self.assertIn('  workflow_call:',text)
        self.assertIn('contents: read',text)
        self.assertNotIn('contents: write',text)
        self.assertNotIn('gh release',text)
        self.assertIn('python3 scripts/inspect_ipk.py',text)
        self.assertIn('bash tests/rootfs_install.sh',text)
        self.assertIn('sha256sum -c -',text)
    def test_failure_recording_is_scoped(self):
        text=(ROOT/'.github/workflows/ci.yml').read_text()
        part=text.split('\n  record-failures:\n',1)[1].split('\n  release:\n')[0]
        self.assertIn("github.event_name == 'push'",part)
        self.assertIn("github.ref != 'refs/heads/main'",part)
        script=(ROOT/'scripts/ci_failure.py').read_text()
        self.assertNotIn('feat/mvp',script)
        self.assertNotIn('--force',script)
        self.assertIn("branch in ('','main')",script)
    def test_third_party_actions_pinned(self):
        for filename in ('ci.yml','sdk-build.yml'):
            for action in re.findall(r'uses: (\S+)',(ROOT/'.github/workflows'/filename).read_text()):
                if action.startswith('./'):continue
                self.assertRegex(action,r'@[a-f0-9]{40}$')

if __name__=='__main__':unittest.main(verbosity=2)
