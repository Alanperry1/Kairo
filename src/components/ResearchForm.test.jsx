import React from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ResearchForm from './ResearchForm';
vi.mock('./VoiceAgent', () => ({ default: () => <button aria-label="Open voice panel"/> }));
afterEach(cleanup);
function setup() {
 const submit=vi.fn();const api={languages:vi.fn().mockResolvedValue({data:[{id:'python',available:true,modules:['numpy','pandas','sklearn']},{id:'r',available:true,modules:['jsonlite','data.table']},{id:'julia',available:false,modules:[]}]})};
 render(<ResearchForm api={api} settings={{model_provider:'google'}} projects={[]} datasets={[]} initialQuestion="How does the treatment affect growth?" onSubmit={submit}/>);return {submit,api};
}
it('defaults to automatic language choice and submits the question',async()=>{
 const {submit}=setup();expect(screen.getByLabelText('Experiment language').value).toBe('auto');await userEvent.click(screen.getByRole('button',{name:'Create research plan'}));expect(submit).toHaveBeenCalledWith({question:'How does the treatment affect growth?',project_id:'',dataset_id:''});
});
it('sends the selected runtime and shows installed scientific packages',async()=>{
 const {submit}=setup();await screen.findByRole('option',{name:'R'});await userEvent.selectOptions(screen.getByLabelText('Experiment language'),'r');await userEvent.click(screen.getByRole('button',{name:'Create research plan'}));expect(submit.mock.calls[0][0].language).toBe('r');expect(screen.getByText(/numpy, pandas, sklearn/)).toBeTruthy();expect(screen.queryByRole('option',{name:/Julia/})).toBeNull();
});
it('accepts another language name without losing the input while typing',async()=>{
 const {submit}=setup();await userEvent.selectOptions(screen.getByLabelText('Experiment language'),'other');await userEvent.type(screen.getByLabelText('Language name'),'lua');await userEvent.click(screen.getByRole('button',{name:'Create research plan'}));expect(submit.mock.calls[0][0].language).toBe('lua');
});

it('prevents reintroducing a removed runtime through Other language',async()=>{
 const {submit}=setup();await userEvent.selectOptions(screen.getByLabelText('Experiment language'),'other');await userEvent.type(screen.getByLabelText('Language name'),'ruby');
 expect(screen.getByRole('button',{name:'Create research plan'}).disabled).toBe(true);expect(submit).not.toHaveBeenCalled();
});
