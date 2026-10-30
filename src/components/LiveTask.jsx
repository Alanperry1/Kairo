import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Check, ChevronDown, LoaderCircle, Radio } from 'lucide-react';

export function useLiveTask() {
  const [task, setTask] = useState(null);
  const start = useCallback(() => setTask({ phase:'working', events:[], text:'' }), []);
  const event = useCallback((update) => setTask(previous => {
    const current=previous||{phase:'working',events:[],text:''};
    if(update.event==='status')return {...current,events:[...current.events.slice(-14),{message:update.message,at:Date.now()}]};
    if(update.event==='model_start')return {...current,text:''};
    if(update.event==='delta')return {...current,text:(current.text+update.text).slice(0,250000)};
    return current;
  }), []);
  const finish = useCallback(() => setTask(current=>current?{...current,phase:'done'}:null), []);
  const fail = useCallback(() => setTask(current=>current?{...current,phase:'failed'}:null), []);
  const clear = useCallback(() => setTask(null), []);
  return { task, start, event, finish, fail, clear };
}

// Extract only draft string fields from the partial JSON response. These are
// provisional output; a complete validated response still determines success.
export function partialDraftField(text,key) {
  const escaped=key.replace(/[.*+?^${}()|[\]\\]/g,'\\$&');const match=new RegExp(`(?:^|[,{]\\s*)"${escaped}"\\s*:\\s*"`).exec(text);
  if(!match)return '';
  let result='';for(let i=match.index+match[0].length;i<text.length;i++){
    const char=text[i];if(char==='"')break;
    if(char==='\\'){
      i++;if(i>=text.length)break;const escaped=text[i];const simple={n:'\n',r:'\r',t:'\t','"':'"','\\':'\\','/':'/',b:'\b',f:'\f'};
      if(escaped==='u'){const code=text.slice(i+1,i+5);if(!/^[0-9a-f]{4}$/i.test(code))break;result+=String.fromCharCode(parseInt(code,16));i+=4;}else if(simple[escaped]!==undefined)result+=simple[escaped];else break;
    }else result+=char;
  }return result;
}
export function draftPreview(text,kind) {
  if(kind==='chat')return partialDraftField(text,'message');
  if(kind==='plan'){
    const code=partialDraftField(text,'code');if(code)return code;
    return ['hypothesis','methodology','language_reason'].map(key=>partialDraftField(text,key)).filter(Boolean).join('\n\n');
  }
  return ['title','abstract','introduction','background','methods','experimental_setup','results','discussion','conclusion'].map(key=>{const value=partialDraftField(text,key);return value?`${key.replaceAll('_',' ').toUpperCase()}\n${value}`:'';}).filter(Boolean).join('\n\n');
}
export default function LiveTask({ task, kind = 'plan' }) {
  const preview=task?draftPreview(task.text,kind):'';const ref=useRef(null);
  useEffect(()=>{if(ref.current)ref.current.scrollTop=ref.current.scrollHeight;},[preview]);
  if(!task)return null;
  const working=task.phase==='working';
  return <section className={`live-task ${task.phase}`} aria-label="Live agent activity"><div className="live-task-heading"><span>{working?<Radio size={14}/>:<Check size={14}/>} {working?'Kairo is working':task.phase==='done'?'Task completed':'Task interrupted'}</span>{working&&<LoaderCircle className="spinning" size={13}/>}</div><ol aria-live="polite" aria-label="Agent stages">{task.events.map((item,index)=><li key={`${item.at}-${index}`}><span className="live-task-dot"/>{item.message}</li>)}</ol>{preview&&<details open={working}><summary>{kind==='plan'?'Live code & method':kind==='chat'?'Live response':'Live document draft'}<ChevronDown size={12}/></summary><pre ref={ref} className={kind==='plan'&&partialDraftField(task.text,'code')?'live-code':''}>{preview}</pre><small>Draft preview · validated before saving</small></details>}</section>;
}
