#!/usr/bin/env python3
"""Full-image B8 load test. Only explicitly named fresh Docker resources are used.
Run on the authorized WSL host. The Docker network is internal and no ports are
published. Both app images are immutable digests. All credentials/data are fake.
This API-key HTTP/SSE benchmark does NOT establish live OAuth/TLS equivalence.
"""
from __future__ import annotations
import argparse, concurrent.futures, hashlib, ipaddress, json, os, pathlib
import statistics, subprocess, threading, time, urllib.request, urllib.error

P=argparse.ArgumentParser()
P.add_argument('--run-id',required=True)
P.add_argument('--baseline',required=True)
P.add_argument('--candidate',required=True)
P.add_argument('--output',required=True)
P.add_argument('--seconds',type=int,default=7200)
P.add_argument('--warmup',type=int,default=300)
P.add_argument('--rps',type=int,default=20)
A=P.parse_args()
assert A.run_id.startswith('sub2api-r1-b8-') and A.run_id.replace('-','').isalnum()
assert 10<=A.seconds<=7200 and 0<=A.warmup<=600 and 4<=A.rps<=40
assert '@sha256:' in A.baseline and '@sha256:' in A.candidate
OUT=pathlib.Path(A.output);OUT.mkdir(parents=True,exist_ok=False)
ROOT=pathlib.Path(__file__).resolve().parent
os.environ['DOCKER_HOST']='unix:///var/run/docker.sock'
PG='postgres@sha256:d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2'
REDIS='redis@sha256:09160599abd229764c0fb44cb6be640294e1d360a54b19985ab4843dcf2d90f1'
NODE='node@sha256:50c8e8ca1d27439048670df5883f32d57cf81cff6233222c893fd0d9884cbd81'
NET=A.run_id; names=[]; network_created=False; stop=threading.Event()
lock=threading.Lock(); data={v:{'requests':0,'errors':[],'samples':{'stream':[],'nonstream':[]},'late_slots':0} for v in ['baseline','candidate']}
mem=[]; apps={}; keys={}; fake_ip=''; checkpoint={}

def write(name,obj):
 p=OUT/name;tmp=p.with_suffix(p.suffix+'.tmp');tmp.write_text(json.dumps(obj,indent=2),encoding='utf-8');tmp.replace(p)
def docker(*args,input=None,check=True,timeout=30):
 p=subprocess.run(['docker',*args],input=input,text=True,capture_output=True,timeout=timeout)
 if check and p.returncode: raise RuntimeError('docker '+str(args[:3])+': '+p.stderr[-1500:])
 return p

def run_container(name,image,opts,args=()):
 assert name.startswith(NET+'-')
 if docker('container','inspect',name,check=False).returncode==0: raise RuntimeError('RESOURCE_CONFLICT '+name)
 docker('run','--pull=never','-d','--name',name,'--label','sub2api.r1.b8='+A.run_id,'--network',NET,'--restart=no','--log-opt','max-size=10m','--log-opt','max-file=2',*opts,image,*args,timeout=60);names.append(name)
 info=json.loads(docker('inspect',name).stdout)[0]
 assert not info['HostConfig']['PortBindings']
 ip=info['NetworkSettings']['Networks'][NET]['IPAddress'];assert ipaddress.ip_address(ip).is_private
 return ip

def http(url,body=None,headers=None):
 req=urllib.request.Request(url,data=None if body is None else json.dumps(body).encode(),headers=headers or {})
 with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req,timeout=10) as r:
  raw=r.read();return r.status,raw

def sql(name,text):
 assert name in names and name.endswith('-db')
 return docker('exec','-i',name,'psql','-v','ON_ERROR_STOP=1','-U','sub2api_r1','-d','sub2api_r1','-t','-A',input=text).stdout

