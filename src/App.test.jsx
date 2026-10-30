import React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import manualPlan from '../backend/examples/custom-plan.json';

afterEach(() => { cleanup(); sessionStorage.clear(); });
const now = new Date().toISOString();
function fakeAPI() {
  let experiments = [];
  const projects = [];
  const datasets = [];
  const settings = { model_provider: 'disabled', model_name: 'qwen3:8b', workers: 1, worker_mode: 'local', model_connectivity: 'not_checked' };
  return {
    baseURL: '/api/v1',
    listExperiments: vi.fn(async () => ({ data: structuredClone(experiments), total: experiments.length })),
    dashboard: vi.fn(async () => ({ total_experiments: experiments.length, reports: experiments.filter((e) => e.status === 'completed').length, status_counts: Object.fromEntries(['draft', 'running', 'planned', 'completed', 'queued', 'failed'].map((status) => [status, experiments.filter((e) => e.status === status).length])), activity: [{ date: '2026-10-29', experiments_created: experiments.length }] })),
    settings: vi.fn(async () => settings), projects: vi.fn(async () => ({ data: structuredClone(projects) })),
    datasets: vi.fn(async () => ({ data: structuredClone(datasets) })), tools: vi.fn(async () => ({ data: [] })), activity: vi.fn(async () => ({ data: [] })), reports: vi.fn(async () => ({ data: [] })),
    createExperiment: vi.fn(async (body) => { const e = { ...body, id: 'exp_live1234', status: 'draft', attempt: 0, progress: 0, plan_version: 0, created_at: now, updated_at: now }; experiments.push(e); return structuredClone(e); }),
    getExperiment: vi.fn(async (id) => structuredClone(experiments.find((e) => e.id === id))),
    savePlan: vi.fn(async (id, plan) => { const e = experiments.find((e) => e.id === id); Object.assign(e, { plan: { ...plan, source: 'manual' }, status: 'planned', plan_version: e.plan_version + 1 }); return structuredClone(e); }),
    generatePlan: vi.fn(),
    runExperiment: vi.fn(async (id) => { const e = experiments.find((e) => e.id === id); Object.assign(e, { status: 'queued', attempt: 1, progress: 10 }); return structuredClone(e); }),
    cancelExperiment: vi.fn(async (id) => { const e = experiments.find((e) => e.id === id); e.status = 'cancelled'; return structuredClone(e); }),
    events: vi.fn(async () => ({ data: [] })), artifacts: vi.fn(async () => ({ data: [] })), plans: vi.fn(async () => ({ data: [] })),
    createProject: vi.fn(async (body) => { const p = { ...body, id: 'prj_live' }; projects.push(p); return p; }),
    uploadDataset: vi.fn(async (file) => { const d = { id: 'ds_live', name: file.name, rows: 6, columns: ['group', 'value'], preview: [['A', '12']], size: file.size, sha256: 'a'.repeat(64) }; datasets.push(d); return d; }),
    deleteExperiment: vi.fn(async (id) => { experiments = experiments.filter((e) => e.id !== id); }),
  };
}

