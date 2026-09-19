import http from 'node:http';
import {spawn} from 'node:child_process';
import {once} from 'node:events';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import assert from 'node:assert/strict';
import {createHash} from 'node:crypto';
const dir=await fs.mkdtemp(path.join(os.tmpdir(),'cliproxy-native-'));
const calls=[];let fail=false;
const upstream=http.createServer(async(req,res)=>{
 let raw='';for await(const chunk of req)raw+=chunk;const body=JSON.parse(raw);const account=req.headers['x-api-key'] || req.headers.authorization;
 calls.push({account,body,headers:req.headers});
 if(fail){res.writeHead(429,{'content-type':'application/json','x-should-retry':'false','retry-after':'60','anthropic-ratelimit-unified-status':'rejected'});res.end(JSON.stringify({type:'error',error:{type:'rate_limit_error',message:'test quota exhausted'}}));return;}
 if(req.url?.includes('count_tokens')){res.writeHead(200,{'content-type':'application/json'});res.end('{"input_tokens":210000}');return;}
 const message={id:'msg_test',type:'message',role:'assistant',model:'claude-opus-5',content:[{type:'text',text:'private response'}],stop_reason:'end_turn',usage:{input_tokens:20,cache_read_input_tokens:220000,cache_creation_input_tokens:50,output_tokens:4}};
 if(body.stream){res.writeHead(200,{'content-type':'text/event-stream','x-should-retry':'false'});res.write('event: ping\ndata: {"type":"ping"}\n\n');res.write(`event: message_start\ndata: ${JSON.stringify({type:'message_start',message})}\n\n`);res.end('event: message_stop\ndata: {"type":"message_stop"}\n\n');}
 else {res.writeHead(200,{'content-type':'application/json','x-should-retry':'false'});res.end(JSON.stringify(message));}
});
await new Promise(r=>upstream.listen(0,'127.0.0.1',r));
const probe=http.createServer();await new Promise(r=>probe.listen(0,'127.0.0.1',r));const port=probe.address().port;await new Promise(r=>probe.close(r));
const base=`http://127.0.0.1:${port}`;
const config={host:'127.0.0.1',port,'auth-dir':path.join(dir,'auth'),'api-keys':['test-client'],'request-retry':0,'max-retry-interval':0,'remote-management':{'secret-key':'test-management','disable-control-panel':true},'claude-api-key':['a','b'].map(id=>({'api-key':`test-account-${id}`,'base-url':`http://127.0.0.1:${upstream.address().port}`,prefix:`account-${id}`,models:[{name:'claude-opus-5',alias:'claude-opus-5'}]}))};
const configFile=path.join(dir,'config.yaml');await fs.writeFile(configFile,JSON.stringify(config));let proc;
async function start(){proc=spawn(process.env.CLIPROXY_BIN || 'cliproxyapi',['-config',configFile],{cwd:dir,stdio:'ignore'});for(let i=0;i<100;i++){try{if((await fetch(base+'/healthz')).ok)return;}catch{}if(proc.exitCode!==null)throw new Error('Proxy exited');await new Promise(r=>setTimeout(r,100));}throw new Error('Proxy never became healthy');}
async function stop(){if(proc&&proc.exitCode===null){const stopped=once(proc,'exit');proc.kill('SIGTERM');await stopped;}}
async function mgmt(route,method='GET',body){const response=await fetch(base+'/v0/management/'+route,{method,headers:{Authorization:'Bearer test-management','Content-Type':'application/json'},...(body?{body:JSON.stringify(body)}:{})});const data=await response.json();assert.equal(response.status,200,JSON.stringify(data));return data;}
const request=(profile,body,key='test-client',endpoint='messages')=>fetch(base+`/inference/${profile}/v1/${endpoint}`,{method:'POST',headers:{'x-api-key':key,'content-type':'application/json','anthropic-beta':'context-1m-2025-08-07','anthropic-future-capability':'future','x-claude-code-agent-id':'test-agent'},body:JSON.stringify(body)});
try{
 await start();
 // Configuration credentials are intentionally omitted by auth-files. Their IDs
 // use the upstream synthesizer's stable hash of kind/key/base/proxy/prefix/headers.
 const routes=config['claude-api-key'].map(entry=>({id:entry.prefix,authFile:'claude:apikey:'+createHash('sha256').update(['claude:apikey',entry['api-key'],entry['base-url'],'',entry.prefix,''].join('\0')).digest('hex').slice(0,12),models:[{id:'claude-opus-5',label:'Opus',contextWindow:1000000}]}));
 await mgmt('account-gateway','PUT',{routes});const body={model:'claude-opus-5',max_tokens:10,messages:[{role:'user',content:'private prompt'}],output_config:{effort:'high'}};
 let response=await request('account-a',body);assert.equal(response.status,200,await response.text());assert.equal(response.headers.get('x-should-retry'),'false');assert.match(String(calls.at(-1).account),/test-account-a$/);assert.equal(calls.at(-1).body.model,'claude-opus-5');assert.equal(calls.at(-1).headers['anthropic-future-capability'],'future');assert.equal(calls.at(-1).headers['x-claude-code-agent-id'],'test-agent');
 response=await request('account-b',{...body,stream:true});assert.equal(response.status,200);assert.match(await response.text(),/event: ping/);assert.match(String(calls.at(-1).account),/test-account-b$/);
 response=await request('account-a',body,'test-client','messages/count_tokens');assert.equal(response.status,200,await response.text());
 const prior=calls.length;response=await request('account-a',body,'bad-key');assert.equal(response.status,401);await response.text();assert.equal(calls.length,prior);
 fail=true;response=await request('account-a',body);assert.equal(response.status,429,await response.text());assert.equal(response.headers.get('x-should-retry'),'false');assert.equal(response.headers.get('retry-after'),'60');assert.equal(calls.length,prior+1);assert.match(String(calls.at(-1).account),/test-account-a$/);
 let journal=await mgmt('account-gateway/receipts');assert.equal(journal.receipts.length,4);assert(journal.receipts.every(r=>r.accountConfirmed));assert.equal(journal.receipts[0].over200kObserved,true);assert.equal(journal.receipts[2].over200kObserved,false);assert.equal(journal.receipts[3].completed,false);assert(!JSON.stringify(journal).includes('private'));
 await stop();await start();assert.equal((await mgmt('account-gateway')).routes.length,2);assert.equal((await mgmt('account-gateway/receipts')).receipts.length,4);
 console.log('PASS: native account routes, authenticating client keys, SSE pings, retry headers, metadata-only receipts and restart persistence. All inference used local fake providers.');
}finally{await stop();upstream.closeAllConnections();await new Promise(r=>upstream.close(r));await fs.rm(dir,{recursive:true,force:true});}
