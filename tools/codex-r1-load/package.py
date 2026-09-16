#!/usr/bin/env python3
"""Create an allowlisted B8 delivery ZIP only after independent acceptance.

No git writes, Docker commands or network calls. Run after final verification
reports have been committed/pushed. Unknown files, auth directories, databases
and raw live-baseline records are never included by a recursive temp-dir copy.
"""
from __future__ import annotations
import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import subprocess
import zipfile

from verify import SOURCE, digest, require, verify_directory, VerificationError

EVIDENCE = [
    'b8-independent-acceptance.json', 'b8-release-evidence-reverified.json',
    'linux-full-unit-release3-result.json', 'linux-full-unit-release3.jsonl',
    'linux-full-unit-release3-command.sh',
    'linux-race-release3-result.json', 'linux-race-release3.jsonl',
    'linux-race-release3-command.sh', 'linux-race-release3-selection.json',
    'candidate-release-build-result.json', 'candidate-release-build.log',
    'candidate-release-build-command.sh', 'candidate-release-metadata.json',
    'candidate-release-inspect.json', 'candidate-release-binary.sha256',
    'candidate-release-export.json', 'candidate-runtime-release-result.json',
    'image-inputs-lock.json', 'baseline-release-source.json',
    'baseline-release-build-result.json', 'baseline-release-build.log',
    'baseline-release-build-command.sh', 'baseline-release-metadata.json',
    'b8-independent-verifier-unit-result.json', 'b8-independent-verifier-unit.log',
    'fullimage-harness-v2.sha256.json', 'fullimage-harness-v2/run.py',
    'fullimage-harness-v2/load_agent.cjs', 'fullimage-harness-v2/fake_upstream.cjs',
    'fullimage-smoke01/result.json',
    'fullimage-soak01/result.json', 'fullimage-soak01/environment.json',
    'fullimage-soak01/memory.json', 'fullimage-soak01/baseline-latencies.json',
    'fullimage-soak01/candidate-latencies.json', 'fullimage-soak01-driver-result.json',
    'b8-remote-ci-final.json', 'b8-git-final.json',
    'source-'+SOURCE+'.tar', 'sub2api-r1-8905cbb82d4b.docker.tar',
]
DOCUMENTS = [
    'docs/codex-gap/r1/b8-verification.md',
    'docs/codex-gap/r1/final-summary.md',
    'tools/codex-r1-load/README.md', 'tools/codex-r1-load/run.py',
    'tools/codex-r1-load/fake_upstream.cjs', 'tools/codex-r1-load/verify.py',
    'tools/codex-r1-load/test_verify.py', 'tools/codex-r1-load/package.py',
]

def git(repo: Path, *args: str) -> str:
    return subprocess.check_output(['git',*args],cwd=repo,text=True,timeout=20).strip()

def build_package(evidence: Path, repo: Path, output: Path) -> dict:
    acceptance=verify_directory(evidence)
    (evidence/'b8-independent-acceptance.json').write_text(json.dumps(acceptance,indent=2)+'\n',encoding='utf-8')
    require(git(repo,'branch','--show-current')=='feature/codex-gap-v1','wrong delivery branch')
    head=git(repo,'rev-parse','HEAD')
    require(not git(repo,'status','--porcelain'),'uncommitted delivery files')
    changes=git(repo,'diff','--name-only',SOURCE,head).splitlines()
    require(all(p.startswith(('docs/codex-gap/r1/','tools/codex-r1-load/')) for p in changes),'image no longer corresponds to current product files')
    delivery=json.loads((evidence/'b8-git-final.json').read_text(encoding='utf-8'))
    require(delivery['head']==head and delivery['remote_feature']==head and delivery['main_unchanged'] is True,'final push/main verification missing')
    require(delivery['remote_main']=='30ed40a56a5f4b5ab7b8dd3d685353db3a531c84','main reference changed; review before delivery')
    report=(repo/'docs/codex-gap/r1/b8-verification.md').read_text(encoding='utf-8')
    require('B8 status: PASSED' in report and SOURCE in report,'final report has not been completed')
    files=[(evidence/name,'evidence/'+name) for name in EVIDENCE]+[(repo/name,name) for name in DOCUMENTS]
    require(all(p.is_file() and not p.is_symlink() for p,_ in files),'allowlisted delivery artifact missing or symlinked')
    manifest={'product_source_commit':SOURCE,'delivery_commit':head,'created_utc':datetime.now(timezone.utc).isoformat(),'main_merged':False,'production_deployed':False,'files':[{'name':name,'bytes':p.stat().st_size,'sha256':digest(p)} for p,name in files]}
    require(not output.exists(),'delivery ZIP already exists; verify it instead of overwriting')
    output.parent.mkdir(parents=True,exist_ok=True)
    tmp=output.with_suffix(output.suffix+'.tmp')
    require(not tmp.exists(),'partial ZIP already exists; inspect before reuse')
    with zipfile.ZipFile(tmp,'w',compression=zipfile.ZIP_DEFLATED,compresslevel=6) as z:
        z.writestr('MANIFEST.json',json.dumps(manifest,indent=2)+'\n')
        for path,name in files: z.write(path,name)
    with zipfile.ZipFile(tmp) as z:
        require(z.testzip() is None,'ZIP CRC verification failed')
    tmp.replace(output)
    summary={'path':str(output),'bytes':output.stat().st_size,'sha256':digest(output),'product_source_commit':SOURCE,'delivery_commit':head,'file_count':len(files),'independent_acceptance':True}
    (evidence/'b8-delivery-package.json').write_text(json.dumps(summary,indent=2)+'\n',encoding='utf-8')
    return summary

def main() -> int:
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('evidence_root',type=Path)
    p.add_argument('repo',type=Path)
    p.add_argument('output_zip',type=Path)
    a=p.parse_args()
    try:
        print(json.dumps(build_package(a.evidence_root,a.repo,a.output_zip),indent=2))
        return 0
    except (VerificationError, OSError, ValueError, KeyError, subprocess.SubprocessError) as e:
        print(json.dumps({'passed':False,'error':str(e)},indent=2))
        return 1

if __name__ == '__main__': raise SystemExit(main())
