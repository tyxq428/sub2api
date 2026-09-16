#!/usr/bin/env python3
"""Full-image B8 load test on isolated Docker resources.
The long-lived HTTP load agent runs inside the same internal Docker network as
both applications and the fake upstream, avoiding host-to-bridge route changes.
No ports are published; all credentials/data are synthetic. This API-key HTTP/SSE
benchmark does NOT establish live OAuth/TLS equivalence.
"""
from __future__ import annotations
import argparse, hashlib, ipaddress, json, os, pathlib, statistics, subprocess, time
P=argparse.ArgumentParser(); P.add_argument('--run-id',required=True); P.add_argument('--baseline',required=True); P.add_argument('--candidate',required=True); P.add_argument('--output',required=True); P.add_argument('--seconds',type=int,default=7200); P.add_argument('--warmup',type=int,default=300); P.add_argument('--rps',type=int,default=20); A=P.parse_args()
assert A.run_id.startswith('sub2api-r1-b8-') and A.run_id.replace('-','').isalnum(); assert 10<=A.seconds<=7200 and 0<=A.warmup<=600 and 4<=A.rps<=40; assert '@sha256:' in A.baseline and '@sha256:' in A.candidate
OUT=pathlib.Path(A.output); OUT.mkdir(parents=True,exist_ok=False); ROOT=pathlib.Path(__file__).resolve().parent; os.environ['DOCKER_HOST']='unix:///var/run/docker.sock'
PG='postgres@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2'; REDIS='redis@sha256:09160599abd229764c0fb44cb6be640294e1d360a54b19985ab4843dcf2d90f1'; NODE='node@sha256:50c8e8ca1d27439048670df5883f32d57cf81cff6233222c893fd0d9884cbd81'
NET=A.run_id; names=[]; network_created=False; mem=[]; apps={}; keys={}; fake_name=NET+'-fake'; load_name=NET+'-load'; state_history=[]
def write(name,obj):
 p=OUT/name; tmp=p.with_suffix(p.suffix+'.tmp'); tmp.write_text(json.dumps(obj,indent=2),encoding='utf-8'); tmp.replace(p)
def docker(*args,input=None,check=True,timeout=30):
 p=subprocess.run(['docker',*args],input=input,text=True,capture_output=True,timeout=timeout)
 if check and p.returncode: raise RuntimeError('docker '+str(args[:3])+': '+p.stderr[-1500:])
 return p
def run_container(name,image,opts,args=()):
 assert name.startswith(NET+'-');
 if docker('container','inspect',name,check=False).returncode==0: raise RuntimeError('RESOURCE_CONFLICT '+name)
 docker('run','--pull=never','-d','--name',name,'--label','sub2api.r1.b8='+A.run_id,'--network',NET,'--restart=no','--log-opt','max-size=10m','--log-opt','max-file=2',*opts,image,*args,timeout=60); names.append(name)
 info=json.loads(docker('inspect',name).stdout)[0]; assert not info['HostConfig']['PortBindings']; ip=info['NetworkSettings']['Networks'][NET]['IPAddress']; assert ipaddress.ip_address(ip).is_private; return ip
def sql(name,text):
 assert name in names and name.endswith('-db'); return docker('exec','-i',name,'psql','-v','ON_ERROR_STOP=1','-U','sub2api_r1','-d','sub2api_r1','-t','-A',input=text).stdout
def wait_local_http(name,url,attempts=75):
 for _ in range(attempts):
  p=docker('exec',name,'wget','-q','-T','2','-O','-',url,check=False,timeout=5)
  if p.returncode==0:return
  if json.loads(docker('inspect',name).stdout)[0]['State']['Running'] is False: raise RuntimeError(name+' exited')
  time.sleep(2)
 raise RuntimeError(name+' http not ready')
