#!/usr/bin/env python3
"""Independently verify an ACTUAL completed R1 B8 run. Never starts a test.

Reads the raw latency and memory files rather than trusting result.passed.
The acceptance dimensions and image identities are intentionally fixed to R1.
This tool cannot grant completion to a short smoke, partial run, or stale image.
It has no Docker, credential, git, deployment, or network operations.
"""
from __future__ import annotations
import argparse
import hashlib
import json
import math
from pathlib import Path
import statistics
import sys
from typing import Any

SOURCE = '8905cbb82d4bad011eddae5e83228f52906c4b40'
BASELINE = 'local/sub2api-r1-baseline@sha256:8af502b156cecdbb6f9469d604776c540ae501995094f9d8a50bb08f4bd42611'
CANDIDATE = 'local/sub2api-r1@sha256:ed414eb7c1896506c1a7ab009ff3cfdca3857a9de366776e746be95b1f555a85'
HARNESS = {'run.py': 'd5b3baf318cea8596137797aaf5721ed49d0973fc70cf0dd0c2b17712b085f60', 'load_agent.cjs': '61b5a329378d52257e7782641afe30ea67896d2994379318d68e5b0a35fe9596', 'fake_upstream.cjs': 'cff43063aeea46844f473c4960d94ef239feb4da7a7db932b2a2c3b233f67980', 'duplicate_tracker.cjs': '30511bdb5531a1be2e7176122df0b4c35e34c85f2525c14edef250d9178fc839'}
SECONDS, WARMUP, RPS = 7200, 300, 20
FINAL_RUN_ID = 'sub2api-r1-b8-soak05'
FINAL_RUN_DIR = 'fullimage-soak05'

class VerificationError(ValueError):
    """A required result is absent, inconsistent, or outside the approved gate."""

def require(condition: bool, message: str) -> None:
    if not condition:
        raise VerificationError(message)

def load(path: Path) -> Any:
    require(path.is_file(), f'MISSING_REQUIRED_ARTIFACT: {path.name}')
    return json.loads(path.read_text(encoding='utf-8'))

def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open('rb') as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()

def quantile(values: list[float], q: float) -> float:
    require(bool(values), 'empty latency series')
    require(all(isinstance(v, (int,float)) and math.isfinite(v) and v > 0 for v in values), 'nonpositive or nonfinite latency')
    ordered = sorted(values)
    return ordered[int((len(ordered) - 1) * q)]

