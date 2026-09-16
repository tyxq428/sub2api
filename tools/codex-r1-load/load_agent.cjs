#!/usr/bin/env node
'use strict';
const fs = require('fs');
const { performance } = require('perf_hooks');
const OUT = '/out';
const cfg = JSON.parse(fs.readFileSync(OUT + '/agent-config.json', 'utf8'));
const variants = ['baseline','candidate'];
const data = Object.fromEntries(variants.map(v => [v,{requests:0,errors:[],samples:{stream:[],nonstream:[]},late_slots:0}]));
let stop = false;
function atomic(name,obj){ const p=OUT+'/'+name, t=p+'.tmp'; fs.writeFileSync(t,JSON.stringify(obj,null,2)); fs.renameSync(t,p); }
function sleep(ms){ return new Promise(r=>setTimeout(r,Math.max(0,ms))); }
function q(a,p){ const x=[...a].sort((a,b)=>a-b); if(!x.length) throw new Error('empty latency'); return x[Math.min(x.length-1,Math.floor((x.length-1)*p))]; }
async function request(v,worker,n){
  const stream=(n%2)===0, kind=stream?'stream':'nonstream';
  const rid=`${cfg.run_id}-${v}-${worker}-${n}`;
  const body={model:'gpt-5.6-sol',input:'r1:'+rid+'|'+'x'.repeat(4096),stream};
  const before=performance.now();
  const res=await fetch(cfg.apps[v].url+'/v1/responses',{method:'POST',headers:{'content-type':'application/json','authorization':'Bearer '+cfg.apps[v].key,'session-id':'r1-'+v+'-'+worker},body:JSON.stringify(body),signal:AbortSignal.timeout(10000)});
  const raw=await res.text(); const elapsed=performance.now()-before;
  if(res.status!==200) throw new Error('http status '+res.status+' '+raw.slice(0,300));
  if(stream){
    const events=raw.split(/\r?\n/).filter(l=>l.startsWith('data: ')&&l.slice(6)!=='[DONE]').map(l=>JSON.parse(l.slice(6)));
    const complete=events.filter(e=>e.type==='response.completed').map(e=>e.response);
    if(complete.length!==1||complete[0].id!=='resp_'+rid||complete[0].status!=='completed') throw new Error('invalid completed '+rid);
    if(!events.some(e=>e.type==='response.output_text.delta'&&e.delta==='r1-ok')) throw new Error('missing delta '+rid);
  } else {
    const response=JSON.parse(raw);
    if(response.id!=='resp_'+rid||response.status!=='completed'||response.output?.[0]?.content?.[0]?.text!=='r1-ok') throw new Error('invalid response '+rid);
  }
  return {kind,elapsed};
}
async function worker(v,w,start,warmEnd,end){
  const interval=4000/cfg.rps; let next=start+w*1000/cfg.rps, n=0;
  while(next<end&&!stop){
    const delay=next-performance.now(); if(delay>0) await sleep(delay); if(stop) break;
    try{
      const {kind,elapsed}=await request(v,w,n);
      data[v].requests++;
      if(next>=warmEnd) data[v].samples[kind].push(elapsed);
      if(performance.now()-next>interval) data[v].late_slots++;
    }catch(e){ data[v].errors.push(String(e).slice(0,1000)); stop=true; break; }
    n++; next+=interval;
  }
}
async function main(){
  const preflight={};
  for(const v of variants){ preflight[v]=0; for(let n=0;n<10;n++){ await request(v,'smoke',n); preflight[v]++; } }
  const start=performance.now()+1000, warmEnd=start+cfg.warmup*1000, end=warmEnd+cfg.seconds*1000;
  const startedEpoch=Date.now()/1000+1;
  atomic('agent-status.json',{phase:'running',started_epoch:startedEpoch,expected_finish_epoch:startedEpoch+cfg.warmup+cfg.seconds,preflight});
  const timer=setInterval(()=>atomic('agent-status.json',{phase:'running',elapsed_s:Math.max(0,(performance.now()-start)/1000),progress:Object.fromEntries(variants.map(v=>[v,{requests:data[v].requests,errors:data[v].errors.length,measured:Object.fromEntries(Object.entries(data[v].samples).map(([k,x])=>[k,x.length]))}]))}),30000);
  const jobs=[]; for(const v of variants) for(let w=0;w<4;w++) jobs.push(worker(v,w,start,warmEnd,end));
  await Promise.all(jobs); clearInterval(timer);
  const actual=(performance.now()-start)/1000;
  const upstreamRes=await fetch(cfg.fake_url+'/stats',{signal:AbortSignal.timeout(5000)}); if(!upstreamRes.ok) throw new Error('fake stats '+upstreamRes.status); const upstream=await upstreamRes.json();
  const result={passed:false,actual_elapsed_s:actual,started_utc:new Date(startedEpoch*1000).toISOString(),finished_utc:new Date().toISOString(),preflight,upstream,variants:{}};
  for(const v of variants){
    atomic(v+'-latencies.json',data[v].samples);
    result.variants[v]={requests:data[v].requests,errors:data[v].errors,latency:Object.fromEntries(Object.entries(data[v].samples).map(([k,x])=>[k,{n:x.length,p50_ms:q(x,.5),p95_ms:q(x,.95),p99_ms:q(x,.99)}])),late_slots:data[v].late_slots};
  }
  result.passed=!stop&&variants.every(v=>data[v].errors.length===0);
  atomic('agent-result.json',result); atomic('agent-status.json',{phase:result.passed?'completed':'failed',result});
  if(!result.passed) process.exitCode=1;
}
main().catch(e=>{ stop=true; const r={passed:false,error:String(e),variants:data}; try{atomic('agent-result.json',r);atomic('agent-status.json',{phase:'failed',result:r});}catch(_){} console.error(e); process.exitCode=1; });