def provision(v,image):
 db=NET+'-'+v+'-db'; redis=NET+'-'+v+'-redis'; app=NET+'-'+v+'-app'
 run_container(db,PG,['--cpus=1','--memory=1g','--tmpfs','/var/lib/postgresql:rw,size=768m','-e','PGDATA=/var/lib/postgresql/r1data','-e','POSTGRES_DB=sub2api_r1','-e','POSTGRES_USER=sub2api_r1','-e','POSTGRES_PASSWORD=SyntheticR1Database_Only_37'])
 run_container(redis,REDIS,['--cpus=1','--memory=256m'],['redis-server','--save','','--appendonly','no'])
 for _ in range(45):
  if docker('exec',db,'pg_isready','-U','sub2api_r1','-d','sub2api_r1',check=False).returncode==0:break
  time.sleep(1)
 else: raise RuntimeError('fresh postgres not ready')
 env={'AUTO_SETUP':'true','SERVER_HOST':'0.0.0.0','SERVER_PORT':'8080','SERVER_MODE':'release','RUN_MODE':'standard','DATABASE_HOST':db,'DATABASE_PORT':'5432','DATABASE_USER':'sub2api_r1','DATABASE_PASSWORD':'SyntheticR1Database_Only_37','DATABASE_DBNAME':'sub2api_r1','DATABASE_SSLMODE':'disable','DATABASE_MAX_OPEN_CONNS':'8','DATABASE_MAX_IDLE_CONNS':'2','REDIS_HOST':redis,'REDIS_PORT':'6379','REDIS_POOL_SIZE':'16','REDIS_MIN_IDLE_CONNS':'2','ADMIN_EMAIL':'r1-admin@example.invalid','ADMIN_PASSWORD':'SyntheticR1Admin_Only_37!','JWT_SECRET':'r1-synthetic-jwt-no-production-access-012345678901234567890123456789','TOTP_ENCRYPTION_KEY':'0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef','TZ':'UTC','GOMAXPROCS':'2','SECURITY_URL_ALLOWLIST_ENABLED':'false','SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS':'true','SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP':'true'}
 opts=['--cpus=2','--memory=2g','--security-opt','no-new-privileges','--tmpfs','/app/data:rw,size=128m']
 for k,val in env.items(): opts+=['-e',k+'='+val]
 run_container(app,image,opts); apps[v]={'name':app,'url':'http://'+app+':8080','db':db,'image':image}; wait_local_http(app,'http://127.0.0.1:8080/health'); assert sql(db,'SELECT count(*) FROM accounts;').strip()=='0'
 key='sk-r1-synthetic-'+v+'-only-8905cbb82'; keys[v]=key; creds=json.dumps({'api_key':'synthetic-upstream-r1-only','base_url':'http://'+fake_name+':8081'})
 seed="""BEGIN;
INSERT INTO users(id,email,password_hash,balance,concurrency,status) VALUES(8808,'r1-load@example.invalid','synthetic-disabled-password',1000000000,16,'active');
INSERT INTO groups(id,name,platform,rate_multiplier,is_exclusive,status) VALUES(8808,'r1-fake-openai','openai',1,false,'active');
INSERT INTO accounts(id,name,platform,type,credentials,extra,concurrency,priority,status,schedulable) VALUES(8808,'r1-fake-upstream','openai','apikey','%s','{\"openai_passthrough\":true,\"upstream_billing_probe_enabled\":false}',16,1,'active',true);
INSERT INTO account_groups(account_id,group_id,priority) VALUES(8808,8808,1);
INSERT INTO api_keys(user_id,key,name,group_id,status) VALUES(8808,'%s','r1-fake-load',8808,'active');
COMMIT;"""%(creds,key); sql(db,seed); print('PROVISIONED',v,flush=True)
def process_memory(name):
 p=docker('exec',name,'cat','/proc/1/status',check=False,timeout=5)
 if p.returncode!=0:return None
 return {l.split(':')[0]:int(l.split()[1])*1024 for l in p.stdout.splitlines() if l.startswith(('VmRSS:','VmHWM:','VmSize:'))}
def inspect_states():
 states={}
 for name in names:
  p=docker('inspect',name,check=False,timeout=5)
  if p.returncode!=0: states[name]={'missing':True}; continue
  st=json.loads(p.stdout)[0]['State']; states[name]={k:st.get(k) for k in ['Status','Running','ExitCode','OOMKilled','Error','StartedAt','FinishedAt']}
 return states
