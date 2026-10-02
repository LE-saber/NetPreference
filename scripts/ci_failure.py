#!/usr/bin/env python3
"""Persist a single failed CI phase. No credentials or environment dumps."""
import os
from pathlib import Path
import subprocess
import sys
phase=sys.argv[1]
run=os.environ.get('GITHUB_RUN_ID','local')
attempt=os.environ.get('GITHUB_RUN_ATTEMPT','1')
path=Path('docs/failures')/f'ci-{run}-{attempt}-{phase}.md'
path.parent.mkdir(parents=True,exist_ok=True)
log=Path('dist/logs')/(phase+'.txt')
text=log.read_text(errors='replace')[-16000:] if log.exists() else 'No phase log was produced; inspect the workflow job steps.'
url='https://github.com/'+os.environ.get('GITHUB_REPOSITORY','LE-saber/NetPreference')+'/actions/runs/'+run
path.write_text(f'# CI failure: {phase}\n\nRun: {url}\n\nThis phase failed and is not verified. See the log below and subsequent recovery commits. No production router was modified.\n\n```text\n{text}\n```\n')
print(path)
subprocess.run(['git','config','user.name','github-actions[bot]'],check=True)
subprocess.run(['git','config','user.email','41898282+github-actions[bot]@users.noreply.github.com'],check=True)
subprocess.run(['git','add',str(path)],check=True)
subprocess.run(['git','commit','-m',f'docs: record {phase} failure for run {run}'],check=True)
for retry in range(3):
    subprocess.run(['git','fetch','origin','feat/mvp'],check=True)
    subprocess.run(['git','rebase','origin/feat/mvp'],check=True)
    pushed=subprocess.run(['git','push','origin','HEAD:feat/mvp'])
    if pushed.returncode == 0:
        break
else:
    raise SystemExit('failed to push CI failure record after 3 rebased attempts')
