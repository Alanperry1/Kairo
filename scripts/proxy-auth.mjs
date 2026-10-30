// Runs only in Vite's Node process. Never import this module into browser code.
export function authorizeProxyRequest(outgoing, incoming, token) {
  const origin = incoming.headers.origin;
  const site = incoming.headers['sec-fetch-site'];
  if (site && site !== 'same-origin' && site !== 'none') return;
  if (origin) {
    try {
      const source = new URL(origin);
      const protocol = incoming.socket.encrypted ? 'https:' : 'http:';
      if (source.host !== incoming.headers.host || source.protocol !== protocol) return;
      outgoing.removeHeader('origin');
    } catch { return; }
  }
  if (token) outgoing.setHeader('authorization', `Bearer ${token}`);
}
