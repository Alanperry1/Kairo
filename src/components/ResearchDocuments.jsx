import React, { useEffect, useRef, useState } from 'react';
import { ArrowDownToLine, BookOpen, Check, ChevronRight, Copy, FileCode2, FileText, LoaderCircle, Sparkles } from 'lucide-react';
import { saveDownload } from '../api';
import { shortID, timeAgo } from '../templates';

export function DocumentActions({ api, experiment, onGenerated }) {
  const [includeFigures, setIncludeFigures] = useState(true);
  const [pending, setPending] = useState('');
  const request = useRef(null);
  useEffect(() => () => request.current?.abort(), []);
  const [error, setError] = useState('');
  if (experiment.status !== 'completed' || !experiment.result) return null;
  async function generate(kind) {
    const controller = new AbortController(); request.current = controller;
    setPending(kind); setError('');
    try { const document = await (includeFigures ? api.generateDocument(experiment.id, kind, controller.signal) : api.generateDocument(experiment.id, kind, controller.signal, { include_figures: false })); if (!controller.signal.aborted) onGenerated(document); }
    catch (failure) { if (!controller.signal.aborted) setError(failure.message); }
    finally { if (!controller.signal.aborted) setPending(''); }
  }
  return <div className="document-actions"><div><span className="document-agent-mark"><Sparkles size={16}/></span><span><strong>Write from your evidence</strong><small>Create a saved draft from this completed run.</small></span></div><div className="document-action-buttons"><button className="button secondary" disabled={!!pending} onClick={() => generate('report')}>{pending === 'report' ? <LoaderCircle className="spinning" size={13}/> : <FileText size={13}/>}Generate report</button><button className="button secondary" disabled={!!pending} onClick={() => generate('manuscript')}>{pending === 'manuscript' ? <LoaderCircle className="spinning" size={13}/> : <BookOpen size={13}/>}Generate manuscript</button></div><label className="figure-option"><input type="checkbox" checked={includeFigures} disabled={!!pending} onChange={(event)=>setIncludeFigures(event.target.checked)}/>Include figures when useful</label>{error && <p className="inline-error" role="alert">{error}</p>}{pending && <p className="form-help" role="status">The writing agent is drafting your {pending}…</p>}</div>;
}

