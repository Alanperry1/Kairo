// Read authenticated POST/GET server-sent events without buffering the response.
export async function readEventStream(response, onEvent, signal) {
  if (!response.body?.getReader) throw new Error('This browser cannot read streaming responses.');
  const reader = response.body.getReader(); const decoder = new TextDecoder();
  let buffer = ''; let ended = false;
  function frame(raw) {
    let event = 'message'; const data = [];
    for (const line of raw.split('\n')) {
      if (line.startsWith('event:')) event = line.slice(6).trim();
      if (line.startsWith('data:')) data.push(line.slice(5).trimStart());
    }
    if (!data.length) return;
    const value = JSON.parse(data.join('\n')); onEvent({ event, ...value });
    if (['done', 'error', 'end'].includes(event)) ended = true;
  }
  try {
    while (!ended) {
      if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
      const { done, value } = await reader.read();
      buffer += decoder.decode(value, { stream: !done });
      buffer = buffer.replace(/\r\n/g, '\n');
      if (buffer.length > 8 * 1024 * 1024) throw new Error('The stream exceeded its response limit.');
      let boundary;
      while ((boundary = buffer.indexOf('\n\n')) >= 0) { const raw = buffer.slice(0, boundary); buffer = buffer.slice(boundary + 2); frame(raw); if (ended) break; }
      if (done) { if (buffer.trim()) frame(buffer); break; }
    }
  } finally { await reader.cancel().catch(() => {}); reader.releaseLock(); }
  return ended;
}
