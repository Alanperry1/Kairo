import React, { useCallback, useEffect, useRef, useState } from 'react';
import { ConversationProvider, useConversationClientTool, useConversationControls, useConversationInput, useConversationMode, useConversationStatus } from '@elevenlabs/react';
import { ArrowUp, Check, LoaderCircle, Mic, MicOff, Square } from 'lucide-react';

function VoiceSession({ api, context, onQuestion, available, messages, error, setError, resetMessages, autoStart = false }) {
  const { startSession, endSession, sendContextualUpdate, sendUserMessage } = useConversationControls();
  const { status } = useConversationStatus();
  const { isSpeaking } = useConversationMode();
  const { isMuted, setMuted } = useConversationInput();
  const [preparing, setPreparing] = useState(false);
  const [message, setMessage] = useState('');
  const [proposal, setProposal] = useState('');
  const generation = useRef(0);
  const pending = useRef(null);
  const transcript = useRef(null);
  const startRef = useRef(null);
  const contextText = JSON.stringify(context || { view: 'experiments' });
  const connected = status === 'connected';
  const busy = preparing || status === 'connecting';

  useConversationClientTool('get_experiment_context', () => contextText);
  useConversationClientTool('set_research_question', ({ question }) => {
    if (typeof question !== 'string' || question.trim().length < 5 || question.trim().length > 4000) return JSON.stringify({ error: 'The question must be 5–4000 characters.' });
    if (!onQuestion) return JSON.stringify({ error: 'This view does not accept a new question.' });
    setProposal(question.trim());
    return JSON.stringify({ draft_ready: true, requires_user_review: true, message: 'The question is displayed for review. The user must click Use question to apply it; nothing has been saved or run.' });
  });

  useEffect(() => () => {
    generation.current++;
    pending.current?.abort();
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
    setPreparing(true); setError(''); setProposal(''); resetMessages();
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
    {proposal && <div className="voice-agent-proposal"><span>YOUR RESEARCH QUESTION</span><p>{proposal}</p><button className="button secondary" type="button" onClick={() => { onQuestion(proposal); setProposal(''); }}><Check size={13}/>Use question</button></div>}
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
