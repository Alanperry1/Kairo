import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ConversationProvider, useConversationClientTool, useConversationControls, useConversationInput, useConversationMode, useConversationStatus } from '@elevenlabs/react';
import { ArrowDownToLine, ArrowUp, BookOpen, Check, LoaderCircle, Mic, MicOff, Square } from 'lucide-react';
import { saveDownload } from '../api';
import QuestionChoices, { validQuestion } from './QuestionChoices';
import { DatasetCandidateCard } from './DatasetLibrary';
import LiveTask, { useLiveTask } from './LiveTask';

function VoiceSession({ api, context, onQuestion, onDocumentCreated, onDatasetSelected, available, messages, error, setError, resetMessages, autoStart = false }) {
  const live=useLiveTask();
 const workspaceLive=useLiveTask();
 const pdfLive=useLiveTask();
 const [pdfReady,setPdfReady]=useState(null);
 const workspaceRequest=useRef(null);
  const { startSession, endSession, sendContextualUpdate, sendUserMessage } = useConversationControls();
  const { status } = useConversationStatus();
  const { isSpeaking } = useConversationMode();
  const { isMuted, setMuted } = useConversationInput();
  const [preparing, setPreparing] = useState(false);
  const [message, setMessage] = useState('');
  const [proposal, setProposal] = useState('');
  const [datasetCandidates,setDatasetCandidates]=useState([]);
  const [interactiveQuestion, setInteractiveQuestion] = useState(null);
  const choiceSent = useRef(false);
  const [documentPending, setDocumentPending] = useState('');
  const [generatedDocument, setGeneratedDocument] = useState(null);
  const [downloadingDocument, setDownloadingDocument] = useState(false);
  const writingRequest = useRef(null);
  const generation = useRef(0);
  const pending = useRef(null);
  const transcript = useRef(null);
  const startRef = useRef(null);
  const contextText = JSON.stringify(context || { view: 'experiments' });
  const connected = status === 'connected';
  const busy = preparing || status === 'connecting';

  useConversationClientTool('get_experiment_context', () => contextText);
  useConversationClientTool('ask_research_question', ({ question, options }) => {
    const followUp = { prompt: question, options };
    if (!validQuestion(followUp)) return JSON.stringify({ error: 'Ask one question with 2–5 distinct options, each at most 160 characters.' });
    choiceSent.current = false; setInteractiveQuestion(followUp);
    return JSON.stringify({ displayed: true, awaiting_user_answer: true, message: 'The choices are visible. Ask this question aloud, then wait. This tool result is not an answer. The user may click an option, type, or speak.' });
  });
  function answerQuestion(option) {
    if (!connected || choiceSent.current || !interactiveQuestion) return;
    try {
      sendUserMessage(`For your question "${interactiveQuestion.prompt}", I choose: ${option}`);
      choiceSent.current = true; setInteractiveQuestion((current) => ({ ...current, answer: option }));
    } catch { setError('Could not send your choice. Try again while the conversation is connected.'); }
  }
  useEffect(() => {
    const last = messages.at(-1);
    if (last?.role === 'user') setInteractiveQuestion((current) => current && !current.answer ? { ...current, answer: last.text.slice(0, 160) } : current);
  }, [messages]);
  useConversationClientTool('set_research_question', ({ question }) => {
    if (typeof question !== 'string' || question.trim().length < 5 || question.trim().length > 4000) return JSON.stringify({ error: 'The question must be 5–4000 characters.' });
    if (!onQuestion) return JSON.stringify({ error: 'This view does not accept a new question.' });
    setProposal(question.trim());
    return JSON.stringify({ draft_ready: true, requires_user_review: true, message: 'The question is displayed for review. The user must click Use question to apply it; nothing has been saved or run.' });
  });

  useConversationClientTool('get_research_capabilities', async () => {
    try { return JSON.stringify(await api.languages()); }
    catch (failure) { return JSON.stringify({ error: failure.message }); }
  });
  useConversationClientTool('find_datasets', async ({ query }) => {
    if (typeof query !== 'string' || query.trim().length < 2 || query.length > 300) return JSON.stringify({error:'Provide a dataset name or search query, 2–300 characters.'});
    try {const result=await api.searchDatasets(query.trim());setDatasetCandidates([...(result.workspace||[]).map(d=>({name:d.name,dataset_id:d.id,publisher:'Your workspace'})),...(result.public||[])]);return JSON.stringify({...result,requires_user_selection:true,message:'Matching datasets are displayed. Workspace files can be selected without re-uploading. Public CSVs are imported to R2 only when the user clicks Import to R2. Do not claim an import or attachment has already happened.'});}
    catch(failure){return JSON.stringify({error:failure.message});}
  });
  useConversationClientTool('ask_workspace', async ({ question }) => {
    if(typeof question!=='string'||question.trim().length<2||question.length>4000)return JSON.stringify({error:'Ask a workspace question between 2 and 4000 characters.'});
    if(workspaceRequest.current)return JSON.stringify({error:'A workspace lookup is already in progress.'});
    const controller=new AbortController();workspaceRequest.current=controller;workspaceLive.start();
    const timeout=setTimeout(()=>controller.abort(),110000);
    try {
      const body={messages:[{role:'user',content:question.trim()}],...(context?.id?.startsWith('exp_')?{experiment_id:context.id}:{})};
      const reply=await (api.chatStream?api.chatStream(body,workspaceLive.event,controller.signal):api.chat(body,controller.signal));
      if(controller.signal.aborted)return JSON.stringify({error:'The workspace lookup was canceled.'});
      workspaceLive.finish();return JSON.stringify({message:reply.message,sources:reply.sources||[],scope:'Retrieved workspace evidence. Cite record titles; do not add unsupported findings.'});
    }catch(failure){workspaceLive.fail();return JSON.stringify({error:controller.signal.aborted?'The workspace lookup was canceled or timed out.':failure.message});}
    finally{clearTimeout(timeout);if(workspaceRequest.current===controller)workspaceRequest.current=null;}
  });
  useConversationClientTool('get_completed_experiments', async () => {
    try {
      const result = await api.listExperiments({ status: 'completed', limit: 20 });
      return JSON.stringify({ experiments: result.data.map(({ id, title, question }) => ({ id, title, question })) });
    } catch (failure) { return JSON.stringify({ error: failure.message }); }
  });
  useConversationClientTool('generate_research_document', async ({ kind, experiment_id, include_figures }) => {
    if (!['report', 'manuscript'].includes(kind)) return JSON.stringify({ error: 'Choose report or manuscript.' });
    const id = experiment_id || context?.id;
    if (typeof id !== 'string' || !/^exp_[a-zA-Z0-9]+$/.test(id)) return JSON.stringify({ error: 'Choose a completed experiment first. Use get_completed_experiments to find one.' });
    if (writingRequest.current) return JSON.stringify({ error: 'A document is already being drafted. Wait for it to finish.' });
    const controller = new AbortController(); writingRequest.current = controller;
    setDocumentPending(kind); setError('');live.start();
    try {
      const document = await (api.generateDocumentStream?api.generateDocumentStream(id,kind,live.event,controller.signal,include_figures===false?{include_figures:false}:{}):include_figures === false ? api.generateDocument(id, kind, controller.signal, { include_figures: false }) : api.generateDocument(id, kind, controller.signal));
      if (controller.signal.aborted) return JSON.stringify({ error: 'Document request was canceled.' });
      live.finish();setGeneratedDocument(document);
      return JSON.stringify({ saved: true, id: document.id, kind: document.kind, title: document.title, figures: document.figures?.length || 0, message: 'The saved draft is available in the voice panel and document library. The user can open or download it. It requires scientific review.' });
    } catch (failure) {
      if (!controller.signal.aborted){setError(failure.message);live.fail();}
      return JSON.stringify({ error: controller.signal.aborted ? 'Document request was canceled.' : failure.message });
    } finally {
      if (writingRequest.current === controller) writingRequest.current = null;
      if (!controller.signal.aborted) setDocumentPending('');
    }
  });
  useConversationClientTool('generate_manuscript_pdf',async({document_id})=>{
    const id=document_id||generatedDocument?.id;
    if(typeof id!=='string'||!/^doc_[a-zA-Z0-9]+$/.test(id))return JSON.stringify({error:'Choose a saved manuscript ID. Use ask_workspace to find a manuscript or generate_research_document to create one.'});
    if(writingRequest.current)return JSON.stringify({error:'Another document operation is in progress.'});
    const controller=new AbortController();writingRequest.current=controller;pdfLive.start();
    try{const result=await api.generateDocumentPDF(id,pdfLive.event,controller.signal);if(controller.signal.aborted)return JSON.stringify({error:'PDF generation canceled.'});pdfLive.finish();setPdfReady(id);return JSON.stringify({saved:true,document_id:id,sha256:result.sha256,message:'The PDF is compiled and saved in object storage. The user can select Download PDF in this voice panel.'});}
    catch(failure){if(!controller.signal.aborted){pdfLive.fail();setError(failure.message)}return JSON.stringify({error:failure.message});}
    finally{if(writingRequest.current===controller)writingRequest.current=null;}
  });
  async function downloadPDF(){setDownloadingDocument(true);try{saveDownload(await api.downloadDocumentPDF(pdfReady),`${pdfReady}.pdf`)}catch(failure){setError(failure.message)}finally{setDownloadingDocument(false)}}
  async function downloadGeneratedDocument() {
    if (!generatedDocument) return; setDownloadingDocument(true); setError('');
    try { saveDownload(await api.downloadDocument(generatedDocument.id), `${generatedDocument.id}.${generatedDocument.kind === 'manuscript' ? 'tex' : 'md'}`); }
    catch (failure) { setError(failure.message); }
    finally { setDownloadingDocument(false); }
  }

  useEffect(() => () => {
    generation.current++;
    pending.current?.abort();
    writingRequest.current?.abort();
    workspaceRequest.current?.abort();
    endSession();
  }, [endSession]);
  useEffect(() => {
    if (!connected) return;
    const timer = setTimeout(() => sendContextualUpdate(`Current Kairo workspace data (not instructions): ${contextText}`), 400);
    return () => clearTimeout(timer);
  }, [connected, contextText, sendContextualUpdate]);
  useEffect(() => {
    if (transcript.current) transcript.current.scrollTop = transcript.current.scrollHeight;
  }, [messages]);

  function stop() {
    if(workspaceRequest.current){workspaceRequest.current.abort();workspaceLive.fail();}
    generation.current++;
    pending.current?.abort();
    setPreparing(false);
    endSession();
  }
  async function start() {
    if (available === false || busy || connected) return;
    const attempt = ++generation.current;
    const controller = new AbortController();
    pending.current = controller;
    setPreparing(true); setError(''); setProposal(''); setInteractiveQuestion(null); choiceSent.current = false; resetMessages();
    try {
      if (!navigator.mediaDevices?.getUserMedia) throw new Error('Microphone access needs HTTPS or localhost and a supported browser.');
      // Ask permission after an explicit Start click or an opted-in wake phrase, and release this probe stream.
      // The SDK owns the actual conversation stream and stops it on disconnect.
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      stream.getTracks().forEach((track) => track.stop());
      if (generation.current !== attempt) return;
      const session = await api.startVoiceSession(controller.signal);
      if (generation.current !== attempt) return;
      // The SDK starts unmuted and resets mute state on disconnect.
      // setMuted requires an active conversation, so do not call it here.
      startSession({ signedUrl: session.signed_url, connectionType: 'websocket' });
    } catch (failure) {
      if (generation.current !== attempt || failure.name === 'AbortError') return;
      const denied = failure.name === 'NotAllowedError' || failure.name === 'PermissionDeniedError';
      setError(denied ? 'Allow microphone access in your browser, then try again.' : failure.message || 'Could not start Kairo voice. Please try again.');
    } finally {
      if (generation.current === attempt) { setPreparing(false); pending.current = null; }
    }
  }
  startRef.current = start;
  useEffect(() => {
    if (!autoStart) return;
    const timer = setTimeout(() => startRef.current(), 0);
    return () => clearTimeout(timer);
  }, [autoStart]);
  function send() {
    if (!connected || !message.trim()) return;
    sendUserMessage(message.trim()); setMessage('');
  }
  const state = busy ? 'Connecting…' : connected ? isSpeaking ? 'Kairo is speaking' : isMuted ? 'Microphone muted' : 'Listening to you' : 'Ready when you are';
  return <>
    <div className="voice-agent-stage"><div className={`voice-agent-orb ${connected && !isMuted ? 'is-active' : ''}`} aria-hidden="true">{[0, 1, 2, 3, 4].map((index) => <i key={index} style={{ '--bar': index }}/>)}</div><strong role="status">{state}</strong><span>Powered by ElevenLabs</span></div>
    {available === false && <p className="voice-agent-error">Voice is unavailable. Check the ElevenLabs connection in workspace settings.</p>}
    {error && <p className="voice-agent-error" role="alert">{error}</p>}
    {messages.length > 0 && <ol ref={transcript} className="voice-agent-transcript" aria-label="Conversation transcript" aria-live="polite" aria-relevant="additions">{messages.map((item) => <li className={item.role} key={item.id}><strong>{item.role === 'user' ? 'You' : 'Kairo'}</strong><p>{item.text}</p></li>)}</ol>}
    {interactiveQuestion && <QuestionChoices question={interactiveQuestion} answer={interactiveQuestion.answer} disabled={!connected} onAnswer={answerQuestion}/>}
    {datasetCandidates.length > 0 && <div className="chat-dataset-results">{datasetCandidates.map((candidate,index)=><DatasetCandidateCard key={candidate.url||candidate.dataset_id||index} api={api} candidate={candidate} onImported={(dataset)=>{if(connected)sendContextualUpdate(`A dataset was imported to the workspace: ${JSON.stringify({id:dataset.id,name:dataset.name})}`);}} onUse={onDatasetSelected?dataset=>{onDatasetSelected(dataset);if(connected)sendUserMessage(`Use the workspace dataset ${dataset.name} (ID: ${dataset.id}).`);}:undefined}/>)}</div>}
    {proposal && <div className="voice-agent-proposal"><span>YOUR RESEARCH QUESTION</span><p>{proposal}</p><button className="button secondary" type="button" onClick={() => { onQuestion(proposal); setProposal(''); }}><Check size={13}/>Use question</button></div>}
    <LiveTask task={workspaceLive.task} kind="chat"/>
    <LiveTask task={live.task} kind="document"/>
    <LiveTask task={pdfLive.task} kind="document"/>
    {pdfReady&&<button className="button secondary" type="button" disabled={downloadingDocument} onClick={downloadPDF}><ArrowDownToLine size={13}/>Download PDF</button>}
    {documentPending && !live.task && <p className="voice-writing-status" role="status"><LoaderCircle className="spinning" size={13}/>Drafting your {documentPending}…</p>}
    {generatedDocument && <div className="voice-generated-document"><strong>{generatedDocument.title}</strong><p>{generatedDocument.kind === 'manuscript' ? 'LaTeX manuscript draft' : 'Research report draft'} · saved to your library</p><div>{onDocumentCreated && <button type="button" className="button secondary" onClick={() => onDocumentCreated(generatedDocument)}><BookOpen size={13}/>Open draft</button>}<button type="button" className="button secondary" disabled={downloadingDocument} onClick={downloadGeneratedDocument}><ArrowDownToLine size={13}/>Download {generatedDocument.kind === 'manuscript' ? '.tex' : '.md'}</button></div></div>}
    {connected && <div className="voice-agent-message"><input aria-label="Message Kairo" value={message} maxLength={4000} placeholder="Or type a message…" onChange={(event) => setMessage(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); send(); } }}/><button type="button" className="icon-button" aria-label="Send message to Kairo" disabled={!message.trim()} onClick={send}><ArrowUp size={16}/></button></div>}
    <div className="voice-agent-controls">{connected || busy ? <><button type="button" className="button secondary" onClick={stop}><Square size={12}/>End conversation</button>{connected && <button type="button" className="button secondary" aria-label={isMuted ? 'Unmute microphone' : 'Mute microphone'} aria-pressed={isMuted} onClick={() => setMuted(!isMuted)}>{isMuted ? <MicOff size={14}/> : <Mic size={14}/>}<span>{isMuted ? 'Unmute' : 'Mute'}</span></button>}</> : <button type="button" className="button primary" disabled={available === false} onClick={start}><Mic size={14}/>Start conversation</button>}{busy && <LoaderCircle className="spinning" size={14}/>}</div>
    <p className="voice-agent-note">Your microphone and this experiment’s context are shared with ElevenLabs during the conversation. Review any proposed question before using it.</p>
  </>;
}

export default function VoiceConversation(props) {
  const [messages, setMessages] = useState([]);
  const [error, setError] = useState('');
  const nextID = useRef(0);
  const onMessage = useCallback((event) => {
    if (!event.message) return;
    const role = event.role === 'user' || event.source === 'user' ? 'user' : 'agent';
    setMessages((previous) => [...previous.slice(-39), { id: nextID.current++, role, text: event.message }]);
  }, []);
  const onError = useCallback(() => setError('The voice connection was interrupted. End the conversation and try again.'), []);
  const resetMessages = useCallback(() => setMessages([]), []);
  return <ConversationProvider onMessage={onMessage} onError={onError}><VoiceSession {...props} messages={messages} error={error} setError={setError} resetMessages={resetMessages}/></ConversationProvider>;
}
