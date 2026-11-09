import {eventReadId,frozenEventRange} from '../api/eventIdentity';
import type {TimelineCaptureLimit} from '../components/TimelineCaptureNotice';
export interface CapturedSelection {version:1;system:string;from:string;to:string;ids:string[];eventReadId?:string;limits:TimelineCaptureLimit[]}
const limits=['NATS and collector events','finding transitions','Kubernetes events'];
const bad=()=>new Error('This captured selection link is invalid or its system/range has changed. Reopen the original captured selection.');
export function validateSelection(s:CapturedSelection){
 if(!s||s.version!==1||typeof s.system!=='string'||!s.system||s.system.length>256||/[\x00-\x1f\x7f]/.test(s.system)||typeof s.from!=='string'||typeof s.to!=='string'||Object.keys(s).some(k=>!['version','system','from','to','ids','eventReadId','limits'].includes(k)))throw bad();
 frozenEventRange({start:s.from,end:s.to});eventReadId(s.eventReadId);
 if(!Array.isArray(s.ids)||s.ids.length<1||s.ids.length>2000||new Set(s.ids).size!==s.ids.length||s.ids.some(id=>typeof id!=='string'||eventReadId(id)===0n))throw bad();
 if(!Array.isArray(s.limits)||s.limits.length>3||new Set(s.limits).size!==s.limits.length||s.limits.some(x=>!limits.includes(x)))throw bad();
}
function page(s:CapturedSelection,offset:number,size:number){if(!Number.isInteger(offset)||offset<0||offset>=s.ids.length||!Number.isInteger(size)||size<1||size>50)throw bad()}
export function selectionPath(s:CapturedSelection,offset=0,pageSize=50):string{
 validateSelection(s);page(s,offset,pageSize);
 // ASVS 1.2.2/2.2.2: data-only bounded fragment, never an API destination or HTML.
 const bytes=new TextEncoder().encode(JSON.stringify(s));if(bytes.length>65536)throw bad();
 const encoded=btoa(String.fromCharCode(...bytes)).replaceAll('+','-').replaceAll('/','_').replace(/=+$/,'');
 const q=new URLSearchParams({system:s.system,from:s.from,to:s.to,offset:String(offset),pageSize:String(pageSize)});if(s.eventReadId!==undefined)q.set('eventReadId',s.eventReadId);
 return `/timeline/events?${q}#selection=${encoded}`;
}
export function selectionFromURL(u:URL):{selection:CapturedSelection;offset:number;pageSize:number}{
 if(u.pathname!=='/timeline/events'||u.hash.length>88000||!/^#selection=[A-Za-z0-9_-]+$/.test(u.hash))throw bad();
 const q=u.searchParams;if([...q.keys()].some(k=>!['system','from','to','eventReadId','offset','pageSize'].includes(k)||q.getAll(k).length!==1))throw bad();
 let s:CapturedSelection;try{const encoded=u.hash.slice(11).replaceAll('-','+').replaceAll('_','/');const bytes=Uint8Array.from(atob(encoded),c=>c.charCodeAt(0));if(bytes.length>65536)throw bad();s=JSON.parse(new TextDecoder('utf-8',{fatal:true}).decode(bytes)) as CapturedSelection;validateSelection(s)}catch{throw bad()}
 if(q.get('system')!==s.system||q.get('from')!==s.from||q.get('to')!==s.to||(q.get('eventReadId')??undefined)!==s.eventReadId)throw bad();
 const rawOffset=q.get('offset')??'0',rawSize=q.get('pageSize')??'50';if(!/^(0|[1-9]\d*)$/.test(rawOffset)||!/^([1-9]\d*)$/.test(rawSize))throw bad();const offset=Number(rawOffset),pageSize=Number(rawSize);page(s,offset,pageSize);return {selection:s,offset,pageSize};
}
