import React, { lazy, Suspense, useId, useState } from 'react';
import { AudioLines, ChevronDown, LoaderCircle, Mic, X } from 'lucide-react';

const VoiceConversation = lazy(() => import('./VoiceConversation'));

export default function VoiceAgent({ api, context, onQuestion, available }) {
  const [expanded, setExpanded] = useState(false);
  const panelID = useId();
  return <section className={`voice-agent ${expanded ? 'is-open' : ''}`} aria-label="Kairo voice companion">
    <div className="voice-agent-heading"><span className="voice-agent-icon"><AudioLines size={19}/></span><div><strong>Kairo voice</strong><span>Think through your experiment, together.</span></div><button type="button" className="button secondary" aria-expanded={expanded} aria-controls={panelID} onClick={() => setExpanded((value) => !value)}>{expanded ? <X size={13}/> : <Mic size={13}/>}<span>{expanded ? 'Close voice panel' : 'Talk to Kairo'}</span>{!expanded && <ChevronDown size={12}/>}</button></div>
    {expanded && <div id={panelID} className="voice-agent-body"><Suspense fallback={<p className="voice-agent-loading"><LoaderCircle className="spinning" size={14}/>Loading Kairo voice…</p>}><VoiceConversation api={api} context={context} onQuestion={onQuestion} available={available}/></Suspense></div>}
  </section>;
}