def sample_memory(elapsed):
 row={'elapsed_s':round(elapsed,2)}
 for v,a in apps.items(): row[v]=process_memory(a['name']) or {}
 if fake_name in names: row['fake']=process_memory(fake_name) or {}
 if load_name in names: row['load']=process_memory(load_name) or {}
 mem.append(row)
def quantile(values,q):
 values=sorted(values); assert values; return values[min(len(values)-1,int((len(values)-1)*q))]
result={'passed':False,'run_id':A.run_id,'scope':'full-image API-key HTTP responses and SSE to fake upstream; not live OAuth/TLS','real_model_requests':0,'parameters':vars(A)}
try:
 if docker('network','inspect',NET,check=False).returncode==0: raise RuntimeError('RESOURCE_CONFLICT network')
 docker('network','create','--internal','--label','sub2api.r1.b8='+A.run_id,NET); network_created=True; ni=json.loads(docker('network','inspect',NET).stdout)[0]; assert ni['Internal']; result['network_internal']=True
 run_container(fake_name,NODE,['--cpus=1','--memory=512m','--security-opt','no-new-privileges','--mount',f'type=bind,source={ROOT},target=/harness,readonly'],['node','/harness/fake_upstream.cjs'])
 for _ in range(30):
  p=docker('exec',fake_name,'node','-e',"fetch('http://127.0.0.1:8081/stats').then(r=>{if(!r.ok)process.exit(2)}).catch(()=>process.exit(3))",check=False,timeout=5)
  if p.returncode==0:break
  time.sleep(1)
 else: raise RuntimeError('fake upstream not ready')
 for v,img in [('baseline',A.baseline),('candidate',A.candidate)]: provision(v,img)
 config={'run_id':A.run_id,'seconds':A.seconds,'warmup':A.warmup,'rps':A.rps,'apps':{v:{'url':apps[v]['url'],'key':keys[v]} for v in apps},'fake_url':'http://'+fake_name+':8081'}; write('agent-config.json',config)
 harness={'controller_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'fixture_sha256':hashlib.sha256((ROOT/'fake_upstream.cjs').read_bytes()).hexdigest(),'agent_sha256':hashlib.sha256((ROOT/'load_agent.cjs').read_bytes()).hexdigest(),'tracker_sha256':hashlib.sha256((ROOT/'duplicate_tracker.cjs').read_bytes()).hexdigest()}
 write('environment.json',{'apps':apps,'network':{'name':NET,'internal':True},'load_agent':load_name,**harness})
 host_start=time.monotonic(); expected=time.time()+A.warmup+A.seconds+5; result['started_utc']=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()); result['expected_finish_epoch']=expected
 run_container(load_name,NODE,['--cpus=2','--memory=1g','--security-opt','no-new-privileges','--mount',f'type=bind,source={ROOT},target=/harness,readonly','--mount',f'type=bind,source={OUT},target=/out'],['node','/harness/load_agent.cjs'])
 while True:
  if (OUT/'STOP').exists(): raise RuntimeError('cancelled by STOP sentinel')
  sample_memory(max(0,time.monotonic()-host_start)); write('memory.json',mem)
  states=inspect_states(); state_history.append({'elapsed_s':round(time.monotonic()-host_start,2),'states':states}); write('container-states.json',state_history)
  info=json.loads(docker('inspect',load_name).stdout)[0]; running=info['State']['Running']; status={'phase':'running','elapsed_s':round(time.monotonic()-host_start,2),'expected_finish_epoch':expected,'latest_memory':mem[-1],'states':states}
  for name,st in states.items():
   if name!=load_name and st.get('Running') is not True: raise RuntimeError('infrastructure container exited: '+name+' state='+json.dumps(st,sort_keys=True))
  if (OUT/'agent-status.json').is_file():
   try: status['agent']=json.loads((OUT/'agent-status.json').read_text(encoding='utf-8'))
   except Exception: pass
  write('status.json',status)
  if not running:
   if info['State']['ExitCode']!=0: raise RuntimeError('load agent exit '+str(info['State']['ExitCode']))
   break
  time.sleep(30)
 agent=json.loads((OUT/'agent-result.json').read_text(encoding='utf-8')); assert agent.get('passed') is True and not agent.get('error'); result['actual_elapsed_s']=agent['actual_elapsed_s']; result['finished_utc']=agent['finished_utc']; result['upstream']=agent['upstream']; result['variants']={}
 for v in apps:
  av=agent['variants'][v]; stable=[x[v]['VmRSS'] for x in mem if x['elapsed_s']>=A.warmup+A.seconds/2]; early=[x[v]['VmRSS'] for x in mem if A.warmup<=x['elapsed_s']<A.warmup+A.seconds/4]; final=[x[v]['VmRSS'] for x in mem if x['elapsed_s']>=A.warmup+3*A.seconds/4]
  if not stable: stable=[x[v]['VmRSS'] for x in mem]
  result['variants'][v]={**av,'stable_rss_median':statistics.median(stable),'stable_rss_samples':len(stable),'rss_peak':max(x[v]['VmRSS'] for x in mem),'early_rss_median':statistics.median(early) if early else None,'final_rss_median':statistics.median(final) if final else None}
 b=result['variants']['baseline']; c=result['variants']['candidate']; result['degradation_pct']={k:(c['latency'][k]['p95_ms']/b['latency'][k]['p95_ms']-1)*100 for k in ['stream','nonstream']}; result['degradation_pct']['stable_rss']=(c['stable_rss_median']/b['stable_rss_median']-1)*100
 result['gates']={'zero_errors':not any(v['errors'] for v in result['variants'].values()),'no_upstream_duplicates':result['upstream']['duplicates']==0,'valid_upstream_inputs':result['upstream']['invalid']==0,'upstream_count_exact':result['upstream']['requests']==sum(v['requests'] for v in result['variants'].values())+20,'duration_complete':result['actual_elapsed_s']>=A.warmup+A.seconds-1,'sample_count_complete':all(sum(x['n'] for x in v['latency'].values())>=A.seconds*A.rps-8 for v in result['variants'].values()),'p95_within_10pct':all(result['degradation_pct'][k]<=10 for k in ['stream','nonstream']),'stable_rss_within_20pct':result['degradation_pct']['stable_rss']<=20,'fake_tracker_bounded':result['upstream'].get('tracker',{}).get('bytes',2**31)<2*1024*1024 and result['upstream'].get('memory',{}).get('heapUsed',2**31)<128*1024*1024}; result['passed']=all(result['gates'].values())
