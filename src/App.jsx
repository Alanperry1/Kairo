import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ArrowDownToLine, ArrowUpRight, Atom, ChartNoAxesCombined, ChevronLeft, ChevronRight, CircleCheck, Code2, FileText, FlaskConical, Folder, HelpCircle, LoaderCircle, Orbit, Plus, Search, Settings2, X } from 'lucide-react';
import { createAPI, readSessionToken, saveDownload, storeSessionToken } from './api';
import { useDebounced, useWorkspace } from './hooks';
import { domainLabels, isBusy, labels, shortID, timeAgo } from './templates';
import Dialog from './components/Dialog';
import ExperimentDetails, { Status } from './components/ExperimentDetails';
import ResearchForm from './components/ResearchForm';
import VoiceAgent from './components/VoiceAgent';

const nav = [['Experiments', FlaskConical], ['Reports', FileText], ['Datasets', Folder]];
const domains = { physics: { Icon: Atom, color: 'purple' }, statistics: { Icon: ChartNoAxesCombined, color: 'green' }, computer_science: { Icon: Code2, color: 'blue' } };
const pageSize = 20;

function WorkspaceSearch({ api, onOpen, onClose }) {
  const [query, setQuery] = useState('');
  const [state, setState] = useState({ data: [], loading: true, error: '' });
  const debounced = useDebounced(query);
  useEffect(() => {
    const controller = new AbortController();
    setState((previous) => ({ ...previous, loading: true, error: '' }));
    api.listExperiments({ q: debounced, limit: 50 }, controller.signal).then((result) => {
      if (!controller.signal.aborted) setState({ data: result.data, loading: false, error: '' });
    }).catch((error) => { if (!controller.signal.aborted) setState({ data: [], loading: false, error: error.message }); });
    return () => controller.abort();
  }, [api, debounced]);
  return <Dialog title="Search experiments" onClose={onClose} className="workspace-search-modal"><div className="search-dialog-input"><Search size={18}/><input data-autofocus aria-label="Search workspace experiments" placeholder="Question, title, or experiment ID…" value={query} onChange={(event) => setQuery(event.target.value)}/></div>{state.error && <div className="inline-error" role="alert">{state.error}</div>}<div className="search-results">{state.loading ? <p className="loading-state">Searching…</p> : state.data.length ? state.data.map((run) => <button key={run.id} onClick={() => onOpen(run.id)}><FlaskConical size={16}/><span>{run.title}<small>{shortID(run.id)} · {labels[run.status]}</small></span><ArrowUpRight size={14}/></button>) : <div className="empty-state"><h3>No experiments found</h3><p>Try another question or create a new experiment.</p></div>}</div></Dialog>;
}

