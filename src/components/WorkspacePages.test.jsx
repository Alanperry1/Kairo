import React from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { OverviewPage, ProjectsPage, ActivityPage } from './WorkspacePages';
afterEach(cleanup);
it('opens a project and its research navigation', async () => {
 const open = vi.fn(); const project = { id: 'p1', name: 'Population science' };
 render(<ProjectsPage projects={[project]} onOpen={open}/>);
 await userEvent.click(screen.getByRole('button', { name: /Population science/ })); expect(open).toHaveBeenCalledWith(project);
});
it('shows real overview counts and navigates to reports', async () => {
 const navigate=vi.fn();render(<OverviewPage dashboard={{ total_experiments:7,status_counts:{completed:3} }} projects={[]} datasets={[]} experiments={[]} onNavigate={navigate}/>);
 await userEvent.click(screen.getByRole('button',{name:/3 Completed/}));expect(navigate).toHaveBeenCalledWith('Reports');
});
it('loads document generation events and opens their experiment', async()=>{
 const open=vi.fn();const api={activity:vi.fn().mockResolvedValue({data:[{id:'event1',experiment_id:'exp_done',kind:'manuscript_generated',message:'Generated a saved manuscript draft',created_at:new Date().toISOString()}]})};
 render(<ActivityPage api={api} onOpenExperiment={open}/>);
 await userEvent.click(await screen.findByRole('button',{name:/Generated a saved manuscript draft/}));expect(open).toHaveBeenCalledWith('exp_done');
});