def provision(v,image):
 db=NET+'-'+v+'-db';redis=NET+'-'+v+'-redis';app=NET+'-'+v+'-app'
 run_container(db,PG,['--cpus=1','--memory=1g','--tmpfs','/var/lib/postgresql:rw,size=768m','-e','PGDATA=/var/lib/postgresql/r1data','-e','POSTGRES_DB=sub2api_r1','-e','POSTGRES_USER=sub2api_r1','-e','POSTGRES_PASSWORD=SyntheticR1Database_Only_37'])
 run_container(redis,REDIS,['--cpus=1','--memory=256m'],['redis-server','--save','','--appendonly','no'])
 for _ in range(45):
  if docker('exec',db,'pg_isready','-U','sub2api_r1','-d','sub2api_r1',check=False).returncode==0:break
  time.sleep(1)
 else: raise RuntimeError('fresh postgres not ready')
 env={'AUTO_SETUP':'true','SERVER_HOST':'0.0.0.0','SERVER_PORT':'8080','SERVER_MODE':'release','RUN_MODE':'standard','DATABASE_HOST':db,'DATABASE_PORT':'5432','DATABASE_USER':'sub2api_r1','DATABASE_PASSWORD':'SyntheticR1Database_Only_37','DATABASE_DBNAME':'sub2api_r1','DATABASE_SSLMODE':'disable','DATABASE_MAX_OPEN_CONNS':'8','DATABASE_MAX_IDLE_CONNS':'2','REDIS_HOST':redis,'REDIS_PORT':'6379','REDIS_POOL_SIZE':'16','REDIS_MIN_IDLE_CONNS':'2','ADMIN_EMAIL':'r1-admin@example.invalid','ADMIN_PASSWORD':'SyntheticR1Admin_Only_37!','JWT_SECRET':'r1-synthetic-jwt-no-production-access-012345678901234567890123456789','TOTP_ENCRYPTION_KEY':'0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef','TZ':'UTC','GOMAXPROCS':'2','SECURITY_URL_ALLOWLIST_ENABLED':'false','SECURITY_URL_ALLOWLIST_ALLOW_PRIVATE_HOSTS':'true','SECURITY_URL_ALLOWLIST_ALLOW_INSECURE_HTTP':'true'}
 opts=['--cpus=2','--memory=2g','--security-opt','no-new-privileges','--tmpfs','/app/data:rw,size=128m']
 for k,val in env.items():opts+=['-e',k+'='+val]
 ip=run_container(app,image,opts);apps[v]={'name':app,'url':'http://'+ip+':8080','db':db,'image':image}
 for _ in range(75):
  try:
   if http(apps[v]['url']+'/health')[0]==200:break
  except Exception:pass
  if json.loads(docker('inspect',app).stdout)[0]['State']['Running'] is False:raise RuntimeError('fresh app exited')
  time.sleep(2)
 else:raise RuntimeError('fresh app not ready')
 assert sql(db,'SELECT count(*) FROM accounts;').strip()=='0'
 key='sk-r1-synthetic-'+v+'-only-8905cbb82';keys[v]=key
 creds=json.dumps({'api_key':'synthetic-upstream-r1-only','base_url':'http://'+fake_ip+':8081'})
 seed="""BEGIN;
INSERT INTO users(id,email,password_hash,balance,concurrency,status) VALUES(8808,'r1-load@example.invalid','synthetic-disabled-password',1000000000,16,'active');
INSERT INTO groups(id,name,platform,rate_multiplier,is_exclusive,status) VALUES(8808,'r1-fake-openai','openai',1,false,'active');
INSERT INTO accounts(id,name,platform,type,credentials,extra,concurrency,priority,status,schedulable) VALUES(8808,'r1-fake-upstream','openai','apikey','%s','{"openai_passthrough":true,"upstream_billing_probe_enabled":false}',16,1,'active',true);
INSERT INTO account_groups(account_id,group_id,priority) VALUES(8808,8808,1);
INSERT INTO api_keys(user_id,key,name,group_id,status) VALUES(8808,'%s','r1-fake-load',8808,'active');
COMMIT;"""%(creds,key)
 sql(db,seed)
 # Seed before first gateway read; no production accounts or real tokens exist.
 print('PROVISIONED',v,flush=True)