function ConnectionSettings({ api, settings, unauthorized, hasToken, onSaveToken, tools }) {
  const [token, setToken] = useState('');
  const capabilities = tools.filter((tool) => tool.category === 'capability' || tool.id === 'python.experiment');
  return <>
    <div className="engine-setting"><span className="domain-icon"><Atom size={18}/></span><div><strong>{settings?.model_provider === 'disabled' ? 'General research workspace' : settings?.model_name || 'Research engine'}</strong><p>{settings?.model_provider === 'disabled' ? 'Your questions, methods, and computational experiments.' : 'Your model is configured in the root .env file.'}</p></div></div>
    <details className="code-disclosure"><summary>Scientific tools</summary><div className="tools-list">{capabilities.map((tool) => <div key={tool.id}><span><strong>{tool.name}</strong><small>{tool.dependencies.join(' · ')}</small></span><span className={tool.available ? 'available' : 'unavailable'} title={tool.unavailable_reason}>{tool.available ? 'Available' : 'Unavailable'}</span></div>)}{!capabilities.length && <p className="form-help">Restart the updated backend to load the general execution capability.</p>}</div></details>
    <div className="voice-settings"><strong>ElevenLabs voice agent</strong><span>{settings?.voice_enabled ? 'Configured · available in experiments' : 'Not configured · add ELEVENLABS_API_KEY and ELEVENLABS_AGENT_ID to the root .env, then restart the backend.'}</span></div>
    <details className="code-disclosure" open={unauthorized || undefined}><summary>Connection & access</summary><div className="settings-grid"><span>API</span><code>{api.baseURL}</code><span>Workers</span><strong>{settings?.workers ?? '—'}</strong><span>Execution</span><strong>{settings?.worker_mode || 'Unavailable'}</strong></div>{unauthorized && <div className="inline-error">Restart the frontend and backend so both read the same API_TOKEN from .env.</div>}<form onSubmit={(event) => { event.preventDefault(); onSaveToken(token.trim()); setToken(''); }}><label className="field-label" htmlFor="api-token">Backend API token</label><input id="api-token" type="password" className="form-input" autoComplete="off" value={token} onChange={(event) => setToken(event.target.value)} placeholder={hasToken ? 'Token saved for this tab' : 'Optional for local use'}/><p className="form-help">The local Vite proxy reads API_TOKEN from .env automatically. This field is only needed for a direct API connection.</p><div className="modal-actions">{hasToken && <button className="button secondary" type="button" onClick={() => onSaveToken('')}>Clear token</button>}<button className="button primary" disabled={!token.trim()} type="submit">Save token & reconnect</button></div></form></details>
  </>;
}

