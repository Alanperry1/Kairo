// Run the Go API and Vite separately. This checks the actual same-origin proxy,
// including browser-like Origin headers, without requiring a live AI model.
import assert from 'node:assert/strict';
import { createAPI } from '../src/api.js';
import { readFile } from 'node:fs/promises';

const baseURL = process.env.INTEGRATION_API_URL || 'http://127.0.0.1:5173/api/v1';
const origin = new URL(baseURL).origin;
const api = createAPI({
  baseURL,
  getToken: () => process.env.API_TOKEN || '',
  fetcher: (url, options) => fetch(url, { ...options, headers: { ...options.headers, Origin: origin } }),
});
const plan = JSON.parse(await readFile(new URL('../backend/examples/custom-plan.json', import.meta.url), 'utf8'));
let experiment;
try {
  const initial = await api.dashboard();
  const project = await api.createProject({ name: `Integration ${Date.now()}`, description: 'Frontend-to-Go proxy verification' });
  const file = new File(['group,value\nA,12\nA,15\nA,13\nB,17\nB,19\nB,16\n'], 'integration-groups.csv', { type: 'text/csv' });
  const dataset = await api.uploadDataset(file);
  assert.equal(dataset.rows, 6);
  assert.deepEqual(dataset.columns, ['group', 'value']);
  const downloaded = await api.downloadDataset(dataset.id);
  assert.match(await downloaded.blob.text(), /A,12/);
  experiment = await api.createExperiment({ question: plan.research_question, project_id: project.id, dataset_id: '' });
  assert.equal(experiment.status, 'draft');
  experiment = await api.savePlan(experiment.id, plan);
  assert.equal(experiment.status, 'planned');
  assert.equal(experiment.plan.source, 'manual');
  const filtered = await api.listExperiments({ project_id: project.id, q: 'carrying capacity' });
  assert.equal(filtered.total, 1);
  await api.runExperiment(experiment.id);
  const deadline = Date.now() + 60000;
  do {
    experiment = await api.getExperiment(experiment.id);
    if (['completed', 'failed', 'cancelled'].includes(experiment.status)) break;
    await new Promise((resolve) => setTimeout(resolve, 100));
  } while (Date.now() < deadline);
  assert.equal(experiment.status, 'completed', experiment.error);
  assert.equal(experiment.result.kind, 'simulation');
  const result = experiment.result.metrics;
  assert.equal(result['K=100'].initial_condition_checked, true);
  assert.equal(result['K=200'].initial_condition_checked, true);
  assert.ok(result['K=200'].final_population > result['K=100'].final_population);
  const reports = await api.reports();
  assert.ok(reports.data.some((r) => r.experiment_id === experiment.id));
  const report = await api.downloadReport(experiment.id);
  assert.match(await report.blob.text(), /Computed findings/);
  const artifacts = await api.artifacts(experiment.id);
  assert.ok(artifacts.data.some((a) => a.name === 'attempt-001/chart.svg'));
  const artifact = await api.downloadArtifact(experiment.id, 'attempt-001/results.json');
  assert.equal(JSON.parse(await artifact.blob.text()).kind, 'simulation');
  const bundle = await api.downloadBundle(experiment.id);
  assert.equal((await bundle.blob.arrayBuffer()).byteLength > 1000, true);
  const events = await api.events(experiment.id);
  assert.ok(events.data.some((e) => e.kind === 'completed'));
  const dashboard = await api.dashboard();
  assert.equal(dashboard.total_experiments, initial.total_experiments + 1);
  const foreign = await fetch(`${baseURL}/settings`, { headers: { Origin: 'https://untrusted.example', ...(process.env.API_TOKEN ? { Authorization: `Bearer ${process.env.API_TOKEN}` } : {}) } });
  assert.equal(foreign.status, 403, 'Foreign origins must not be normalized by the Vite proxy');
  await api.deleteExperiment(experiment.id);
  experiment = null;
  console.log('PASS: proxy, same-origin POST, foreign-origin rejection, projects, CSV uploads/downloads, custom plan review, isolated execution, live results, filtering, reports, artifacts, ZIP export, activity, counts, and deletion.');
  console.log(`Created project ${project.id} and dataset ${dataset.id} remain available in this test workspace.`);
} finally {
  if (experiment && !['running', 'queued', 'planning'].includes(experiment.status)) await api.deleteExperiment(experiment.id).catch(() => {});
}
