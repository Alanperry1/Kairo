import React, { useState } from 'react';
import { ArrowRight, LoaderCircle } from 'lucide-react';
import VoiceAgent from './VoiceAgent';

export default function ResearchForm({ api, settings, projects, datasets, initialQuestion = '', onSubmit, creating }) {
  const [question, setQuestion] = useState(initialQuestion);
  const [projectID, setProjectID] = useState('');
  const [datasetID, setDatasetID] = useState('');
  const aiDisabled = settings?.model_provider === 'disabled';
  const canSubmit = settings && !creating && question.trim().length >= 5;
  return <form onSubmit={(event) => {
    event.preventDefault();
    if (!canSubmit) return;
    onSubmit({ question: question.trim(), project_id: projectID, dataset_id: datasetID });
  }}>
      <label className="field-label" htmlFor="new-research-question">Research question</label>
      <textarea id="new-research-question" data-autofocus className="modal-textarea" required minLength={5} maxLength={4000} value={question} onChange={(event) => setQuestion(event.target.value)} placeholder="What would you like to investigate? Describe your question, data, and constraints."/>
      {aiDisabled ? <div className="inline-notice">Save any research question now. Connect an AI model to generate its method and code, or add a manual plan after saving.</div> : <p className="form-help">The model designs a method and Python experiment for your question. Review both before running. Execution depends on available data and worker libraries.</p>}
    {(projects.length > 0 || datasets.length > 0) && <details className="code-disclosure form-options"><summary>Project & data options</summary><div className="form-columns"><div><label className="field-label" htmlFor="experiment-project">Project</label><select className="form-input" id="experiment-project" value={projectID} onChange={(event) => setProjectID(event.target.value)}><option value="">No project</option>{projects.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</select></div><div><label className="field-label" htmlFor="experiment-dataset">Dataset</label><select className="form-input" id="experiment-dataset" value={datasetID} onChange={(event) => setDatasetID(event.target.value)}><option value="">No dataset</option>{datasets.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}</select></div></div></details>}
    <VoiceAgent api={api} available={settings?.voice_enabled} context={{ view: 'new_experiment', question, project: projects.find((item) => item.id === projectID)?.name || '', dataset: datasets.find((item) => item.id === datasetID)?.name || '' }} onQuestion={setQuestion}/>
    <p className="form-help">You’ll review the plan before anything runs.</p>
    <div className="modal-actions"><button className="button primary" disabled={!canSubmit} type="submit">{creating ? <LoaderCircle className="spinning" size={14}/> : <ArrowRight size={14}/>}{aiDisabled ? 'Save research question' : 'Create research plan'}</button></div>
  </form>;
}