export default function App({ client }) {
  const tokenRef = useRef(readSessionToken());
  const api = useMemo(() => client || createAPI({ baseURL: import.meta.env.VITE_API_BASE_URL || '/api/v1', getToken: () => tokenRef.current }), [client]);
  const [page, setPage] = useState('Experiments');
  const [status, setStatus] = useState('');
  const [query, setQuery] = useState('');
  const [projectID, setProjectID] = useState('');
  const [offset, setOffset] = useState(0);
  const [modal, setModal] = useState(() => {
    const search = new URLSearchParams(window.location.search);
    if (search.get('new') === '1') {
      let question = '';
      try {
        question = (sessionStorage.getItem('kairo-pending-question') || '').slice(0, 4000);
      } catch { /* a blank custom experiment still works without storage */ }
      return { type: 'new', initialQuestion: question };
    }
    return null;
  });
  useEffect(() => {
    if (new URLSearchParams(window.location.search).get('new') === '1') {
      try { sessionStorage.removeItem('kairo-pending-question'); } catch { /* storage is optional */ }
    }
  }, []);
  const [searchOpen, setSearchOpen] = useState(false);
  const [toast, setToast] = useState('');
  const [actionError, setActionError] = useState('');
  const [revision, setRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [downloadPending, setDownloadPending] = useState('');
  const [hasToken, setHasToken] = useState(!!tokenRef.current);
  const toastTimer = useRef();
  const debouncedQuery = useDebounced(query);
  const workspace = useWorkspace(api, { q: debouncedQuery, status, project_id: projectID, limit: pageSize, offset }, revision);
  const { experiments: runs, dashboard, settings, projects, datasets, tools, reports } = workspace;
  useEffect(() => {
    if (workspace.connected && offset > 0 && offset >= workspace.total) setOffset(Math.max(0, Math.floor((workspace.total - 1) / pageSize) * pageSize));
  }, [workspace.connected, workspace.total, offset]);
  const refresh = useCallback(() => setRevision((value) => value + 1), []);
  const closeModal = useCallback(() => setModal(null), []);
  const closeSearch = useCallback(() => setSearchOpen(false), []);
  const notify = useCallback((message) => { clearTimeout(toastTimer.current); setToast(message); toastTimer.current = setTimeout(() => setToast(''), 4000); }, []);
  useEffect(() => () => clearTimeout(toastTimer.current), []);
  useEffect(() => { const onKey = (event) => { if ((event.metaKey || event.ctrlKey) && event.key === 'k') { event.preventDefault(); setSearchOpen((value) => !value); setModal(null); } }; window.addEventListener('keydown', onKey); return () => window.removeEventListener('keydown', onKey); }, []);
  const changePage = (next) => { setPage(next); setQuery(''); setStatus(''); setProjectID(''); setOffset(0); };
  const openExperiment = (id) => { setSearchOpen(false); setModal({ type: 'experiment', id }); };
  const openNew = (initialQuestion = '') => { setActionError(''); setModal({ type: 'new', initialQuestion }); };
  const counts = dashboard?.status_counts || {};
  const connectionText = workspace.loading ? 'Connecting…' : workspace.unauthorized ? 'Authentication required' : workspace.connected ? 'API connected' : 'Connection interrupted';
  const activeProject = projects.find((p) => p.id === projectID);
  async function createResearch(body) {
    setCreating(true); setActionError('');
    let created;
    try {
      created = await api.createExperiment(body);
      setModal({ type: 'experiment', id: created.id }); refresh();
      if (settings?.model_provider !== 'disabled') await api.generatePlan(created.id);
      notify(settings?.model_provider !== 'disabled' ? 'Research plan saved. Review it before running.' : 'Research question saved. Add a plan or connect an AI model to continue.');
    } catch (error) { setActionError(error.message); if (created) notify('Experiment saved. Its plan needs attention.'); }
    finally { setCreating(false); refresh(); }
  }
  async function upload(event) {
    const input = event.currentTarget;
    const file = input.files?.[0];
    if (!file) return;
    setUploading(true); setActionError('');
    try { const dataset = await api.uploadDataset(file); refresh(); notify(`${dataset.name} uploaded · ${dataset.rows} rows`); }
    catch (error) { setActionError(error.message); }
    finally { setUploading(false); input.value = ''; }
  }
  async function downloadDataset(dataset) {
    setDownloadPending(dataset.id); setActionError('');
    try { saveDownload(await api.downloadDataset(dataset.id), dataset.name); }
    catch (error) { setActionError(error.message); }
    finally { setDownloadPending(''); }
  }
  function saveToken(token) { tokenRef.current = token; storeSessionToken(token); setHasToken(!!token); refresh(); notify(token ? 'Token saved. Reconnecting to the API…' : 'Token cleared. Reconnecting…'); }
  return <div className="app-shell">
    <aside className="sidebar">
      <a className="workspace" aria-label="Kairo home" href="/"><span className="brand-mark"><Orbit size={19}/></span><strong>Kairo</strong></a>
      <nav aria-label="Workspace">{nav.map(([label, Icon]) => <button key={label} aria-label={label} title={label} className={`nav-item ${page === label ? 'active' : ''}`} onClick={() => changePage(label)}><Icon size={17}/><span>{label}</span></button>)}</nav>
      <div className="projects-section"><div className="workspace-label">Projects<button className="icon-button" aria-label="New project" title="New project" onClick={() => setModal({ type: 'project' })}><Plus size={14}/></button></div>
        {projects.slice(0, 5).map((project) => <button className={`project ${projectID === project.id ? 'selected' : ''}`} key={project.id} onClick={() => { setPage('Experiments'); setProjectID(project.id); setQuery(''); setStatus(''); setOffset(0); }}><Folder size={14}/><span>{project.name}</span></button>)}
        {projects.length > 5 && <button className="all-projects" onClick={() => setModal({ type: 'projects' })}>All projects<ChevronRight size={12}/></button>}
      </div>
      <div className="sidebar-bottom"><button className="nav-item" aria-label="Settings" onClick={() => setModal({ type: 'settings' })}><Settings2 size={17}/><span>Settings</span></button><button className="nav-item" aria-label="Help and documentation" onClick={() => setModal({ type: 'help' })}><HelpCircle size={17}/><span>Getting started</span></button><div className="workspace-caption">Your research, in one place.</div></div>
    </aside>
    <div className="main-shell">
      <header className="topbar"><div><span>Research Lab</span><ChevronRight size={12}/><strong>{page}</strong></div><div><span className="connection-label"><span className={`live-dot ${workspace.connected ? '' : 'offline'}`}/>{connectionText}</span><button className="icon-button" title="Search experiments · ⌘K" aria-label="Search workspace" onClick={() => setSearchOpen(true)}><Search size={16}/></button></div></header>
      <main>
        <div className="page-heading"><div><h1>{activeProject && page === 'Experiments' ? activeProject.name : page}</h1><p>{{Experiments:'A question, a plan, and the evidence.', Reports:'Findings from your completed experiments.', Datasets:'The data behind your research.'}[page]}</p></div>{page === 'Experiments' && <button className="button primary" disabled={creating || !workspace.connected} onClick={() => openNew()}><Plus size={15}/>New experiment</button>}{page === 'Datasets' && <label className={`button primary upload-label ${uploading || !workspace.connected ? 'disabled' : ''}`}>{uploading ? <LoaderCircle className="spinning" size={14}/> : <Plus size={15}/>} {uploading ? 'Uploading…' : 'Upload CSV'}<input aria-label="Upload CSV dataset" type="file" accept=".csv,text/csv" disabled={uploading || !workspace.connected} onChange={upload}/></label>}</div>
        {workspace.error && <div className="connection-banner" role="alert"><div><strong>{workspace.unauthorized ? 'Sign in to your research API' : 'Unable to refresh the workspace'}</strong><p>{workspace.error}{dashboard && ' Showing previously loaded data.'}</p></div><button className="button secondary" onClick={refresh}>Retry</button><button className="button secondary" onClick={() => setModal({ type: 'settings' })}>Settings</button></div>}
        {actionError && <div className="inline-error global-error" role="alert"><span>{actionError}</span><button className="icon-button" aria-label="Dismiss error" onClick={() => setActionError('')}><X size={15}/></button></div>}
        {page === 'Experiments' && <>
          <div className="workspace-summary"><span><strong>{dashboard?.total_experiments ?? '—'}</strong> total</span><span><i className="summary-dot running"/><strong>{counts.running ?? '—'}</strong> running</span><span><i className="summary-dot completed"/><strong>{counts.completed ?? '—'}</strong> completed</span>{activeProject && <button className="filter-chip" onClick={() => { setProjectID(''); setOffset(0); }} aria-label="Clear project filter">{activeProject.name}<X size={12}/></button>}</div>
          {!modal && <VoiceAgent api={api} available={settings?.voice_enabled} context={{ view: 'experiments', total_experiments: dashboard?.total_experiments, running: counts.running }} onQuestion={openNew}/>}
          <section className="experiments-section" aria-label="Experiment list"><div className="table-toolbar"><div className="table-search"><Search size={15}/><input aria-label="Filter experiments" placeholder="Search experiments…" value={query} onChange={(event) => { setQuery(event.target.value); setOffset(0); }}/></div><select className="status-filter" aria-label="Filter by experiment status" value={status} onChange={(event) => { setStatus(event.target.value); setOffset(0); }}><option value="">All statuses</option>{Object.entries(labels).map(([value, label]) => <option value={value} key={value}>{label}</option>)}</select></div>
            <div className="table-scroll"><table><thead><tr><th>Experiment</th><th>Status</th><th className="updated-column">Updated</th><th/></tr></thead><tbody>{runs.map((run) => { const { Icon, color } = domains[run.plan?.domain] || { Icon: FlaskConical, color: 'gray' }; return <tr key={run.id} onClick={() => openExperiment(run.id)} tabIndex={0} onKeyDown={(event) => { if (event.key === 'Enter') openExperiment(run.id); }}><td><div className="experiment-name"><span className={`domain-icon ${color}`}><Icon size={17}/></span><div><strong>{run.title}</strong><p>{domainLabels[run.plan?.domain] || run.plan?.domain || 'Awaiting a plan'}{run.plan && ` · Plan v${run.plan_version}`}</p></div></div></td><td><Status status={run.status}/>{isBusy(run.status) && <div className="progress"><i style={{ width: `${run.progress}%` }}/></div>}</td><td className="time-cell updated-column">{timeAgo(run.updated_at)}</td><td><button className="icon-button row-open" aria-label={`Open experiment ${shortID(run.id)}`} onClick={(event) => { event.stopPropagation(); openExperiment(run.id); }}><ChevronRight size={16}/></button></td></tr>; })}</tbody></table>
              {!runs.length && <div className="empty-state">{workspace.loading ? <><LoaderCircle className="spinning" size={23}/><h3>Loading your research…</h3></> : <><span className="empty-icon"><FlaskConical size={24}/></span><h3>{query || status || projectID ? 'No matching experiments' : 'Your next discovery starts here'}</h3><p>{query || status || projectID ? 'Try another search or change your filter.' : 'Create an experiment to turn a question into evidence.'}</p></>}</div>}
            </div>
            {workspace.total > pageSize && <div className="table-pagination"><span>{offset + 1}–{Math.min(offset + pageSize, workspace.total)} of {workspace.total}</span><div><button className="icon-button" aria-label="Previous experiments" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}><ChevronLeft size={15}/></button><button className="icon-button" aria-label="Next experiments" disabled={offset + pageSize >= workspace.total} onClick={() => setOffset(offset + pageSize)}><ChevronRight size={15}/></button></div></div>}
          </section>
        </>}
        {page === 'Reports' && (reports.length ? <section className="resource-list">{reports.map((report) => <button className="resource-row" key={report.experiment_id} onClick={() => openExperiment(report.experiment_id)}><span className="domain-icon"><FileText size={18}/></span><span><strong>{report.title}</strong><small>{timeAgo(report.updated_at)}</small></span><ChevronRight size={16}/></button>)}</section> : <div className="empty-state"><span className="empty-icon"><FileText size={24}/></span><h3>No reports yet</h3><p>Completed experiments will appear here.</p></div>)}
        {page === 'Datasets' && <section className="resource-list">{datasets.map((dataset) => <div className="resource-row" key={dataset.id}><span className="domain-icon"><Folder size={18}/></span><button className="dataset-name" aria-label={`Preview ${dataset.name}`} onClick={() => setModal({ type: 'dataset', dataset })}><strong>{dataset.name}</strong><small>{dataset.rows} rows · {dataset.columns.length} columns</small></button><button className="icon-button" title="Download CSV" aria-label={`Download ${dataset.name}`} disabled={downloadPending === dataset.id} onClick={() => downloadDataset(dataset)}><ArrowDownToLine size={16}/></button></div>)}{!datasets.length && <div className="empty-state"><span className="empty-icon"><Folder size={24}/></span><h3>No datasets yet</h3><p>Upload a CSV to use it in an experiment.</p><small>Up to 10 MiB · 50,000 rows</small></div>}</section>}
      </main>
    </div>
    {searchOpen && <WorkspaceSearch api={api} onOpen={openExperiment} onClose={closeSearch}/>}
    {modal && <Dialog key={modal.type === 'experiment' ? modal.id : modal.type} title={modal.type === 'experiment' ? 'Experiment' : { new: 'A new experiment', settings: 'Workspace settings', help: 'A home for your next discovery', project: 'Create a project', projects: 'Your research projects', dataset: modal.dataset?.name }[modal.type]} onClose={closeModal} className={modal.type === 'experiment' ? 'experiment-modal' : ''}>
      {modal.type === 'experiment' && <ExperimentDetails api={api} id={modal.id} revision={revision} settings={settings} datasets={datasets} onChanged={refresh} onClose={closeModal} notify={notify} onNewQuestion={openNew}/>}
      {modal.type === 'new' && <ResearchForm api={api} settings={settings} projects={projects} datasets={datasets} initialQuestion={modal.initialQuestion} onSubmit={createResearch} creating={creating}/>}
      {modal.type === 'settings' && <ConnectionSettings api={api} settings={settings} unauthorized={workspace.unauthorized} hasToken={hasToken} onSaveToken={saveToken} tools={tools}/>}
      {modal.type === 'project' && <ProjectForm api={api} onSaved={() => { refresh(); closeModal(); notify('Project created'); }}/>}
      {modal.type === 'projects' && <><div className="detail-section">{projects.map((project) => <button className="project-list-item" key={project.id} onClick={() => { setPage('Experiments'); setProjectID(project.id); setStatus(''); setQuery(''); setOffset(0); closeModal(); }}><Folder size={16}/>{project.name}</button>)}{!projects.length && <p>No projects yet.</p>}</div><button className="button primary" onClick={() => setModal({ type: 'project' })}><Plus size={14}/>New project</button></>}
      {modal.type === 'dataset' && <><p className="form-help">{modal.dataset.rows} rows · SHA-256 {modal.dataset.sha256}</p><div className="dataset-preview"><table><thead><tr>{modal.dataset.columns.map((column) => <th key={column}>{column}</th>)}</tr></thead><tbody>{modal.dataset.preview.map((row, i) => <tr key={i}>{row.map((cell, j) => <td key={j}>{cell}</td>)}</tr>)}</tbody></table></div><p className="form-help">First {modal.dataset.preview.length} rows. Attach this dataset when creating an experiment or before planning a draft.</p><div className="modal-actions"><button className="button secondary" onClick={() => downloadDataset(modal.dataset)} disabled={!!downloadPending}><ArrowDownToLine size={14}/>Download CSV</button></div></>}
      {modal.type === 'help' && <><p>Ask a question, review a research plan, and follow the experiment from execution to evidence.</p><div className="detail-section"><h3>Start with your research question</h3><p>Choose New experiment and describe what you want to investigate. Generate a plan with a connected model or add your own manual plan, then review it before running.</p><h3>Use AI planning</h3><p>Configure Ollama or a compatible model on the Go backend. AI mode generates a plan for your question, which you review before execution. Unsupported requests and missing inputs appear as explicit errors.</p><h3>Results you can reproduce</h3><p>Completed experiments provide findings, charts, CSV data, a Markdown report, and a ZIP containing the source and exact inputs. Open an experiment’s Results and Artifacts tabs to explore them.</p><h3>Connection</h3><p>During development, Vite forwards /api to the Go backend on port 8080. Check Workspace settings if the backend requires a token. Configuration and deployment instructions are in the project README.</p></div></>}
    </Dialog>}
    {toast && <div className="toast" role="status"><CircleCheck size={16}/>{toast}</div>}
  </div>;
}

function ProjectForm({ api, onSaved }) {
  const [name, setName] = useState(''); const [description, setDescription] = useState('');
  const [pending, setPending] = useState(false); const [error, setError] = useState('');
  return <form onSubmit={async (event) => { event.preventDefault(); setPending(true); setError(''); try { await api.createProject({ name: name.trim(), description }); onSaved(); } catch (cause) { setError(cause.message); } finally { setPending(false); } }}><p className="form-help">Group related experiments into a research project.</p><label className="field-label" htmlFor="project-name">Project name</label><input data-autofocus className="form-input" id="project-name" required maxLength={120} value={name} onChange={(event) => setName(event.target.value)}/><label className="field-label" htmlFor="project-description">Description</label><textarea className="form-input" id="project-description" maxLength={2000} rows={3} value={description} onChange={(event) => setDescription(event.target.value)}/>{error && <div className="inline-error" role="alert">{error}</div>}<div className="modal-actions"><button className="button primary" disabled={pending || !name.trim()}>{pending ? <LoaderCircle size={14} className="spinning"/> : <Plus size={14}/>}Create project</button></div></form>;
}
