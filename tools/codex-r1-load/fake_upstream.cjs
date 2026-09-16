// Isolated B8 fake upstream. No external dependencies or network clients.
const http = require('node:http');
const seen = new Set();
const stats = {requests:0, stream:0, nonstream:0, duplicates:0, invalid:0};
http.createServer((req,res)=>{
  if(req.method==='GET' && req.url==='/stats') {res.setHeader('Content-Type','application/json');res.end(JSON.stringify(stats));return;}
  if(req.method!=='POST' || !req.url.endsWith('/responses')) {res.writeHead(404);res.end();return;}
  let raw=''; req.on('data',d=>{raw+=d; if(raw.length>65536) req.destroy();});
  req.on('end',()=>{
    let body;try{body=JSON.parse(raw);}catch{stats.invalid++;res.writeHead(400);res.end('invalid json');return;}
    const marker=/^r1:([^|]+)\|/.exec(String(body.input));
    if(!marker || req.headers.authorization!=='Bearer synthetic-upstream-r1-only') {stats.invalid++;res.writeHead(400);res.end('invalid synthetic fixture');return;}
    const rid=marker[1]; stats.requests++;if(seen.has(rid)) stats.duplicates++;seen.add(rid);
    const response={id:'resp_'+rid,object:'response',model:body.model,status:'completed',output:[{id:'msg_'+rid,type:'message',role:'assistant',status:'completed',content:[{type:'output_text',text:'r1-ok',annotations:[]}]}],usage:{input_tokens:1024,output_tokens:2,total_tokens:1026}};
    if(!body.stream){stats.nonstream++;setTimeout(()=>{res.writeHead(200,{'Content-Type':'application/json'});res.end(JSON.stringify(response));},25);return;}
    stats.stream++;res.writeHead(200,{'Content-Type':'text/event-stream','Cache-Control':'no-cache'});
    const events=[{type:'response.created',response:{...response,status:'in_progress',output:[]}},{type:'response.output_text.delta',item_id:'msg_'+rid,output_index:0,content_index:0,delta:'r1-ok'},{type:'response.completed',response}];
    let i=0; const timer=setInterval(()=>{if(res.destroyed){clearInterval(timer);return;}if(i<events.length){const e=events[i++];res.write('event: '+e.type+'\ndata: '+JSON.stringify(e)+'\n\n');}else{clearInterval(timer);res.end('data: [DONE]\n\n');}},10);
  });
}).listen(8081,'0.0.0.0');