except Exception as e: result['error']=repr(e)
finally:
 cleanup=[]
 try: write('container-states-final.json',inspect_states())
 except Exception: pass
 for name in reversed(names):
  try:
   info=json.loads(docker('inspect',name).stdout)[0]
   if info['Config']['Labels'].get('sub2api.r1.b8')!=A.run_id: raise RuntimeError('label ownership mismatch')
   if name.endswith(('-app','-fake','-load')): (OUT/(name+'-tail.log')).write_text(docker('logs','--tail','2000',name,check=False).stdout+docker('logs','--tail','2000',name,check=False).stderr,encoding='utf-8')
   p=docker('rm','-f',name,check=False); cleanup.append({'name':name,'removed':p.returncode==0})
  except Exception as e: cleanup.append({'name':name,'error':repr(e)})
 if network_created:
  p=docker('network','rm',NET,check=False); cleanup.append({'name':NET,'removed':p.returncode==0})
 result['cleanup']=cleanup; result['cleanup_complete']=all(x.get('removed') for x in cleanup); result['passed']=result['passed'] and result['cleanup_complete']; write('result.json',result); write('status.json',{'phase':'completed' if result['passed'] else 'failed','result':result}); print(json.dumps(result,indent=2),flush=True)
raise SystemExit(0 if result['passed'] else 1)