def request(v,worker,n):
 stream=n%2==0;kind='stream' if stream else 'nonstream'
 rid=f'{A.run_id}-{v}-{worker}-{n}'
 body={'model':'gpt-5.6-sol','input':'r1:'+rid+'|'+'x'*4096,'stream':stream}
 before=time.monotonic_ns()
 status,raw=http(apps[v]['url']+'/v1/responses',body,{'Content-Type':'application/json','Authorization':'Bearer '+keys[v],'session-id':'r1-'+v+'-'+str(worker)})
 elapsed=(time.monotonic_ns()-before)/1e6
 if status!=200:raise RuntimeError('http status '+str(status))
 if stream:
  events=[json.loads(l[6:]) for l in raw.decode().splitlines() if l.startswith('data: ') and l[6:]!='[DONE]']
  complete=[e['response'] for e in events if e.get('type')=='response.completed']
  assert len(complete)==1 and complete[0]['id']=='resp_'+rid and complete[0]['status']=='completed',raw[:300]
  assert any(e.get('type')=='response.output_text.delta' and e.get('delta')=='r1-ok' for e in events),raw[:300]
 else:
  response=json.loads(raw);assert response['id']=='resp_'+rid and response['status']=='completed' and response['output'][0]['content'][0]['text']=='r1-ok',raw[:300]
 return kind,elapsed

def worker(v,w,start,warm_end,end):
 interval=4/A.rps;next_tick=start+w/A.rps;n=0
 while next_tick<end and not stop.is_set():
  delay=next_tick-time.monotonic()
  if delay>0:stop.wait(delay)
  if stop.is_set():break
  try:kind,elapsed=request(v,w,n)
  except Exception as e:
   with lock:data[v]['errors'].append(str(e)[:1000])
   stop.set();break
  with lock:
   data[v]['requests']+=1
   if next_tick>=warm_end:data[v]['samples'][kind].append(elapsed)
   if time.monotonic()-next_tick>interval:data[v]['late_slots']+=1
  n+=1;next_tick+=interval

def sample_memory(elapsed):
 row={'elapsed_s':round(elapsed,2)}
 for v,a in apps.items():
  s=docker('exec',a['name'],'cat','/proc/1/status').stdout
  row[v]={l.split(':')[0]:int(l.split()[1])*1024 for l in s.splitlines() if l.startswith(('VmRSS:','VmHWM:','VmSize:'))}
 mem.append(row)

def quantile(values,q):
 values=sorted(values);assert values;return values[min(len(values)-1,int((len(values)-1)*q))]