def verify_soak(result: dict, memory: list, series: dict, environment: dict) -> dict:
    p = result.get('parameters', {})
    require((p.get('seconds'), p.get('warmup'), p.get('rps')) == (SECONDS, WARMUP, RPS), 'not the approved 7200-second measured run with 300-second warmup / 20 RPS')
    require(p.get('baseline') == BASELINE and p.get('candidate') == CANDIDATE, 'stale or mismatched image digest')
    require(result.get('network_internal') is True and environment.get('network', {}).get('internal') is True, 'missing internal-network evidence')
    require(result.get('real_model_requests') == 0, 'real model traffic is forbidden')
    require(result.get('actual_elapsed_s', 0) >= WARMUP + SECONDS - 1, 'measurement duration incomplete')
    require(result.get('passed') is True and not result.get('error'), 'load controller did not pass')
    expected_gates = {'zero_errors','no_upstream_duplicates','valid_upstream_inputs','upstream_count_exact','duration_complete','sample_count_complete','p95_within_10pct','stable_rss_within_20pct','fake_tracker_bounded'}
    require(all(result.get('gates', {}).get(k) is True for k in expected_gates), 'controller gate absent or failed')
    require(environment.get('controller_sha256') == HARNESS['run.py'] and environment.get('agent_sha256') == HARNESS['load_agent.cjs'] and environment.get('fixture_sha256') == HARNESS['fake_upstream.cjs'] and environment.get('tracker_sha256') == HARNESS['duplicate_tracker.cjs'], 'harness identity mismatch')
    require(bool(memory) and memory[0]['elapsed_s'] < 60 and memory[-1]['elapsed_s'] >= WARMUP + SECONDS - 31, 'memory observation window incomplete')
    require(all(0 <= b['elapsed_s'] - a['elapsed_s'] <= 65 for a,b in zip(memory, memory[1:])), 'memory sampling has an excessive gap')
    computed = {}
    for variant, image in [('baseline', BASELINE), ('candidate', CANDIDATE)]:
        require(environment.get('apps', {}).get(variant, {}).get('image') == image, f'{variant} environment image mismatch')
        v = result['variants'][variant]
        require(not v.get('errors'), f'{variant} request errors')
        require(abs(v['requests'] - (WARMUP + SECONDS) * RPS) <= 8, f'{variant} request count incomplete')
        computed[variant] = {}
        total = 0
        for kind in ['stream', 'nonstream']:
            values = series[variant][kind]
            require(abs(len(values) - SECONDS * RPS // 2) <= 8, f'{variant}/{kind} measured sample count incomplete')
            total += len(values)
            declared = v['latency'][kind]
            require(declared['n'] == len(values), f'{variant}/{kind} sample count inconsistent')
            metrics = {f'p{int(q*100)}_ms':quantile(values,q) for q in [.5,.95,.99]}
            for key, actual in metrics.items():
                require(math.isclose(actual, declared[key], rel_tol=1e-9, abs_tol=1e-9), f'{variant}/{kind}/{key} inconsistent with raw samples')
            computed[variant][kind] = {'n':len(values), **metrics}
        require(abs(total - SECONDS * RPS) <= 8, f'{variant} total measured sample count incomplete')
        stable = [row[variant]['VmRSS'] for row in memory if row['elapsed_s'] >= WARMUP + SECONDS / 2]
        require(len(stable) >= 100 and all(isinstance(v, (int,float)) and v > 0 for v in stable), 'insufficient valid stable RSS observations')
        median = statistics.median(stable)
        require(median == v['stable_rss_median'] and len(stable) == v['stable_rss_samples'], f'{variant} RSS result inconsistent with raw observations')
        computed[variant]['stable_rss_median'] = median
        computed[variant]['stable_rss_samples'] = len(stable)
    upstream = result['upstream']
    require(upstream['invalid'] == upstream['duplicates'] == 0, 'invalid or duplicated upstream request')
    require(upstream.get('tracker', {}).get('bytes', 2**31) < 2*1024*1024, 'fake duplicate tracker memory is unbounded')
    require(upstream.get('memory', {}).get('heapUsed', 2**31) < 128*1024*1024, 'fake V8 heap exceeds bounded-harness gate')
    require(upstream['requests'] == sum(v['requests'] for v in result['variants'].values()) + 20, 'upstream/client request counts differ')
    run_id = result['run_id']
    require(run_id == FINAL_RUN_ID, 'unrecognized run identity')
    expected_names = {run_id, run_id+'-fake', run_id+'-load'} | {run_id+'-'+v+'-'+s for v in ['baseline','candidate'] for s in ['app','db','redis']}
    cleanup = result.get('cleanup', [])
    require(len(cleanup) == 9 and {x['name'] for x in cleanup} == expected_names and all(x.get('removed') is True for x in cleanup), 'resource cleanup evidence incomplete')
    require(result.get('cleanup_complete') is True, 'cleanup not complete')
    degradation = {kind:(computed['candidate'][kind]['p95_ms']/computed['baseline'][kind]['p95_ms']-1)*100 for kind in ['stream','nonstream']}
    degradation['stable_rss'] = (computed['candidate']['stable_rss_median']/computed['baseline']['stable_rss_median']-1)*100
    require(all(degradation[k] <= 10 for k in ['stream','nonstream']), 'P95 exceeds +10% gate')
    require(degradation['stable_rss'] <= 20, 'stable RSS exceeds +20% gate')
    return {'passed':True, 'source_commit':SOURCE, 'run_id':run_id, 'actual_elapsed_s':result['actual_elapsed_s'], 'recomputed':computed, 'degradation_pct':degradation, 'limitations':['API-key HTTP/SSE full-image workload, not OAuth/WS load','Official v0.2.5 source rebuilt with pinned images, not an official registry binary','No live A/C or TLS/JA3 equivalence claim']}

def verify_directory(root: Path) -> dict:
    soak = root / FINAL_RUN_DIR
    result = load(soak/'result.json')
    report = verify_soak(result, load(soak/'memory.json'), {v:load(soak/(v+'-latencies.json')) for v in ['baseline','candidate']}, load(soak/'environment.json'))
    require(load(root/(FINAL_RUN_DIR+'-driver-result.json'))['exit'] == 0, 'driver did not exit successfully')
    for name, expected in HARNESS.items():
        require(digest(root/'fullimage-harness-v3'/name) == expected, 'persisted harness file changed')
    for prefix in ['linux-full-unit-release3','linux-race-release3']:
        value = load(root/(prefix+'-result.json'))
        require(value['source_commit'] == SOURCE and value['go_exit'] == 0 and not value['failed'], 'test source/status mismatch')
        require(value['required_run_pass'] == value['required'] and not value['missing_required'] and not value['skipped_required'], 'required tests absent or skipped')
        require(digest(root/(prefix+'.jsonl')) == value['sha256'], 'raw Go test log hash mismatch')
        if prefix == 'linux-race-release3':
            require(value['race_warnings'] == 0 and value['required'] == 211, 'race evidence mismatch')
        else:
            require(value['top_passed'] == 10890 and value['required'] == 67, 'full suite evidence mismatch')
    runtime = load(root/'candidate-runtime-release-result.json')
    require(runtime['source_commit'] == SOURCE and runtime['passed'] is True and runtime['network_internal'] is True and runtime['production_used'] is False, 'runtime smoke invalid')
    export = load(root/'candidate-release-export.json')
    require(export['source_commit'] == SOURCE and export['image_digest'] == CANDIDATE.split('@')[1] and export['production_deployed'] is False, 'candidate export identity mismatch')
    tar = root/'sub2api-r1-8905cbb82d4b.docker.tar'
    require(tar.stat().st_size == export['docker_tar_bytes'] and digest(tar) == export['docker_tar_sha256'], 'exported Docker archive hash mismatch')
    require(digest(root/('source-'+SOURCE+'.tar')) == load(root/'b8-release-evidence-reverified.json')['source_archive_sha256'], 'source archive hash mismatch')
    report['export'] = export
    report['artifact_hashes'] = {str(p.relative_to(root)):digest(p) for p in [soak/'result.json',soak/'memory.json',soak/'environment.json',soak/'baseline-latencies.json',soak/'candidate-latencies.json',root/'candidate-release-export.json']}
    return report

def main() -> int:
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('evidence_root', type=Path)
    args=p.parse_args()
    try:
        report=verify_directory(args.evidence_root)
    except (VerificationError, OSError, ValueError, KeyError, TypeError) as e:
        print(json.dumps({'passed':False,'error':str(e)},indent=2),file=sys.stderr)
        return 1
    output=args.evidence_root/'b8-independent-acceptance.json'
    output.write_text(json.dumps(report,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(report,indent=2))
    return 0

if __name__ == '__main__':
    raise SystemExit(main())