describe('connected dashboard', () => {
  it('shows real empty state without seeding local demo experiments', async () => {
    localStorage.setItem('researchos-runs', JSON.stringify([{ id: 'EXP-024', title: 'Old demo' }]));
    render(<App client={fakeAPI()}/>);
    await screen.findAllByText('API connected');
    expect(screen.queryByText('Old demo')).toBeNull();
    expect(screen.getByText('Your next discovery starts here')).toBeTruthy();
    expect(screen.queryByText('Comparing optimization algorithms')).toBeNull();
  });
  it('saves a manual plan, explicitly queues execution, and cancels through the API', async () => {
    const api = fakeAPI(); const user = userEvent.setup();
    render(<App client={api}/>); await screen.findAllByText('API connected');
    await user.click(screen.getByRole('button', { name: 'New experiment' }));
    await user.type(screen.getByLabelText('Research question'), manualPlan.research_question);
    await user.click(screen.getByRole('button', { name: 'Save research question' }));
    const dialog = await screen.findByRole('dialog', { name: 'Experiment' });
    await user.click(await within(dialog).findByRole('button', { name: 'Paste a plan' }));
    fireEvent.change(within(dialog).getByLabelText('Experiment plan JSON'), { target: { value: JSON.stringify(manualPlan) } });
    await user.click(within(dialog).getByRole('button', { name: 'Save plan' }));
    await waitFor(() => expect(within(dialog).queryByRole('button', { name: 'Run experiment' })).toBeTruthy());
    expect(api.createExperiment).toHaveBeenCalledWith({ question: manualPlan.research_question, project_id: '', dataset_id: '' });
    expect(api.savePlan).toHaveBeenCalledWith('exp_live1234', manualPlan);
    expect(api.runExperiment).not.toHaveBeenCalled();
    await user.click(within(dialog).getByRole('button', { name: 'Run experiment' }));
    await waitFor(() => expect(api.runExperiment).toHaveBeenCalledWith('exp_live1234'));
    await user.click(await within(dialog).findByRole('button', { name: 'Cancel run' }));
    await waitFor(() => expect(api.cancelExperiment).toHaveBeenCalledWith('exp_live1234'));
    await within(dialog).findByText('Cancelled');
  });
  it('creates projects and sends CSV files to the backend', async () => {
    const api = fakeAPI(); const user = userEvent.setup(); render(<App client={api}/>);
    await screen.findAllByText('API connected');
    await user.click(screen.getByRole('button', { name: 'New project' }));
    await user.type(screen.getByLabelText('Project name'), 'Flight lab');
    await user.click(screen.getByRole('button', { name: 'Create project' }));
    await waitFor(() => expect(api.createProject).toHaveBeenCalledWith({ name: 'Flight lab', description: '' }));
    await user.click(screen.getByRole('button', { name: 'Datasets', exact: true }));
    const file = new File(['group,value\nA,12\nB,14'], 'measurements.csv', { type: 'text/csv' });
    await user.upload(screen.getByLabelText('Upload CSV dataset'), file);
    await waitFor(() => expect(api.uploadDataset).toHaveBeenCalledWith(file));
    expect(await screen.findByRole('button', { name: 'Preview measurements.csv' })).toBeTruthy();
  });
  it('does not offer templates through legacy template links', async () => {
    window.history.replaceState({}, '', '/app?template=sorting');
    try {
      const api = fakeAPI(); const user = userEvent.setup(); render(<App client={api}/>);
      await screen.findAllByText('API connected');
      expect(screen.queryByRole('dialog')).toBeNull();
      await user.click(screen.getByRole('button', { name: 'New experiment' }));
      expect(screen.queryByRole('button', { name: 'Example templates' })).toBeNull();
      expect(screen.queryByLabelText('Experiment method')).toBeNull();
      expect(screen.getByLabelText('Research question').value).toBe('');
      expect(api.savePlan).not.toHaveBeenCalled();
    } finally { window.history.replaceState({}, '', '/'); }
  });
  it('accepts an arbitrary question instead of forcing a template', async () => {
    const api = fakeAPI(); const user = userEvent.setup(); render(<App client={api}/>);
    await screen.findAllByText('API connected');
    await user.click(screen.getByRole('button', { name: 'New experiment' }));
    await user.type(screen.getByLabelText('Research question'), 'How does carrying capacity affect population growth?');
    await user.click(screen.getByRole('button', { name: 'Save research question' }));
    await waitFor(() => expect(api.createExperiment).toHaveBeenCalledWith({question:'How does carrying capacity affect population growth?',project_id:'',dataset_id:''}));
    expect(api.savePlan).not.toHaveBeenCalled();
    expect(api.generatePlan).not.toHaveBeenCalled();
    expect(await screen.findByRole('dialog', {name:'Experiment'})).toBeTruthy();
  });
  it('prefills the homepage question in a custom experiment', async () => {
    const question = 'How does a change in interest rate affect this mathematical model?';
    window.history.replaceState({}, '', '/app?new=1');
    sessionStorage.setItem('kairo-pending-question', question);
    try {
      const api = fakeAPI(); const user = userEvent.setup(); render(<App client={api}/>);
      await screen.findAllByText('API connected');
      expect(screen.getByLabelText('Research question').value).toBe(question);
      expect(sessionStorage.getItem('kairo-pending-question')).toBeNull();
      await user.click(screen.getByRole('button', { name: 'Save research question' }));
      await waitFor(() => expect(api.createExperiment).toHaveBeenCalledWith({ question, project_id: '', dataset_id: '' }));
      expect(api.savePlan).not.toHaveBeenCalled();
    } finally { window.history.replaceState({}, '', '/'); }
  });
  it('displays API failures instead of using demo data', async () => {
    const api = fakeAPI(); api.listExperiments.mockRejectedValue(new Error('Backend unreachable'));
    render(<App client={api}/>);
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', expect.stringContaining('Backend unreachable'));
    expect(screen.getAllByText('Connection interrupted').length).toBeGreaterThan(0);
  });
  it('sends status and search filters to the server', async () => {
    const api = fakeAPI(); render(<App client={api}/>); await screen.findAllByText('API connected');
    fireEvent.change(screen.getByLabelText('Filter experiments'), { target: { value: 'projectile' } });
    fireEvent.change(screen.getByLabelText('Filter by experiment status'), { target: { value: 'failed' } });
    await waitFor(() => expect(api.listExperiments).toHaveBeenCalledWith(expect.objectContaining({ q: 'projectile', status: 'failed' }), expect.any(AbortSignal)));
  });
});