result={'passed':False,'run_id':A.run_id,'scope':'full-image API-key HTTP responses and SSE to fake upstream; not live OAuth/TLS','real_model_requests':0,'parameters':vars(A)}
try:
 if docker('network','inspect',NET,check=False).returncode==0:raise RuntimeError('RESOURCE_CONFLICT network')
 docker('network','create','--internal','--label','sub2api.r1.b8='+A.run_id,NET);network_created=True
 ni=json.loads(docker('network','inspect',NET).stdout)[0];assert ni['Internal'];result['network_internal']=True
 fake_ip=run_container(NET+'-fake',NODE,['--cpus=1','--memory=512m','--security-opt','no-new-privileges','--mount',f'type=bind,source={ROOT},target=/harness,readonly'],['node','/harness/fake_upstream.cjs'])
 for v,img in [('baseline',A.baseline),('candidate',A.candidate)]:provision(v,img)
 # Fail fast on both surfaces before investing two hours.
 for v in apps:
  for n in range(10):request(v,'smoke',n)
 print('PREFLIGHT_PASS',flush=True)
 write('environment.json',{'apps':apps,'network':{'name':NET,'internal':True},'fixture_sha256':hashlib.sha256((ROOT/'fake_upstream.cjs').read_bytes()).hexdigest(),'controller_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()})
 start=time.monotonic()+1;warm_end=start+A.warmup;end=warm_end+A.seconds
 result['started_utc']=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime());result['expected_finish_epoch']=time.time()+1+A.warmup+A.seconds
 write('status.json',{'phase':'running',**result})
 with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
  futures=[pool.submit(worker,v,w,start,warm_end,end) for v in apps for w in range(4)]
  while not all(f.done() for f in futures):
   if (OUT/'STOP').exists():stop.set();raise RuntimeError('cancelled by STOP sentinel')
   sample_memory(max(0,time.monotonic()-start))
   with lock:
    progress={v:{'requests':d['requests'],'errors':len(d['errors']),'measured':{k:len(x) for k,x in d['samples'].items()}} for v,d in data.items()}
   write('status.json',{'phase':'running','elapsed_s':round(time.monotonic()-start,2),'expected_finish_epoch':result['expected_finish_epoch'],'progress':progress,'latest_memory':mem[-1]})
   write('memory.json',mem)
   stop.wait(min(30,max(0,end-time.monotonic())))
  for f in futures:f.result()
 result['actual_elapsed_s']=time.monotonic()-start; result['finished_utc']=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime())
 result['upstream']=json.loads(http('http://'+fake_ip+':8081/stats')[1])
 result['variants']={}
 for v,d in data.items():
  stats={k:{'n':len(x),'p50_ms':quantile(x,.5),'p95_ms':quantile(x,.95),'p99_ms':quantile(x,.99)} for k,x in d['samples'].items()}
  stable=[x[v]['VmRSS'] for x in mem if x['elapsed_s']>=A.warmup+A.seconds/2]
  early=[x[v]['VmRSS'] for x in mem if A.warmup<=x['elapsed_s']<A.warmup+A.seconds/4]
  final=[x[v]['VmRSS'] for x in mem if x['elapsed_s']>=A.warmup+3*A.seconds/4]
  if not stable:stable=[x[v]['VmRSS'] for x in mem]
  result['variants'][v]={'requests':d['requests'],'errors':d['errors'],'latency':stats,'late_slots':d['late_slots'],'stable_rss_median':statistics.median(stable),'stable_rss_samples':len(stable),'rss_peak':max(x[v]['VmRSS'] for x in mem),'early_rss_median':statistics.median(early) if early else None,'final_rss_median':statistics.median(final) if final else None}
  write(v+'-latencies.json',d['samples'])
 b=result['variants']['baseline'];c=result['variants']['candidate']
 result['degradation_pct']={k:(c['latency'][k]['p95_ms']/b['latency'][k]['p95_ms']-1)*100 for k in ['stream','nonstream']}
 result['degradation_pct']['stable_rss']=(c['stable_rss_median']/b['stable_rss_median']-1)*100
 result['gates']={'zero_errors':not any(d['errors'] for d in data.values()),'no_upstream_duplicates':result['upstream']['duplicates']==0,'valid_upstream_inputs':result['upstream']['invalid']==0,'upstream_count_exact':result['upstream']['requests']==sum(d['requests'] for d in data.values())+20,'duration_complete':result['actual_elapsed_s']>=A.warmup+A.seconds-1,'sample_count_complete':all(sum(len(x) for x in d['samples'].values())>=A.seconds*A.rps-8 for d in data.values()),'p95_within_10pct':all(result['degradation_pct'][k]<=10 for k in ['stream','nonstream']),'stable_rss_within_20pct':result['degradation_pct']['stable_rss']<=20}
 result['passed']=all(result['gates'].values())
except Exception as e:
 stop.set();result['error']=repr(e)
finally:
 stop.set();cleanup=[]
 for name in reversed(names):
  try:
   info=json.loads(docker('inspect',name).stdout)[0]
   if info['Config']['Labels'].get('sub2api.r1.b8')!=A.run_id:raise RuntimeError('label ownership mismatch')
   if name.endswith('-app'):
    (OUT/(name+'-tail.log')).write_text(docker('logs','--tail','500',name,check=False).stdout,encoding='utf-8')
   p=docker('rm','-f',name,check=False);cleanup.append({'name':name,'removed':p.returncode==0})
  except Exception as e:cleanup.append({'name':name,'error':repr(e)})
 if network_created:
  p=docker('network','rm',NET,check=False);cleanup.append({'name':NET,'removed':p.returncode==0})
 result['cleanup']=cleanup
 result['cleanup_complete']=all(x.get('removed') for x in cleanup)
 result['passed']=result['passed'] and result['cleanup_complete']
 write('result.json',result);write('status.json',{'phase':'completed' if result['passed'] else 'failed','result':result})
 print(json.dumps(result,indent=2),flush=True)
raise SystemExit(0 if result['passed'] else 1)
