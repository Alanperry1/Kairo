import React, { useEffect, useRef, useState } from 'react';
import { ArrowUp, ArrowUpRight, LoaderCircle, Orbit, Plus, RotateCcw } from 'lucide-react';
import QuestionChoices from './QuestionChoices';
import VoiceAgent from './VoiceAgent';
import LiveTask, { useLiveTask } from './LiveTask';
import { DatasetCandidateCard } from './DatasetLibrary';

export function conversationHistory(messages) {
  const history = []; let size = 0;
  for (const item of [...messages].reverse()) {
    const content = [item.content, item.follow_up && `Follow-up: ${item.follow_up.prompt}\nChoices: ${item.follow_up.options.join(' | ')}`, item.draft && `Proposed study: ${item.draft.question}\nLanguage: ${item.draft.language}`].filter(Boolean).join('\n\n').slice(0, 9000);
    if (size + content.length > 24000 || history.length >= 24) break;
    size += content.length; history.unshift({ role: item.role, content });
  }
  return history;
}

export default function ResearchChat({ api, settings, experiment, active = true, initialQuestion = '', datasets = [], onDatasetsChanged, onUseDraft, onDocumentCreated, onOpenSource }) {
  const live=useLiveTask();
  const [messages, setMessages] = useState([]);
  const [datasetID, setDatasetID] = useState(experiment?.dataset_id || '');
  const [importedDatasets, setImportedDatasets] = useState([]);
  const allDatasets=[...datasets,...importedDatasets.filter(d=>!datasets.some(item=>item.id===d.id))];
  const [input, setInput] = useState(initialQuestion);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [retryMessages, setRetryMessages] = useState(null);
  const request = useRef(null);
  const nextID = useRef(0);
  const bottom = useRef(null);
  const inputRef = useRef(null);
  const unavailable = settings?.model_provider === 'disabled';
  useEffect(() => () => request.current?.abort(), []);
  useEffect(() => { bottom.current?.scrollIntoView?.({ behavior: 'smooth', block: 'nearest' }); }, [messages, pending]);
  async function respond(history) {
    if (request.current) return;
    const controller = new AbortController(); request.current = controller;
    setPending(true); setError(''); setRetryMessages(null);live.start();
    try {
      const body={ messages: conversationHistory(history), ...(experiment?.id ? { experiment_id: experiment.id } : {}), ...(datasetID ? { dataset_id: datasetID } : {}) };
      const reply=await (api.chatStream?api.chatStream(body,live.event,controller.signal):api.chat(body,controller.signal));
      if(!controller.signal.aborted)live.finish();
      if (!controller.signal.aborted) setMessages((current) => [...current, { id: nextID.current++, role: 'assistant', content: reply.message, follow_up: reply.follow_up, draft: reply.draft, datasets: reply.datasets, sources: reply.sources }]);
    } catch (failure) { if (!controller.signal.aborted) { setError(failure.message); setRetryMessages(history);live.fail(); } }
    finally { if (request.current === controller) request.current = null; if (!controller.signal.aborted) setPending(false); }
  }
  function send(text = input) {
    if (request.current || !text.trim() || text.trim().length > 4000 || unavailable) return;
    const history = [...messages, { id: nextID.current++, role: 'user', content: text.trim() }];
    setMessages(history); setInput(''); respond(history);
  }
  function reset() { request.current?.abort(); request.current = null; setMessages([]); setPending(false); setInput(''); setError(''); setRetryMessages(null);live.clear(); inputRef.current?.focus(); }
  const lastAssistant = messages.at(-1)?.role === 'assistant' ? messages.at(-1).id : null;
  return <section className="research-chat" aria-label="Kairo research conversation">
    <div className="research-chat-top"><span><i className="chat-brand"><Orbit size={19}/></i><span><strong>Your research companion</strong><small>{experiment ? `Discussing ${experiment.title || experiment.id}` : 'Think it through, together.'}</small></span></span><div>{active && <VoiceAgent showWakeActivation={false} api={api} context={{ view: 'conversation', id: experiment?.id, question: input || initialQuestion }} available={settings?.voice_enabled} onQuestion={setInput} onDatasetSelected={dataset=>{setImportedDatasets(current=>[...current.filter(d=>d.id!==dataset.id),dataset]);setDatasetID(dataset.id);onDatasetsChanged?.();}} onDocumentCreated={onDocumentCreated}/>}<button className="icon-button" type="button" title="New conversation" aria-label="New conversation" onClick={reset}><Plus size={18}/></button></div></div>
    <div className="chat-thread" role="log" aria-label="Research messages" aria-live="polite" aria-relevant="additions">
      {!messages.length && <div className="chat-welcome"><span className="chat-welcome-icon"><Orbit size={30} strokeWidth={1.3}/></span><h2>What’s on your mind?</h2><p>Bring an idea. We’ll figure out the next step.</p><div className="chat-starters">{['Help me design an experiment', 'Help me choose a research method', 'Explain my experiment’s results'].map((text) => <button type="button" disabled={unavailable || pending} key={text} onClick={() => send(text)}>{text}<ArrowUpRight size={14}/></button>)}</div></div>}
      {messages.map((item, index) => <article key={item.id} className={`chat-message ${item.role}`}><span className="chat-author">{item.role === 'user' ? 'You' : <><Orbit size={14}/>Kairo</>}</span><div className="chat-message-body"><p>{item.content}</p>{item.sources?.length>0&&<div className="chat-sources" aria-label="Workspace sources"><span>Sources consulted</span>{item.sources.map(source=><button type="button" key={source.key} onClick={()=>onOpenSource?.(source)} disabled={!onOpenSource} title={`${source.kind} · ${source.id}`}><span>{source.kind}</span>{source.title}<ArrowUpRight size={12}/></button>)}</div>}{item.follow_up && <QuestionChoices question={item.follow_up} disabled={pending || item.id !== lastAssistant} answer={messages[index + 1]?.role === 'user' ? messages[index + 1].content : undefined} onAnswer={send}/>} {item.datasets?.length > 0 && <div className="chat-dataset-results">{item.datasets.map((candidate,index)=><DatasetCandidateCard key={candidate.url||candidate.dataset_id||index} api={api} candidate={candidate} onImported={(dataset)=>{setImportedDatasets(current=>[...current,dataset]);setDatasetID(dataset.id);onDatasetsChanged?.();}} onUse={(dataset)=>{setDatasetID(dataset.id);setInput(`Use the workspace dataset ${dataset.name} (ID: ${dataset.id}) for my experiment.`);}}/>)}</div>} {item.draft && <div className="chat-research-draft"><span>YOUR EXPERIMENT</span><p>{item.draft.question}</p><div><small>{item.draft.language === 'auto' ? 'Language chosen by the agent' : item.draft.language}</small><button type="button" className="button primary" disabled={pending} onClick={() => onUseDraft({...item.draft,...(datasetID?{dataset_id:datasetID}:{})})}>Review experiment<ArrowUpRight size={14}/></button></div></div>}</div></article>)}
      {live.task && <LiveTask task={live.task} kind="chat"/>}
      {pending && !live.task && <div className="chat-thinking" role="status"><LoaderCircle size={15} className="spinning"/>Kairo is thinking…</div>}
      <div ref={bottom}/>
    </div>
    {error && <div className="inline-error chat-error" role="alert">{error}{retryMessages && <button className="button secondary" type="button" disabled={pending} onClick={() => respond(retryMessages)}><RotateCcw size={13}/>Retry reply</button>}</div>}
    {unavailable && <p className="inline-notice">Connect a research model in settings to chat. You can also use the microphone for ElevenLabs voice.</p>}
    {allDatasets.length>0 && <label className="chat-dataset-picker">Dataset<select aria-label="Dataset for Kairo" className="form-input" value={datasetID} onChange={event=>setDatasetID(event.target.value)}><option value="">Let Kairo help me find data</option>{allDatasets.map(d=><option key={d.id} value={d.id}>{d.name}</option>)}</select></label>}
    <form className="chat-composer" onSubmit={(event) => { event.preventDefault(); send(); }}><textarea ref={inputRef} aria-label="Ask Kairo" maxLength={4000} rows={2} placeholder="Tell Kairo your idea, or answer its question…" value={input} disabled={unavailable} onChange={(event) => setInput(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); send(); } }}/><button type="submit" aria-label="Send to Kairo" disabled={pending || unavailable || !input.trim()}><ArrowUp size={18}/></button></form>
    <p className="chat-footnote">Choose an option or write your own answer. You review every experiment before it runs.</p>
  </section>;
}
