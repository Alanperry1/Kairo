import React, { lazy, Suspense, useId, useState } from 'react';
import { LoaderCircle, Mic, X } from 'lucide-react';
import { useVoiceActivation } from '../voice-activation';
import { useWakePhrase } from '../useWakePhrase';

const VoiceConversation = lazy(() => import('./VoiceConversation'));

export default function VoiceAgent({ api, context, onQuestion, onDocumentCreated, available, showWakeActivation = true }) {
  const [expanded, setExpanded] = useState(false);
  const panelID = useId();
  const [wakeActivation, setWakeActivation] = useState(0);
  const { enabled, setEnabled } = useVoiceActivation();
  const wake = useWakePhrase({
    enabled: enabled && available !== false,
    paused: expanded,
    onWake: () => { setWakeActivation((value) => value + 1); setExpanded(true); },
    onDisable: () => setEnabled(false),
  });
  const wakeStatus = wake.phase === 'listening' ? 'Listening for “Hey Kairo”' : wake.phase === 'starting' ? 'Starting wake listening…' : wake.phase === 'paused' ? 'Wake listening paused' : 'Say “Hey Kairo” to open voice';
  return <section className={`voice-agent voice-agent-compact ${expanded ? 'is-open' : ''}`} aria-label="Kairo voice companion">
    <div className="voice-agent-heading"><button type="button" className="button secondary voice-mic-button" aria-label={expanded ? 'Close voice panel' : 'Open voice panel'} title={expanded ? 'Close voice panel' : 'Open voice panel'} aria-expanded={expanded} aria-controls={panelID} onClick={() => { setWakeActivation(0); setExpanded((value) => !value); }}>{expanded ? <X size={17} aria-hidden="true"/> : <Mic size={17} aria-hidden="true"/>}</button></div>
    {showWakeActivation && expanded && <div className="voice-wake-controls">{wake.supported ? <><button type="button" role="switch" aria-label="Listen for Hey Kairo" aria-checked={enabled} disabled={available === false} onClick={() => setEnabled((value) => !value)}><span className="voice-wake-switch" aria-hidden="true"/><span>Hey Kairo</span></button><span role="status">{wakeStatus}</span></> : <span>Wake phrases aren’t supported in this browser. Select the microphone.</span>}</div>}
    {wake.error && <p className="voice-wake-error" role="alert">{wake.error}</p>}
    {showWakeActivation && enabled && expanded && <p className="voice-wake-note">Your browser listens in this tab and may use its speech service. Listening pauses when the tab is hidden.</p>}
    {expanded && <div id={panelID} className="voice-agent-body"><Suspense fallback={<p className="voice-agent-loading"><LoaderCircle className="spinning" size={14}/>Loading Kairo voice…</p>}><VoiceConversation api={api} context={context} onQuestion={onQuestion} onDocumentCreated={onDocumentCreated} available={available} autoStart={wakeActivation > 0}/></Suspense></div>}
  </section>;
}