export default function ResearchDocuments({ api, kind, revision, onOpenDocument, runReports = [], onOpenExperiment }) {
  const [documents, setDocuments] = useState([]);
  const [experiments, setExperiments] = useState([]);
  const [selected, setSelected] = useState('');
  const [loading, setLoading] = useState(true);
  const [includeFigures, setIncludeFigures] = useState(true);
  const [pending, setPending] = useState(false);
  const request = useRef(null);
  useEffect(() => () => request.current?.abort(), []);
  const [error, setError] = useState('');
  const [reload, setReload] = useState(0);
  const manuscript = kind === 'manuscript';
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    Promise.all([api.documents({ kind, limit: 100 }, controller.signal), api.listExperiments({ status: 'completed', limit: 100 }, controller.signal)]).then(([saved, completed]) => {
      if (controller.signal.aborted) return;
      setDocuments(saved.data); setExperiments(completed.data);
      setSelected((id) => completed.data.some((run) => run.id === id) ? id : completed.data[0]?.id || '');
    }).catch((failure) => { if (!controller.signal.aborted) setError(failure.message); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [api, kind, revision, reload]);
  async function generate() {
    if (!selected) return; setPending(true); setError('');
    const controller = new AbortController(); request.current = controller;
    try { const document = await (includeFigures ? api.generateDocument(selected, kind, controller.signal) : api.generateDocument(selected, kind, controller.signal, { include_figures: false })); if (!controller.signal.aborted) { setDocuments((current) => [document, ...current]); onOpenDocument(document); } }
    catch (failure) { if (!controller.signal.aborted) setError(failure.message); }
    finally { if (!controller.signal.aborted) setPending(false); }
  }
  return <>
    <div className="document-composer"><div className="document-composer-heading"><span className="document-agent-mark"><Sparkles size={19}/></span><div><h2>{manuscript ? 'From evidence to manuscript.' : 'A report, grounded in your results.'}</h2><p>{manuscript ? 'A LaTeX research draft with your method, results, limitations, and provenance.' : 'Let the writing agent turn a completed experiment into a research report.'}</p></div></div><label className="field-label" htmlFor="document-experiment">Completed experiment</label><div className="document-composer-controls"><select className="form-input" id="document-experiment" value={selected} disabled={loading || pending || !experiments.length} onChange={(event) => setSelected(event.target.value)}>{!experiments.length && <option value="">Complete an experiment first</option>}{experiments.map((run) => <option value={run.id} key={run.id}>{run.title}</option>)}</select><button className="button primary" disabled={!selected || pending || loading} onClick={generate}>{pending ? <LoaderCircle className="spinning" size={14}/> : <Sparkles size={14}/>}Generate {manuscript ? 'manuscript' : 'report'}</button></div><label className="figure-option"><input type="checkbox" checked={includeFigures} disabled={pending} onChange={(event)=>setIncludeFigures(event.target.checked)}/>Include figures when useful</label>{pending && <p className="form-help" role="status">Drafting from the recorded experiment evidence…</p>}</div>
    {error && <div className="inline-error" role="alert">{error}<button className="button secondary" onClick={() => setReload((value) => value + 1)}>Retry</button></div>}
    {loading ? <p className="loading-state"><LoaderCircle className="spinning" size={18}/>Loading documents…</p> : documents.length ? <section className="resource-list document-library" aria-label={manuscript ? 'Saved manuscripts' : 'Saved research reports'}>{documents.map((document) => <button className="resource-row" key={document.id} onClick={() => onOpenDocument(document)}><span className="domain-icon">{manuscript ? <FileCode2 size={18}/> : <FileText size={18}/>}</span><span><strong>{document.title}</strong><small>{manuscript ? 'LaTeX' : 'Markdown'} · {document.source === 'model' ? 'AI draft' : 'Evidence draft'} · {timeAgo(document.created_at)}</small></span><ChevronRight size={16}/></button>)}</section> : <div className="empty-state document-empty"><BookOpen size={27}/><h3>{manuscript ? 'Your manuscript library starts here' : 'No research reports yet'}</h3><p>Choose a completed experiment above to create your first draft.</p></div>}
    {!manuscript && runReports.length > 0 && <section className="run-reports"><h2>Run reports</h2><div className="resource-list">{runReports.map((report) => <button className="resource-row" key={report.experiment_id} onClick={() => onOpenExperiment(report.experiment_id)}><span className="domain-icon"><FileText size={17}/></span><span><strong>{report.title}</strong><small>Computed experiment report · {timeAgo(report.updated_at)}</small></span><ChevronRight size={16}/></button>)}</div></section>}
  </>;
}

export function DocumentViewer({ api, initialDocument, onOpenExperiment }) {
  const [document, setDocument] = useState(initialDocument.content ? initialDocument : null);
  const [error, setError] = useState('');
  const [pending, setPending] = useState('');
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (initialDocument.content) return;
    const controller = new AbortController();
    api.getDocument(initialDocument.id, controller.signal).then((value) => { if (!controller.signal.aborted) setDocument(value); }).catch((failure) => { if (!controller.signal.aborted) setError(failure.message); });
    return () => controller.abort();
  }, [api, initialDocument]);
  async function download(bundle) {
    setPending(bundle ? 'bundle' : 'source'); setError('');
    try { const result = bundle ? await api.downloadDocumentBundle(document.id) : await api.downloadDocument(document.id); saveDownload(result, bundle ? `${document.id}-source.zip` : `${document.id}.${document.kind === 'manuscript' ? 'tex' : 'md'}`); }
    catch (failure) { setError(failure.message); }
    finally { setPending(''); }
  }
  async function copy() { try { await navigator.clipboard.writeText(document.content); setCopied(true); } catch { setError('Copy was unavailable. Download the source instead.'); } }
  if (!document) return error ? <div className="inline-error" role="alert">{error}</div> : <p className="loading-state"><LoaderCircle className="spinning" size={18}/>Loading draft…</p>;
  const manuscript = document.kind === 'manuscript';
  return <><div className="document-viewer-heading"><h3>{document.title}</h3><span>{manuscript ? 'LaTeX manuscript' : 'Research report'} · {document.source === 'model' ? document.model : 'Recorded evidence'} · {timeAgo(document.created_at)}</span></div><p className="inline-notice">Research draft. Review scientific claims, citations, and authorship before sharing.{manuscript && ' Export the .tex source to compile in LuaLaTeX or Overleaf.'}</p>{error && <p className="inline-error" role="alert">{error}</p>}{document.figures?.length > 0 && <section className="document-figures" aria-label="Report figures"><div className="document-figures-heading"><strong>Figures from your evidence</strong><small>Matplotlib · PNG + vector PDF</small></div>{document.figures.map((figure,index)=><DocumentFigurePreview key={figure.name} api={api} documentID={document.id} figure={figure} index={index}/>)}</section>}<div className="document-toolbar"><span>{manuscript ? 'main.tex' : 'report.md'}</span><button className="button secondary" onClick={copy}>{copied ? <Check size={13}/> : <Copy size={13}/>} {copied ? 'Copied' : 'Copy source'}</button></div><pre className="document-source" aria-label={manuscript ? 'LaTeX manuscript source' : 'Research report content'}>{document.content}</pre><div className="document-provenance"><button className="lp-text-link" onClick={() => onOpenExperiment(document.experiment_id)}>Experiment {shortID(document.experiment_id)}<ChevronRight size={12}/></button><span>Attempt {document.attempt} · Plan v{document.plan_version}</span></div><div className="modal-actions"><button className={`button ${document.figures?.length ? 'primary' : 'secondary'}`} disabled={!!pending} onClick={() => download(true)}><ArrowDownToLine size={13}/>{document.figures?.length ? 'Source, figures & evidence ZIP' : 'Source & evidence ZIP'}</button><button className={`button ${document.figures?.length ? 'secondary' : 'primary'}`} disabled={!!pending} onClick={() => download(false)}>{pending === 'source' ? <LoaderCircle className="spinning" size={13}/> : <ArrowDownToLine size={13}/>}Download {manuscript ? '.tex' : '.md'}</button></div>{document.figures?.length > 0 && <p className="form-help">Download the ZIP to keep the figures alongside your source. The manuscript includes vector PDFs; the report uses PNGs.</p>}</>;
}

function DocumentFigurePreview({ api, documentID, figure, index }) {
 const [state,setState]=useState({url:'',error:''});
 useEffect(()=>{
  const controller=new AbortController();let url;
  api.downloadDocumentFigure(documentID,figure.name,controller.signal).then(({blob})=>{if(controller.signal.aborted)return;url=URL.createObjectURL(blob);setState({url,error:''});}).catch((error)=>{if(!controller.signal.aborted)setState({url:'',error:error.message});});
  return ()=>{controller.abort();if(url)URL.revokeObjectURL(url);};
 },[api,documentID,figure.name]);
 return <figure className="document-figure">{state.url ? <img src={state.url} alt={`Figure ${index+1}: ${figure.caption}`}/> : state.error ? <p className="inline-error" role="alert">Figure preview unavailable. {state.error}</p> : <p className="loading-state"><LoaderCircle size={16} className="spinning"/>Loading figure…</p>}<figcaption><strong>Figure {index+1}.</strong> {figure.caption}<small>{figure.source} · {figure.renderer}</small></figcaption></figure>;
}
