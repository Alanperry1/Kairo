import React, { createContext, useCallback, useContext, useState } from 'react';

const VoiceActivationContext = createContext(null);
const preferenceKey = 'kairo-wake-enabled';
function readPreference() {
  try { return localStorage.getItem(preferenceKey) === 'true'; } catch { return false; }
}
export function VoiceActivationProvider({ children }) {
  const [enabled, updateEnabled] = useState(readPreference);
  const setEnabled = useCallback(value => updateEnabled(current => {
    const next = typeof value === 'function' ? value(current) : value;
    try { localStorage.setItem(preferenceKey, String(next)); } catch { /* Memory opt-in still works. */ }
    return next;
  }), []);
  return <VoiceActivationContext.Provider value={{ enabled, setEnabled }}>{children}</VoiceActivationContext.Provider>;
}

export function useVoiceActivation() {
  const shared = useContext(VoiceActivationContext);
  const [enabled, setEnabled] = useState(false);
  return shared || { enabled, setEnabled };
}
