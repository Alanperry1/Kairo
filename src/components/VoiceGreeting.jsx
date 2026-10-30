import React, { useEffect, useRef, useState } from 'react';
import { Mic, Volume2 } from 'lucide-react';

export const greeting = "Hey there, I'm Kairo, your AI research companion.";
const wave = [8, 14, 22, 12, 30, 44, 25, 16, 36, 53, 34, 21, 14, 26, 43, 62, 40, 25, 16, 28, 45, 32, 18, 12, 23, 38, 25, 14, 20, 11, 6];

export default function VoiceGreeting() {
  const [speaking, setSpeaking] = useState(false);
  const [error, setError] = useState('');
  const current = useRef(null);

  useEffect(() => () => {
    if (current.current) {
      current.current.onstart = null;
      current.current.onend = null;
      current.current.onerror = null;
      current.current = null;
      window.speechSynthesis?.cancel();
    }
  }, []);

  function playGreeting() {
    setError('');
    const speech = window.speechSynthesis;
    if (!speech || typeof window.SpeechSynthesisUtterance !== 'function') {
      setError('Voice playback is not available in this browser.');
      return;
    }
    if (current.current) {
      current.current = null;
      speech.cancel();
      setSpeaking(false);
      return;
    }
    const utterance = new window.SpeechSynthesisUtterance(greeting);
    utterance.lang = 'en-US';
    utterance.rate = .95;
    utterance.pitch = 1;
    utterance.volume = 1;
    const voices = speech.getVoices?.() || [];
    const voice = voices.find((item) => item.default && item.lang.toLowerCase().startsWith('en')) || voices.find((item) => item.lang.toLowerCase() === 'en-us') || voices.find((item) => item.lang.toLowerCase().startsWith('en'));
    if (voice) utterance.voice = voice;
    const finish = () => {
      if (current.current !== utterance) return;
      current.current = null;
      setSpeaking(false);
    };
    utterance.onstart = () => { if (current.current === utterance) setSpeaking(true); };
    utterance.onend = finish;
    utterance.onerror = (event) => {
      if (current.current !== utterance) return;
      finish();
      if (event.error !== 'canceled' && event.error !== 'interrupted') setError('Voice playback could not start. Please try again.');
    };
    current.current = utterance;
    setSpeaking(true);
    try {
      speech.cancel();
      speech.speak(utterance);
    } catch {
      finish();
      setError('Voice playback could not start. Please try again.');
    }
  }

  return <div className={`lp-voice-visual ${speaking ? 'is-speaking' : ''}`}>
    <div className="lp-voice-wave" aria-hidden="true">{wave.map((height, index) => <span key={index} style={{ height: `${height}px`, '--bar-index': index }}/>)}</div>
    <button className="lp-voice-orb lp-voice-button" type="button" aria-label={speaking ? 'Stop Kairo greeting' : 'Play Kairo greeting'} aria-pressed={speaking} title="Play browser voice demo" onClick={playGreeting}>{speaking ? <Volume2 size={23} strokeWidth={1.3}/> : <Mic size={23} strokeWidth={1.3}/>}</button>
    <p>From a spoken idea<br/>to a question worth testing.</p>
    {error && <p className="lp-voice-error" role="alert">{error}</p>}
  </div>;
}
