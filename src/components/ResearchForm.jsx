import React, { useEffect, useState } from 'react';
import { ArrowRight, LoaderCircle } from 'lucide-react';
import VoiceAgent from './VoiceAgent';

export default function ResearchForm({ api, settings, projects, datasets, initialQuestion = '', initialLanguage = 'auto', initialDatasetID = '', onAskKairo, onSubmit, creating, onDocumentCreated }) {
  const [language, setLanguage] = useState(['auto','python','javascript','r','go','cpp'].includes(initialLanguage) ? initialLanguage : 'other');
  const [customLanguage, setCustomLanguage] = useState(['auto','python','javascript','r','go','cpp'].includes(initialLanguage) ? '' : initialLanguage);
  const selectedLanguage = language === 'other' ? customLanguage.trim().toLowerCase() : language;
  const [languages, setLanguages] = useState([]);
  useEffect(() => { if (!api.languages) return; const controller = new AbortController(); api.languages(controller.signal).then((result) => { if (!controller.signal.aborted) setLanguages(result.data.filter(item=>!['rust','ruby','julia'].includes(item.id))); }).catch(() => {}); return () => controller.abort(); }, [api]);
  const [question, setQuestion] = useState(initialQuestion);
  const [voiceDatasets,setVoiceDatasets] = useState([]);
  const formDatasets=[...datasets,...voiceDatasets.filter(d=>!datasets.some(item=>item.id===d.id))];
  const [projectID, setProjectID] = useState('');
  const [datasetID, setDatasetID] = useState(initialDatasetID);
  const aiDisabled = settings?.model_provider === 'disabled';
  const canSubmit = settings && !creating && question.trim().length >= 5 && !['rust','ruby','julia','rs','rb','jl'].includes(selectedLanguage) && /^[a-z][a-z0-9+#-]{0,39}$/.test(selectedLanguage);
  return <form onSubmit={(event) => {
    event.preventDefault();
    if (!canSubmit) return;
    onSubmit({ question: question.trim(), project_id: projectID, dataset_id: datasetID, ...(selectedLanguage !== 'auto' ? { language: selectedLanguage } : {}) });
  }}>
      <label className="field-label" htmlFor="new-research-question">Research question</label>
      <textarea id="new-research-question" data-autofocus className="modal-textarea" required minLength={5} maxLength={4000} value={question} onChange={(event) => setQuestion(event.target.value)} placeholder="What would you like to investigate? Describe your question, data, and constraints."/>
      {aiDisabled ? <div className="inline-notice">Save any research question now. Connect an AI model to generate its method and code, or add a manual plan after saving.</div> : <p className="form-help">The agent chooses a language and libraries for your method. Review the plan before running.</p>}
    <label className="field-label" htmlFor="research-language">Experiment language</label><select id="research-language" className="form-input" value={language} onChange={(event) => setLanguage(event.target.value)}><option value="auto">Let the agent choose</option>{(languages.length ? languages : [{id:'python'}, {id:'javascript'}, {id:'r'}, {id:'go'}, {id:'cpp'}]).map((item) => <option key={item.id} value={item.id}>{({python:'Python', javascript:'JavaScript',r:'R',go:'Go',cpp:'C++'})[item.id] || item.id}{item.available === false ? ' · runtime not installed' : ''}</option>)}<option value="other">Other language…</option></select>{language === 'other' && <><label className="field-label" htmlFor="other-language">Language name</label><input id="other-language" className="form-input" placeholder="For example: lua" value={customLanguage} onChange={(event) => setCustomLanguage(event.target.value)}/><p className="form-help">Execution needs this runtime installed in the worker image. Rust, Ruby, and Julia are not supported.</p></>}
    {languages.some((item) => item.available) && <details className="code-disclosure"><summary>Available languages & libraries</summary>{languages.filter((item) => item.available).map((item) => <p className="form-help" key={item.id}><strong>{item.id}</strong> · {(item.libraries || item.modules)?.join(', ') || 'Standard library'}</p>)}</details>}
    {(projects.length > 0 || formDatasets.length > 0) && <details className="code-disclosure form-options" open={!!datasetID || undefined}><summary>Project & data options</summary><div className="form-columns"><div><label className="field-label" htmlFor="experiment-project">Project</label><select className="form-input" id="experiment-project" value={projectID} onChange={(event) => setProjectID(event.target.value)}><option value="">No project</option>{projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select></div><div><label className="field-label" htmlFor="experiment-dataset">Dataset</label><select className="form-input" id="experiment-dataset" value={datasetID} onChange={(event) => setDatasetID(event.target.value)}><option value="">No dataset</option>{formDatasets.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}</select></div></div></details>}
    <VoiceAgent showWakeActivation={false} api={api} available={settings?.voice_enabled} context={{ view: 'new_experiment', question, language: selectedLanguage, project: projects.find((item) => item.id === projectID)?.name || '', dataset: formDatasets.find((item) => item.id === datasetID)?.name || '' }} onQuestion={setQuestion} onDatasetSelected={dataset=>{setVoiceDatasets(current=>[...current.filter(d=>d.id!==dataset.id),dataset]);setDatasetID(dataset.id);}} onDocumentCreated={onDocumentCreated}/>
    {onAskKairo && <button type="button" className="chat-form-link" onClick={()=>onAskKairo(question)}>Not sure yet? Think it through with Kairo<ArrowRight size={13}/></button>}
    <p className="form-help">You’ll review the plan before anything runs.</p>
    <div className="modal-actions"><button className="button primary" disabled={!canSubmit} type="submit">{creating ? <LoaderCircle className="spinning" size={14}/> : <ArrowRight size={14}/>}{aiDisabled ? 'Save research question' : 'Create research plan'}</button></div>
  </form>;
}
