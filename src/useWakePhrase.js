import { useEffect, useRef, useState } from 'react';

export function hasWakePhrase(text) {
  const normalized = String(text).toLowerCase().replace(/[^a-z\s]/g, ' ');
  return /\bhey\s+(?:kairo|kario|cairo|kyro)\b/.test(normalized);
}

export function useWakePhrase({ enabled, paused, onWake, onDisable }) {
  const Recognition = window.SpeechRecognition || window.webkitSpeechRecognition;
  const [phase, setPhase] = useState('off');
  const [error, setError] = useState('');
  const [visible, setVisible] = useState(document.visibilityState !== 'hidden');
  const callbacks = useRef({ onWake, onDisable });
  callbacks.current = { onWake, onDisable };

  useEffect(() => {
    const listener = () => setVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', listener);
    return () => document.removeEventListener('visibilitychange', listener);
  }, []);

  useEffect(() => {
    if (!enabled || !Recognition) { setPhase('off'); return; }
    if (paused || !visible) { setPhase('paused'); return; }
    setError(''); setPhase('starting');
    let disposed = false;
    let fired = false;
    let stopped = false;
    let retry;
    let history = '';
    let recognition;
    function fail(code) {
      if (disposed || fired || stopped) return;
      stopped = true;
      clearTimeout(retry);
      setError(code === 'not-allowed' || code === 'service-not-allowed'
        ? 'Allow microphone access to enable Hey Kairo.'
        : code === 'audio-capture' ? 'No microphone is available for Hey Kairo.'
          : 'Wake listening could not start. Try again, or select the microphone.');
      setPhase('off');
      recognition?.abort();
      callbacks.current.onDisable?.();
    }
    function start() {
      if (disposed || stopped || fired) return;
      try { recognition.start(); } catch { fail('start-failed'); }
    }
    try { recognition = new Recognition(); } catch { fail('start-failed'); return; }
    recognition.continuous = true;
    recognition.interimResults = false;
    recognition.lang = 'en-US';
    recognition.onstart = () => { if (!disposed && !fired && !stopped) setPhase('listening'); };
    recognition.onresult = (event) => {
      if (disposed || fired || stopped) return;
      for (let index = event.resultIndex || 0; index < event.results.length; index++) {
        const result = event.results[index];
        if (!result.isFinal) continue;
        const text = `${history} ${result[0]?.transcript || ''}`;
        history = text.slice(-160);
        if (!hasWakePhrase(text)) continue;
        fired = true;
        clearTimeout(retry);
        // Release wake recognition before ElevenLabs takes over the microphone.
        recognition.abort();
        setPhase('paused');
        callbacks.current.onWake();
        return;
      }
    };
    recognition.onerror = (event) => {
      if (event.error === 'no-speech' || event.error === 'aborted') return;
      fail(event.error);
    };
    recognition.onend = () => {
      if (disposed || fired || stopped) return;
      setPhase('starting');
      history = '';
      // Browsers may end a continuous recognition session after silence.
      retry = setTimeout(start, 500);
    };
    start();
    return () => {
      disposed = true;
      clearTimeout(retry);
      recognition.onstart = null; recognition.onresult = null;
      recognition.onerror = null; recognition.onend = null;
      recognition.abort();
    };
  }, [enabled, paused, visible, Recognition]);

  return { supported: !!Recognition, phase, error };
}
