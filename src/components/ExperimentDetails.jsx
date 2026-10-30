import React, { useEffect, useRef, useState } from 'react';
import { ArrowDownToLine, Check, FileText, LoaderCircle, Play, RefreshCw, Square, Trash2 } from 'lucide-react';
import { useExperiment } from '../hooks';
import { domainLabels, isBusy, labels, shortID, timeAgo } from '../templates';
import { saveDownload } from '../api';
import ResultChart from './ResultChart';
import VoiceAgent from './VoiceAgent';
import { DocumentActions } from './ResearchDocuments';
import { experimentVoiceContext } from '../voice-context';

export function Status({ status }) {
  return <span className={`status ${status?.replaceAll('_', '-')}`}>{isBusy(status) ? <LoaderCircle className="spinning" size={12}/> : status === 'completed' ? <Check size={12}/> : <span className="status-dot"/>}{labels[status] || status}</span>;
}

export default function ExperimentDetails({ api, id, revision, settings, datasets, onChanged, onClose, notify, onNewQuestion, onAskKairo, onDocumentCreated }) {
  const detail = useExperiment(api, id, revision);
  const [pending, setPending] = useState('');
  const [actionError, setActionError] = useState('');
  const [editing, setEditing] = useState(false);
  const [planText, setPlanText] = useState('');
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [tab, setTab] = useState('Plan');
  const experiment = detail.experiment;
  const initialized = useRef(false);
  useEffect(() => {
    if (experiment && !initialized.current) {
      initialized.current = true;
      if (experiment.status === 'completed') setTab('Results');
    }
  }, [experiment]);
  async function perform(label, fn) {
    setPending(label); setActionError('');
    try { await fn(); detail.refresh(); onChanged(); }
    catch (error) { setActionError(error.message); detail.refresh(); onChanged(); }
    finally { setPending(''); }
  }
  function download(type, name) {
    return perform(`Downloading ${type}`, async () => {
      const result = type === 'bundle' ? await api.downloadBundle(id) : type === 'report' ? await api.downloadReport(id) : await api.downloadArtifact(id, name);
      saveDownload(result, name?.split('/').at(-1) || `${id}-${type === 'bundle' ? 'reproducibility.zip' : 'report.md'}`);
      notify('Download ready');
    });
  }
  if (detail.loading && !experiment) return <div className="loading-state"><LoaderCircle className="spinning" size={22}/>Loading experiment…</div>;
  if (!experiment) return <div className="inline-error" role="alert">{detail.error || 'Experiment is unavailable.'}<button className="button secondary" onClick={detail.refresh}>Retry</button></div>;
  const busy = isBusy(experiment.status);
  const canPlan = !busy && experiment.status !== 'completed';
  const canRun = experiment.plan && ['planned', 'failed', 'cancelled'].includes(experiment.status);
  const plan = experiment.plan;
  const dataset = datasets.find((d) => d.id === experiment.dataset_id);
  return <>
    <div className="experiment-dialog-heading"><span className="eyebrow">{shortID(id)}{plan && ` · ${domainLabels[plan.domain] || plan.domain}`}</span><h3>{experiment.title}</h3><div><Status status={experiment.status}/><span className="detail-caption">Updated {timeAgo(experiment.updated_at)}</span></div></div>
    {(detail.error || actionError || experiment.error) && <div className="inline-error" role="alert">{actionError || experiment.error || detail.error}</div>}
    {busy && <div className="run-progress"><div><span>{experiment.status === 'planning' ? 'The model is designing your experiment…' : experiment.status === 'queued' ? 'Waiting for a scientific worker…' : 'Running and analyzing the experiment…'}</span><strong>{experiment.progress}%</strong></div><div className="progress"><i style={{ width: `${experiment.progress}%` }}/></div></div>}
    <VoiceAgent showWakeActivation={false} api={api} available={settings?.voice_enabled} context={experimentVoiceContext(experiment)} onQuestion={onNewQuestion} onDocumentCreated={onDocumentCreated}/>
    {onAskKairo && <button type="button" className="chat-form-link" onClick={()=>onAskKairo('',experiment)}>Discuss this experiment with Kairo<ArrowDownToLine size={13}/></button>}
    <div className="detail-tabs" role="group" aria-label="Experiment sections">{['Plan', 'Results', 'Activity', 'Artifacts'].map((name) => <button className={tab === name ? 'selected' : ''} key={name} onClick={() => setTab(name)}>{name}{name === 'Artifacts' && detail.artifacts.length > 0 && <span>{detail.artifacts.length}</span>}</button>)}</div>
    {tab === 'Plan' && <>
      {experiment.question !== experiment.title && <div className="detail-section"><h3>Research question</h3><p>{experiment.question}</p></div>}{dataset && <p className="form-help">Dataset: {dataset.name}</p>}
      {plan && !editing && <>
        <div className="detail-section"><h3>Hypothesis</h3><p>{plan.hypothesis}</p></div>
        <div className="detail-section"><h3>Methodology</h3><p>{plan.methodology}</p><div className="plan-metadata"><span className="domain-tag">{({python:'Python',javascript:'JavaScript',r:'R',go:'Go',cpp:'C++'})[plan.language] || plan.language || 'Python'}</span><span className="domain-tag">{plan.source === 'model' ? plan.model : 'Manual plan'}</span><span className="domain-tag">{plan.timeout_seconds}s timeout</span></div></div>
        {plan.language_reason && <p className="form-help">{plan.language_reason}</p>}
        <details className="code-disclosure"><summary>Assumptions & limitations</summary><div className="detail-section"><h3>Assumptions</h3><ul>{plan.assumptions.map((s, i) => <li key={i}>{s}</li>)}</ul><h3>Limitations</h3><ul>{plan.limitations.map((s, i) => <li key={i}>{s}</li>)}</ul></div></details>
        {plan.code && <details className="code-disclosure"><summary>Experiment code · {({python:'Python',javascript:'JavaScript',r:'R',go:'Go',cpp:'C++'})[plan.language] || plan.language || 'Python'}</summary><p className="form-help">Review this custom code and its methodology before running. Execution requires an isolated Docker worker.</p><pre>{plan.code}</pre></details>}
        <details className="code-disclosure"><summary>Parameters & variables</summary><pre>{JSON.stringify({ parameters: plan.parameters, independent_variables: plan.independent_variables, dependent_variables: plan.dependent_variables, controlled_variables: plan.controlled_variables }, null, 2)}</pre></details>
      </>}
      {!plan && !editing && <div className="inline-notice">No plan yet. {settings?.model_provider === 'disabled' ? 'Connect an AI model to design a custom experiment, or paste a manual plan below.' : 'Generate a plan with the configured model, or supply a manual plan.'}</div>}
      {canPlan && !editing && <div className="plan-controls">
        {settings?.model_provider !== 'disabled' && <button className="button secondary" disabled={!!pending || !settings} onClick={() => perform('Generating plan', () => api.generatePlan(id))}><RefreshCw size={13}/>{pending === 'Generating plan' ? 'Generating…' : plan ? 'Regenerate AI plan' : 'Generate AI plan'}</button>}
        <button className="button secondary" disabled={!!pending} onClick={() => { setPlanText(plan ? JSON.stringify(plan, null, 2) : ''); setEditing(true); }}>{plan ? 'Edit plan' : 'Paste a plan'}</button>
      </div>}
      {editing && <form className="plan-editor" onSubmit={(event) => {
        event.preventDefault();
        let parsed;
        try { parsed = JSON.parse(planText); } catch { setActionError('Plan must be valid JSON.'); return; }
        perform('Saving plan', async () => { await api.savePlan(id, parsed); setEditing(false); });
      }}><label className="field-label" htmlFor="plan-json">Experiment plan JSON</label><textarea className="form-input code-input" id="plan-json" rows={16} value={planText} onChange={(event) => setPlanText(event.target.value)} required/><p className="form-help">Saving creates a new plan version. The question must match this experiment, and all parameters must pass backend validation.</p><div className="modal-actions"><button type="button" className="button secondary" onClick={() => setEditing(false)}>Discard changes</button><button className="button primary" disabled={!!pending}>Save plan</button></div></form>}
      {!busy && !experiment.attempt && datasets.length > 0 && <details className="code-disclosure"><summary>Change dataset</summary><div className="detail-section"><label className="field-label" htmlFor="attach-dataset">Attached dataset</label><select id="attach-dataset" className="form-input" value={experiment.dataset_id || ''} disabled={!!pending} onChange={(event) => perform('Attaching dataset', () => api.updateExperiment(id, { dataset_id: event.target.value }))}><option value="">No dataset</option>{datasets.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}</select><p className="form-help">Changing the dataset clears the current plan. Generate or save a new plan after attaching it.</p></div></details>}
      {detail.plans.length > 0 && <details className="code-disclosure"><summary>Plan history · {detail.plans.length} version{detail.plans.length === 1 ? '' : 's'}</summary>{detail.plans.map((entry) => <details key={entry.version}><summary>Version {entry.version} · {entry.plan.source}</summary><pre>{JSON.stringify(entry.plan, null, 2)}</pre></details>)}</details>}
    </>}
    {tab === 'Results' && (experiment.result ? <>
      <p className="result-kind">{experiment.result.kind.replaceAll('_', ' ')} · Computed by {plan?.adapter}</p>
      <ResultChart result={experiment.result} adapter={plan?.adapter}/>
      <div className="detail-section"><h3>Computed findings</h3><ul>{experiment.result.findings.map((s, i) => <li key={i}>{s}</li>)}</ul></div>
      <details className="code-disclosure"><summary>All computed metrics</summary><pre>{JSON.stringify(experiment.result.metrics, null, 2)}</pre></details>
      {experiment.result.narrative && <div className="detail-section"><h3>AI interpretation</h3><p className="narrative-text">{experiment.result.narrative}</p><p className="form-help">{experiment.result.narrative_source} · Interpretation is not independent validation.</p></div>}
      <details className="code-disclosure"><summary>Limitations</summary><div className="detail-section"><ul>{experiment.result.limitations.map((s, i) => <li key={i}>{s}</li>)}</ul></div></details>
      {experiment.result.warnings?.map((s, i) => <div className="inline-notice" key={i}>{s}</div>)}
    </> : <div className="empty-state"><FileText size={22}/><h3>No results yet</h3><p>Review the plan and run the experiment to collect evidence.</p></div>)}
    {tab === 'Activity' && <div className="event-log">{detail.events.length ? detail.events.map((event) => <div key={event.id}><span className="status-dot"/><div><strong>{event.kind.replaceAll('_', ' ')}</strong><p>{event.message}</p><time>{new Date(event.created_at).toLocaleString()}</time></div></div>) : <p>No events available.</p>}</div>}
    {tab === 'Artifacts' && <div className="artifact-list">{detail.artifacts.length ? detail.artifacts.map((artifact) => <button key={artifact.name} disabled={!!pending} onClick={() => download('artifact', artifact.name)}><FileText size={16}/><span>{artifact.name}<small>{(artifact.size / 1024).toFixed(1)} KB · SHA-256 {artifact.sha256.slice(0, 12)}…</small></span><ArrowDownToLine size={15}/></button>) : <div className="empty-state"><h3>No artifacts yet</h3><p>Successful runs produce code, data, charts, reports, and reproduction instructions.</p></div>}</div>}
    <DocumentActions api={api} experiment={experiment} onGenerated={(document) => { onChanged(); onDocumentCreated(document); }}/>
    {pending && <p className="action-progress" role="status"><LoaderCircle size={13} className="spinning"/>{pending}…</p>}
    <div className="experiment-actions">
      <div>{!busy && <details className="more-actions"><summary>More</summary><button className="button danger-quiet" disabled={!!pending} onClick={() => setConfirmDelete((value) => !value)}><Trash2 size={13}/>Delete</button></details>}</div>
      <div>{detail.artifacts.length > 0 && <button className="button secondary" disabled={!!pending} onClick={() => download('bundle')}><ArrowDownToLine size={13}/>Reproduction bundle</button>}{experiment.status === 'completed' && <button className="button secondary" disabled={!!pending} onClick={() => download('report')}><FileText size={13}/>Report</button>}{busy && <button className="button secondary" disabled={pending === 'Cancelling'} onClick={() => perform('Cancelling', () => api.cancelExperiment(id))}><Square size={12}/>Cancel run</button>}{canRun && <button className="button primary" disabled={!!pending || editing} onClick={() => perform('Starting experiment', () => api.runExperiment(id))}><Play size={13}/>{experiment.attempt ? 'Retry experiment' : 'Run experiment'}</button>}</div>
    </div>
    {confirmDelete && <div className="inline-error delete-confirm"><p>Delete this experiment, its saved artifacts, and generated reports and manuscripts? This cannot be undone.</p><button className="button danger" disabled={!!pending} onClick={() => perform('Deleting experiment', async () => { await api.deleteExperiment(id); notify('Experiment deleted'); onClose(); })}>Delete permanently</button><button className="button secondary" onClick={() => setConfirmDelete(false)}>Keep experiment</button></div>}
  </>;
}
