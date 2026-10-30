import React, { createContext, useContext, useState } from 'react';

const VoiceActivationContext = createContext(null);

// Keep opt-in for this workspace visit, without restarting a microphone on reload.
export function VoiceActivationProvider({ children }) {
  const [enabled, setEnabled] = useState(false);
  return <VoiceActivationContext.Provider value={{ enabled, setEnabled }}>{children}</VoiceActivationContext.Provider>;
}

export function useVoiceActivation() {
  const shared = useContext(VoiceActivationContext);
  const [enabled, setEnabled] = useState(false);
  return shared || { enabled, setEnabled };
}
