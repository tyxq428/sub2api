"""Synthetic unit fixtures for the verifier; never accepted as real soak evidence."""
import copy
import json
from pathlib import Path
import tempfile
import unittest

from verify import (BASELINE, CANDIDATE, HARNESS, VerificationError,
                    quantile, verify_directory, verify_soak)

class VerifierTests(unittest.TestCase):
    def setUp(self):
        self.run_id='sub2api-r1-b8-soak01'
        self.memory=[{'elapsed_s':i, 'baseline':{'VmRSS':100000000}, 'candidate':{'VmRSS':100000000}} for i in range(0,7501,30)]
        self.series={v:{k:[50.0]*72000 for k in ['stream','nonstream']} for v in ['baseline','candidate']}
        self.env={'network':{'internal':True},'controller_sha256':HARNESS['run.py'],'agent_sha256':HARNESS['load_agent.cjs'],'fixture_sha256':HARNESS['fake_upstream.cjs'],'apps':{'baseline':{'image':BASELINE},'candidate':{'image':CANDIDATE}}}
        names={self.run_id,self.run_id+'-fake',self.run_id+'-load'}|{self.run_id+'-'+v+'-'+s for v in ['baseline','candidate'] for s in ['app','db','redis']}
        gates=['zero_errors','no_upstream_duplicates','valid_upstream_inputs','upstream_count_exact','duration_complete','sample_count_complete','p95_within_10pct','stable_rss_within_20pct']
        self.result={'parameters':{'seconds':7200,'warmup':300,'rps':20,'baseline':BASELINE,'candidate':CANDIDATE},'network_internal':True,'real_model_requests':0,'actual_elapsed_s':7500.1,'passed':True,'gates':dict.fromkeys(gates,True),'run_id':self.run_id,'upstream':{'requests':300020,'duplicates':0,'invalid':0},'cleanup':[{'name':n,'removed':True} for n in names],'cleanup_complete':True,'variants':{}}
        for v in ['baseline','candidate']:
            self.result['variants'][v]={'requests':150000,'errors':[],'latency':{k:{'n':72000,'p50_ms':50.0,'p95_ms':50.0,'p99_ms':50.0} for k in ['stream','nonstream']},'stable_rss_median':100000000,'stable_rss_samples':121}
    def check(self):
        return verify_soak(self.result,self.memory,self.series,self.env)
    def test_consistent_in_memory_fixture(self):
        self.assertTrue(self.check()['passed'])
    def test_short_smoke_rejected(self):
        self.result['parameters']['seconds']=30
        with self.assertRaisesRegex(VerificationError,'7200'): self.check()
    def test_partial_wall_duration_rejected(self):
        self.result['actual_elapsed_s']=7400
        with self.assertRaisesRegex(VerificationError,'duration'): self.check()
    def test_stale_image_rejected(self):
        self.result['parameters']['candidate']='old-image'
        with self.assertRaisesRegex(VerificationError,'digest'): self.check()
    def test_wrong_harness_rejected(self):
        self.env['controller_sha256']='0'*64
        with self.assertRaisesRegex(VerificationError,'harness'): self.check()
    def test_request_errors_rejected(self):
        self.result['variants']['candidate']['errors']=['timeout']
        with self.assertRaisesRegex(VerificationError,'request errors'): self.check()
    def test_edited_p95_summary_rejected(self):
        self.result['variants']['candidate']['latency']['stream']['p95_ms']=49
        with self.assertRaisesRegex(VerificationError,'raw samples'): self.check()
    def test_too_few_samples_rejected(self):
        self.series['candidate']['stream']=self.series['candidate']['stream'][:10]
        with self.assertRaisesRegex(VerificationError,'sample count'): self.check()
    def test_real_p95_regression_rejected_despite_pass_flag(self):
        self.series['candidate']['stream']=[70.0]*72000
        self.result['variants']['candidate']['latency']['stream'].update(p50_ms=70,p95_ms=70,p99_ms=70)
        with self.assertRaisesRegex(VerificationError,'P95 exceeds'): self.check()
    def test_real_rss_regression_rejected_despite_pass_flag(self):
        for row in self.memory: row['candidate']['VmRSS']=130000000
        self.result['variants']['candidate']['stable_rss_median']=130000000
        with self.assertRaisesRegex(VerificationError,'RSS exceeds'): self.check()
    def test_missing_memory_window_rejected(self):
        self.memory=self.memory[:100]
        with self.assertRaisesRegex(VerificationError,'observation window'): self.check()
    def test_memory_gap_rejected(self):
        del self.memory[10:20]
        with self.assertRaisesRegex(VerificationError,'excessive gap'): self.check()
    def test_duplicate_upstream_rejected(self):
        self.result['upstream']['duplicates']=1
        with self.assertRaisesRegex(VerificationError,'duplicated'): self.check()
    def test_count_mismatch_rejected(self):
        self.result['upstream']['requests']+=1
        with self.assertRaisesRegex(VerificationError,'counts differ'): self.check()
    def test_cleanup_cannot_reference_other_resources(self):
        self.result['cleanup'][0]['name']='sub2api-local'
        with self.assertRaisesRegex(VerificationError,'cleanup'): self.check()
    def test_incomplete_directory_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            with self.assertRaisesRegex(VerificationError,'MISSING_REQUIRED'): verify_directory(Path(folder))
    def test_nonfinite_latency_rejected(self):
        with self.assertRaisesRegex(VerificationError,'nonfinite'): quantile([float('nan')],.95)
    def test_quantile_contract(self):
        self.assertEqual(quantile(list(range(1,101)),.95),95)

if __name__ == '__main__': unittest.main()
