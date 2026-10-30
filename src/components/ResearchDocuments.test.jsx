import React from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ResearchDocuments, { DocumentActions, DocumentViewer } from './ResearchDocuments';
afterEach(cleanup);
const document = { id: 'doc_saved', experiment_id: 'exp_done', kind: 'manuscript', title: 'Population study', content: '\\documentclass{article}', source: 'model', model: 'gemma', attempt: 1, plan_version: 2, created_at: new Date().toISOString() };
it('generates and opens a saved LaTeX manuscript from a completed experiment', async () => {
  const api = { documents: vi.fn().mockResolvedValue({ data: [] }), listExperiments: vi.fn().mockResolvedValue({ data: [{ id: 'exp_done', title: 'Growth study' }] }), generateDocument: vi.fn().mockResolvedValue(document) };
  const open = vi.fn(); render(<ResearchDocuments api={api} kind="manuscript" onOpenDocument={open}/>);
  await screen.findByRole('option', { name: 'Growth study' });
  await userEvent.click(screen.getByRole('button', { name: 'Generate manuscript' }));
  expect(api.generateDocument).toHaveBeenCalledWith('exp_done', 'manuscript', expect.any(AbortSignal));
  expect(open).toHaveBeenCalledWith(document);
  expect(await screen.findByRole('button', { name: /Population study/ })).toBeTruthy();
});
it('shows generation failures and does not open an unsaved document', async () => {
  const open = vi.fn(); const api = { generateDocument: vi.fn().mockRejectedValue(new Error('Model unavailable')) };
  render(<DocumentActions api={api} experiment={{ id: 'exp_done', status: 'completed', result: {} }} onGenerated={open}/>);
  await userEvent.click(screen.getByRole('button', { name: 'Generate report' }));
  expect((await screen.findByRole('alert')).textContent).toContain('Model unavailable'); expect(open).not.toHaveBeenCalled();
});
it('loads saved source and preserves the run provenance', async () => {
  const api = { getDocument: vi.fn().mockResolvedValue(document) }; const open = vi.fn();
  render(<DocumentViewer api={api} initialDocument={{ id: document.id }} onOpenExperiment={open}/>);
  expect((await screen.findByLabelText('LaTeX manuscript source')).textContent).toContain('documentclass');
  expect(screen.getByText('Attempt 1 · Plan v2')).toBeTruthy();
  await userEvent.click(screen.getByRole('button', { name: /Experiment/ })); expect(open).toHaveBeenCalledWith('exp_done');
  expect(screen.getByRole('button', { name: 'Download .tex' })).toBeTruthy();
});

it('previews a saved evidence figure and offers a complete source bundle', async () => {
  const create=vi.spyOn(URL,'createObjectURL').mockReturnValue('blob:test-figure');
  const revoke=vi.spyOn(URL,'revokeObjectURL').mockImplementation(()=>{});
  const figure={name:'figures/figure-01.png',pdf:'figures/figure-01.pdf',caption:'Recorded population values.',source:'results.json: series',renderer:'Matplotlib 3.10.1'};
  const api={downloadDocumentFigure:vi.fn().mockResolvedValue({blob:new Blob(['png'])})};
  const {unmount}=render(<DocumentViewer api={api} initialDocument={{...document,figures:[figure]}} onOpenExperiment={vi.fn()}/>);
  const img=await screen.findByRole('img',{name:'Figure 1: Recorded population values.'});expect(img.getAttribute('src')).toBe('blob:test-figure');
  expect(api.downloadDocumentFigure).toHaveBeenCalledWith(document.id,figure.name,expect.any(AbortSignal));expect(screen.getByRole('button',{name:'Source, figures & evidence ZIP'})).toBeTruthy();
  unmount();expect(revoke).toHaveBeenCalledWith('blob:test-figure');create.mockRestore();revoke.mockRestore();
});
it('lets a user request a text-only report', async () => {
 const open=vi.fn();const api={generateDocument:vi.fn().mockResolvedValue({...document,kind:'report'})};
 render(<DocumentActions api={api} experiment={{id:'exp_done',status:'completed',result:{}}} onGenerated={open}/>);
 await userEvent.click(screen.getByRole('checkbox',{name:'Include figures when useful'}));await userEvent.click(screen.getByRole('button',{name:'Generate report'}));
 expect(api.generateDocument).toHaveBeenCalledWith('exp_done','report',expect.any(AbortSignal),{include_figures:false});
});
