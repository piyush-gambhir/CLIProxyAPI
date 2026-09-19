import http from 'node:http';
import {spawn} from 'node:child_process';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import assert from 'node:assert/strict';
const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'cliproxy-isolation-'));
let failed = false;
const calls = [];
const upstream = http.createServer((req,res) => {
  let body=''; req.on('data', c=> body+=c); req.on('end',()=>{
    const account = req.headers.authorization;
    calls.push(account);
    if (failed && account==='Bearer account-a') {res.writeHead(500,{'Content-Type':'application/json'});res.end(JSON.stringify({error:{message:'account a unavailable',type:'server_error'}}));return;}
    res.writeHead(200,{'Content-Type':'application/json'});
    res.end(JSON.stringify({id:'test',object:'chat.completion',created:1,model:'test-model',choices:[{index:0,message:{role:'assistant',content:account},finish_reason:'stop'}],usage:{prompt_tokens:1,completion_tokens:1,total_tokens:2}}));
  });
});
await new Promise(r=>upstream.listen(0,'127.0.0.1',r));
const probe = http.createServer();await new Promise(r=>probe.listen(0,'127.0.0.1',r));const port=probe.address().port;await new Promise(r=>probe.close(r));
const config = {host:'127.0.0.1',port,'auth-dir':path.join(dir,'auth'),'api-keys':['test-client'], 'request-retry':0,'max-retry-interval':0,'remote-management':{'secret-key':'test-management','disable-control-panel':true}, 'openai-compatibility':[
 {name:'test-provider', 'base-url':`http://127.0.0.1:${upstream.address().port}/v1`,prefix:'account-a','api-key-entries':[{'api-key':'account-a'}],models:[{name:'test-model',alias:'test-model'}]},
 {name:'test-provider-b', 'base-url':`http://127.0.0.1:${upstream.address().port}/v1`,prefix:'account-b','api-key-entries':[{'api-key':'account-b'}],models:[{name:'test-model',alias:'test-model'}]},
]};
// JSON is a YAML subset accepted by the proxy.
await fs.writeFile(path.join(dir,'config.yaml'),JSON.stringify(config));
const proc=spawn(process.env.CLIPROXY_BIN || 'cliproxyapi',['-config',path.join(dir,'config.yaml')],{cwd:dir,stdio:'ignore'});
try {
 const base=`http://127.0.0.1:${port}`;
 for(let i=0;i<60;i++){try{if((await fetch(base+'/healthz')).ok)break;}catch{} await new Promise(r=>setTimeout(r,100));}
 const request=async model=>fetch(base+'/v1/chat/completions',{method:'POST',headers:{Authorization:'Bearer test-client','Content-Type':'application/json'},body:JSON.stringify({model,messages:[{role:'user',content:'test'}]})});
 const a=await request('account-a/test-model');assert.equal(a.status,200,await a.text());assert.deepEqual(calls,['Bearer account-a']);
 calls.length=0;const b=await request('account-b/test-model');assert.equal(b.status,200,await b.text());assert.deepEqual(calls,['Bearer account-b']);
 calls.length=0;failed=true;const unavailable=await request('account-a/test-model');assert.ok(unavailable.status>=400);await unavailable.text();assert.ok(calls.length>0);assert.ok(calls.every(c=>c==='Bearer account-a'));console.log('PASS: installed proxy selects A/B explicitly; failing A never calls B.');
} finally {proc.kill('SIGTERM');await new Promise(r=>proc.once('exit',r));await new Promise(r=>upstream.close(r));await fs.rm(dir,{recursive:true,force:true});}
