import { readEventStream } from './task-stream.js';
export class APIError extends Error {
  constructor(message, status = 0, code = 'network_error') {
    super(message);
    this.name = 'APIError';
    this.status = status;
    this.code = code;
  }
}

const pathID = (id) => encodeURIComponent(id);

export function createAPI({ baseURL = '/api/v1', getToken = () => '', fetcher = (...args) => fetch(...args) } = {}) {
  const base = baseURL.replace(/\/$/, '');
  async function request(path, { method = 'GET', body, signal, binary = false } = {}) {
    const headers = { Accept: binary ? '*/*' : 'application/json' };
    const token = getToken();
    if (token) headers.Authorization = `Bearer ${token}`;
    const isForm = typeof FormData !== 'undefined' && body instanceof FormData;
    if (body !== undefined && !isForm) headers['Content-Type'] = 'application/json';
    let response;
    try {
      response = await fetcher(`${base}${path}`, {
        method, headers, signal,
        ...(body !== undefined ? { body: isForm ? body : JSON.stringify(body) } : {}),
      });
    } catch (error) {
      if (error.name === 'AbortError') throw error;
      throw new APIError('Cannot reach the research API. Check that the Go backend is running and the API proxy is configured.');
    }
    if (!response.ok) {
      let details;
      try { details = await response.json(); } catch { /* proxy failures may not have a JSON body */ }
      throw new APIError(details?.error?.message || `The API returned HTTP ${response.status}.`, response.status, details?.error?.code || 'request_failed');
    }
    if (binary) return { blob: await response.blob(), contentDisposition: response.headers.get('content-disposition') };
    if (response.status === 204) return null;
    try { return await response.json(); }
    catch { throw new APIError('The API returned an unexpected response. Check the API base URL and Vite proxy.', response.status, 'invalid_response'); }
  }
  async function streaming(path, { body, signal, onProgress, method = 'POST', eventsOnly = false } = {}) {
    const headers = {Accept:'text/event-stream'};const token=getToken();if(token)headers.Authorization=`Bearer ${token}`;if(body!==undefined)headers['Content-Type']='application/json';
    let response;
    try {response=await fetcher(`${base}${path}`,{method,headers,signal,...(body!==undefined?{body:JSON.stringify(body)}:{})});}
    catch(error){if(error.name==='AbortError')throw error;throw new APIError('Cannot reach the streaming API. Check the backend connection.');}
    if(!response.ok){let details;try{details=await response.json();}catch{}throw new APIError(details?.error?.message||`The API returned HTTP ${response.status}.`,response.status,details?.error?.code||'request_failed');}
    if(!response.headers.get('content-type')?.includes('text/event-stream'))throw new APIError('The API did not return an event stream.',502,'invalid_stream');
    let result,completed=false;
    await readEventStream(response,event=>{if(event.event==='error')throw new APIError(event.error?.message||'The task failed.',event.status||500,event.error?.code||'task_failed');if(event.event==='done'){result=event.result;completed=true;}if(event.event==='end')completed=true;onProgress?.(event);},signal);
    if(!completed)throw new APIError('The stream disconnected before the task completed. Refresh to check its saved state.',502,'stream_interrupted');
    return eventsOnly?undefined:result;
  }
  const item = (id) => `/experiments/${pathID(id)}`;
  return {
    baseURL: base,
    listExperiments: (filters = {}, signal) => {
      const query = new URLSearchParams();
      Object.entries(filters).forEach(([key, value]) => { if (value !== '' && value !== undefined) query.set(key, String(value)); });
      return request(`/experiments?${query}`, { signal });
    },
    getExperiment: (id, signal) => request(item(id), { signal }),
    createExperiment: (body) => request('/experiments', { method: 'POST', body }),
    updateExperiment: (id, body) => request(item(id), { method: 'PATCH', body }),
    deleteExperiment: (id) => request(item(id), { method: 'DELETE' }),
    generatePlanStream: (id, onProgress, signal) => streaming(`${item(id)}/plan/stream`, { onProgress, signal }),
    watchExperiment: (id, onProgress, signal) => streaming(`${item(id)}/events/stream`, { method:'GET', onProgress, signal, eventsOnly:true }),
    generatePlan: (id) => request(`${item(id)}/plan`, { method: 'POST' }),
    savePlan: (id, plan) => request(`${item(id)}/plan`, { method: 'PUT', body: plan }),
    runExperiment: (id) => request(`${item(id)}/run`, { method: 'POST' }),
    cancelExperiment: (id) => request(`${item(id)}/cancel`, { method: 'POST' }),
    events: (id, signal) => request(`${item(id)}/events`, { signal }),
    plans: (id, signal) => request(`${item(id)}/plans`, { signal }),
    artifacts: (id, signal) => request(`${item(id)}/artifacts`, { signal }),
    dashboard: (signal) => request('/dashboard', { signal }),
    startVoiceSession: (signal) => request('/voice/session', { method: 'POST', signal }),
    settings: (signal) => request('/settings', { signal }),
    projects: (signal) => request('/projects', { signal }),
    createProject: (body) => request('/projects', { method: 'POST', body }),
    datasets: (signal) => request('/datasets', { signal }),
    chatStream: (body, onProgress, signal) => streaming('/assistant/chat/stream', { body, onProgress, signal }),
    chat: (body, signal) => request('/assistant/chat', { method: 'POST', body, signal }),
    languages: (signal) => request('/languages', { signal }),
    tools: (signal) => request('/tools', { signal }),
    activity: (signal) => request('/activity', { signal }),
    documents: (filters = {}, signal) => request(`/documents?${new URLSearchParams(Object.entries(filters).filter(([, value]) => value !== '' && value !== undefined))}`, { signal }),
    getDocument: (id, signal) => request(`/documents/${pathID(id)}`, { signal }),
    generateDocumentStream: (id, kind, onProgress, signal, options = {}) => streaming(`${item(id)}/documents/stream`, { body:{kind,...options}, onProgress, signal }),
    generateDocument: (id, kind, signal, options = {}) => request(`${item(id)}/documents`, { method: 'POST', body: { kind, ...options }, signal }),
    generateDocumentPDF: (id,onProgress,signal) => streaming(`/documents/${pathID(id)}/pdf/stream`,{onProgress,signal}),
    downloadDocumentPDF: (id) => request(`/documents/${pathID(id)}/pdf`,{binary:true}),
    downloadDocument: (id) => request(`/documents/${pathID(id)}/download`, { binary: true }),
    downloadDocumentFigure: (id, name, signal) => request(`/documents/${pathID(id)}/figures/${name.split('/').map(pathID).join('/')}`, { binary: true, signal }),
    downloadDocumentBundle: (id) => request(`/documents/${pathID(id)}/bundle`, { binary: true }),
    reports: (signal) => request('/reports', { signal }),
    searchDatasets: (query, signal) => request(`/datasets/search?q=${encodeURIComponent(query)}`, { signal }),
    importDataset: (body, signal) => request('/datasets/import', { method: 'POST', body, signal }),
    uploadDataset: (file) => {
      if (file.size > 5_000_000_000) return Promise.reject(new APIError('CSV files must be at most 5 GB.', 413, 'dataset_too_large'));
      const form = new FormData();
      form.append('file', file);
      return request('/datasets', { method: 'POST', body: form });
    },
    downloadDataset: (id) => request(`/datasets/${pathID(id)}/download`, { binary: true }),
    downloadBundle: (id) => request(`${item(id)}/bundle`, { binary: true }),
    downloadReport: (id) => request(`${item(id)}/report`, { binary: true }),
    downloadArtifact: (id, name) => request(`${item(id)}/artifacts/${name.split('/').map(encodeURIComponent).join('/')}`, { binary: true }),
  };
}

export function saveDownload({ blob }, filename) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

export function readSessionToken() {
  try { return sessionStorage.getItem('researchos-api-token') || ''; }
  catch { return ''; }
}

export function storeSessionToken(token) {
  try {
    if (token) sessionStorage.setItem('researchos-api-token', token);
    else sessionStorage.removeItem('researchos-api-token');
  } catch { /* the in-memory token still works when browser storage is disabled */ }
}
