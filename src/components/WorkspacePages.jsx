import React, { useEffect, useState } from 'react';
import { Activity, ArrowUpRight, BookOpen, ChevronRight, FileText, FlaskConical, Folder, LoaderCircle } from 'lucide-react';
import { labels, shortID, timeAgo } from '../templates';

export function OverviewPage({ dashboard, projects, datasets, experiments, onOpenExperiment, onNavigate, onNew }) {
  return <><div className="overview-metrics">{[['Experiments', dashboard?.total_experiments ?? 0, FlaskConical, 'Experiments'], ['Completed', dashboard?.status_counts?.completed ?? 0, FileText, 'Reports'], ['Projects', projects.length, Folder, 'Projects'], ['Datasets', datasets.length, BookOpen, 'Datasets']].map(([label, count, Icon, page]) => <button key={label} onClick={() => onNavigate(page)}><Icon size={18}/><strong>{count}</strong><span>{label}</span><ArrowUpRight size={13}/></button>)}</div><div className="overview-section-heading"><h2>Recent experiments</h2><button className="button secondary" onClick={() => onNavigate('Experiments')}>View all<ChevronRight size={13}/></button></div>{experiments.length ? <div className="resource-list">{experiments.slice(0, 5).map((run) => <button className="resource-row" key={run.id} onClick={() => onOpenExperiment(run.id)}><span className="domain-icon"><FlaskConical size={18}/></span><span><strong>{run.title}</strong><small>{labels[run.status]} · {timeAgo(run.updated_at)}</small></span><ChevronRight size={15}/></button>)}</div> : <div className="empty-state"><FlaskConical size={28}/><h3>Your workspace is ready</h3><p>Start with a research question.</p><button className="button primary" onClick={onNew}>New experiment</button></div>}</>;
}

export function ProjectsPage({ projects, onOpen, onNew }) {
  return projects.length ? <div className="project-grid">{projects.map((project) => <button className="project-card" key={project.id} onClick={() => onOpen(project)}><Folder size={22}/><h2>{project.name}</h2><p>{project.description || 'Research project'}</p><span>View experiments<ArrowUpRight size={13}/></span></button>)}</div> : <div className="empty-state"><Folder size={28}/><h3>A place for every research direction</h3><p>Group related experiments in a project.</p><button className="button secondary" onClick={onNew}>Create a project</button></div>;
}

export function ActivityPage({ api, revision, onOpenExperiment }) {
  const [state, setState] = useState({ data: [], loading: true, error: '' });
  useEffect(() => {
    const controller = new AbortController(); let timer;
    async function load() {
      try { const result = await api.activity(controller.signal); if (!controller.signal.aborted) setState({ data: result.data, loading: false, error: '' }); }
      catch (failure) { if (!controller.signal.aborted) setState((old) => ({ ...old, loading: false, error: failure.message })); }
      if (!controller.signal.aborted) timer = setTimeout(load, 5000);
    }
    load(); return () => { controller.abort(); clearTimeout(timer); };
  }, [api, revision]);
  if (state.loading) return <p className="loading-state"><LoaderCircle className="spinning" size={18}/>Loading activity…</p>;
  return <>{state.error && <p className="inline-error" role="alert">{state.error}</p>}{state.data.length ? <div className="workspace-activity">{state.data.map((event) => <button key={event.id} onClick={() => onOpenExperiment(event.experiment_id)}><span className="activity-marker"><Activity size={14}/></span><span><strong>{event.message}</strong><small>{event.kind.replaceAll('_', ' ')} · {shortID(event.experiment_id)}</small></span><time>{timeAgo(event.created_at)}</time><ChevronRight size={14}/></button>)}</div> : <div className="empty-state"><Activity size={26}/><h3>Your research timeline</h3><p>Experiment events and generated documents will appear here.</p></div>}</>;
}
