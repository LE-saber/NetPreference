#!/usr/bin/env python3
"""Record one failed phase on a development push, never main or a PR branch.

The CI recorder downloads only artifacts from the same run. Call separately for
individual failures; each creates its own reviewable commit. No environment or
credential dumps are included.
"""
import os
from pathlib import Path
import re
import subprocess
import sys

phase=sys.argv[1]
if not re.fullmatch(r'[a-z0-9_-]{1,40}',phase):raise SystemExit('invalid phase')
branch=os.environ.get('GITHUB_REF_NAME','')
if os.environ.get('GITHUB_EVENT_NAME')!='push' or branch in ('','main'):
    raise SystemExit('failure commits are allowed only for non-main push events')
subprocess.run(['git','check-ref-format','--branch',branch],check=True,stdout=subprocess.DEVNULL)
run=os.environ.get('GITHUB_RUN_ID','local');attempt=os.environ.get('GITHUB_RUN_ATTEMPT','1')
path=Path('docs/failures/0.2.0')/f'ci-{run}-{attempt}-{phase}.md'
path.parent.mkdir(parents=True,exist_ok=True)
log=Path(sys.argv[2]) if len(sys.argv)>2 else Path('dist/logs')/(phase+'.txt')
text=log.read_text(errors='replace')[-16000:] if log.exists() else 'No phase log produced; inspect the job steps and outcomes artifact.'
url='https://github.com/'+os.environ.get('GITHUB_REPOSITORY','LE-saber/NetPreference')+'/actions/runs/'+run
sha=os.environ.get('GITHUB_SHA','unknown')
path.write_text(f'# CI failure: {phase}\n\nRun: {url}\n\nTested source: `{sha}`. Branch: `{branch}`.\n\nThis phase failed, and is NOT verified. See subsequent repair and successful run evidence. No production router was modified.\n\n```text\n{text}\n```\n')
subprocess.run(['git','config','user.name','github-actions[bot]'],check=True)
subprocess.run(['git','config','user.email','41898282+github-actions[bot]@users.noreply.github.com'],check=True)
subprocess.run(['git','add',str(path)],check=True)
subprocess.run(['git','commit','-m',f'docs: record {phase} failure for run {run}'],check=True)
for retry in range(3):
    subprocess.run(['git','fetch','origin',branch],check=True)
    subprocess.run(['git','rebase','FETCH_HEAD'],check=True)
    if subprocess.run(['git','push','origin',f'HEAD:refs/heads/{branch}']).returncode==0:break
else:raise SystemExit('failure record push failed after 3 non-forced attempts')
print(path)
