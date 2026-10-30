import React from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ResearchChat, { conversationHistory } from './ResearchChat';
vi.mock('./VoiceAgent',()=>({default:()=> <button aria-label="Open voice panel"/>}));
afterEach(cleanup);
const followUp={message:'Let’s narrow down the goal.',follow_up:{prompt:'What do you want to compare?',options:['Prediction accuracy','Runtime','Both']}};
it('asks a contextual follow-up, sends the chosen option with history, and offers a reviewed draft',async()=>{
 const draft={question:'How do linear and tree models compare in accuracy on the supplied dataset?',language:'python'};
 const api={chat:vi.fn().mockResolvedValueOnce(followUp).mockResolvedValueOnce({message:'We can compare accuracy using held-out data.',draft})};const use=vi.fn();
 render(<ResearchChat api={api} settings={{model_provider:'google'}} onUseDraft={use}/>);
 await userEvent.type(screen.getByLabelText('Ask Kairo'),'Compare two prediction models');await userEvent.click(screen.getByRole('button',{name:'Send to Kairo'}));
 await userEvent.click(await screen.findByRole('button',{name:'Prediction accuracy'}));
 await screen.findByRole('button',{name:'Review experiment'});
 const history=api.chat.mock.calls[1][0].messages;expect(history.at(-1)).toEqual({role:'user',content:'Prediction accuracy'});expect(history[1].content).toContain('What do you want to compare?');
 expect(use).not.toHaveBeenCalled();await userEvent.click(screen.getByRole('button',{name:'Review experiment'}));expect(use).toHaveBeenCalledWith(draft);
});
it('accepts a custom answer instead of a suggested option',async()=>{
 const api={chat:vi.fn().mockResolvedValue(followUp)};render(<ResearchChat api={api} settings={{}}/>);
 await userEvent.click(screen.getByRole('button',{name:'Help me design an experiment'}));await screen.findByRole('button',{name:'Runtime'});
 await userEvent.type(screen.getByLabelText('Ask Kairo'),'I care about memory use');await userEvent.click(screen.getByRole('button',{name:'Send to Kairo'}));
 await waitFor(()=>expect(api.chat).toHaveBeenCalledTimes(2));expect(api.chat.mock.calls[1][0].messages.at(-1).content).toBe('I care about memory use');
});
it('keeps a failed message and retries it without duplicating the user turn',async()=>{
 const api={chat:vi.fn().mockRejectedValueOnce(new Error('Rate limited')).mockResolvedValueOnce(followUp)};
 render(<ResearchChat api={api} settings={{}} experiment={{id:'exp_done',title:'Growth'}}/>);
 await userEvent.type(screen.getByLabelText('Ask Kairo'),'What do my results mean?');await userEvent.click(screen.getByRole('button',{name:'Send to Kairo'}));
 expect((await screen.findByRole('alert')).textContent).toContain('Rate limited');await userEvent.click(screen.getByRole('button',{name:'Retry reply'}));await screen.findByRole('button',{name:'Both'});
 expect(api.chat.mock.calls[0][0]).toEqual(api.chat.mock.calls[1][0]);expect(api.chat.mock.calls[1][0].experiment_id).toBe('exp_done');expect(screen.getAllByText('What do my results mean?')).toHaveLength(1);
});
it('aborts a pending reply on reset and ignores its eventual result',async()=>{
 let resolve;const api={chat:vi.fn(()=>new Promise(done=>{resolve=done}))};render(<ResearchChat api={api} settings={{}}/>);
 await userEvent.click(screen.getByRole('button',{name:'Help me design an experiment'}));const signal=api.chat.mock.calls[0][1];await userEvent.click(screen.getByRole('button',{name:'New conversation'}));expect(signal.aborted).toBe(true);
 await act(async()=>resolve(followUp));expect(screen.queryByText(followUp.message)).toBeNull();expect(screen.getByText('What’s on your mind?')).toBeTruthy();
});
it('bounds the model context while preserving the newest answer',()=>{
 const turns=Array.from({length:40},(_,id)=>({id,role:id%2?'user':'assistant',content:'x'.repeat(2000)}));const history=conversationHistory(turns);expect(history.length).toBeLessThanOrEqual(24);expect(history.reduce((n,m)=>n+m.content.length,0)).toBeLessThanOrEqual(24000);expect(history.at(-1).role).toBe('user');
});

it('uses an existing dataset in the agent request and experiment draft',async()=>{
 const draft={question:'How does the recorded temperature vary over time?',language:'python'};const api={chat:vi.fn().mockResolvedValue({message:'We can analyze the supplied observations.',draft})};const use=vi.fn();
 render(<ResearchChat api={api} settings={{}} datasets={[{id:'ds_temperature',name:'temperature.csv'}]} onUseDraft={use}/>);
 await userEvent.selectOptions(screen.getByLabelText('Dataset for Kairo'),'ds_temperature');await userEvent.type(screen.getByLabelText('Ask Kairo'),'Use this dataset');await userEvent.click(screen.getByRole('button',{name:'Send to Kairo'}));await userEvent.click(await screen.findByRole('button',{name:'Review experiment'}));expect(api.chat.mock.calls[0][0].dataset_id).toBe('ds_temperature');expect(use).toHaveBeenCalledWith({...draft,dataset_id:'ds_temperature'});
});

it('shows a live reply before the final validated message arrives',async()=>{
 let finish;const api={chatStream:vi.fn((body,update)=>{update({event:'status',message:'Reading workspace evidence'});update({event:'model_start'});update({event:'delta',text:'{"message":"Here is the beginning'});return new Promise(resolve=>{finish=resolve});})};
 render(<ResearchChat api={api} settings={{}}/>);await userEvent.type(screen.getByLabelText('Ask Kairo'),'Explain my method');await userEvent.click(screen.getByRole('button',{name:'Send to Kairo'}));expect(await screen.findByText('Here is the beginning')).toBeTruthy();expect(screen.getByText('Reading workspace evidence')).toBeTruthy();
 await act(async()=>finish({message:'Here is the complete explanation.'}));expect(await screen.findByText('Here is the complete explanation.')).toBeTruthy();expect(screen.getByText('Task completed')).toBeTruthy();
});

it('opens trusted workspace sources attached to an answer',async()=>{
 const source={key:'manuscript:doc_saved',kind:'manuscript',id:'doc_saved',title:'Recorded population study',experiment_id:'exp_growth'};
 const api={chat:vi.fn().mockResolvedValue({message:'The model assumes a constant carrying capacity.',sources:[source]})};const open=vi.fn();
 render(<ResearchChat api={api} settings={{}} onOpenSource={open}/>);
 await userEvent.type(screen.getByLabelText('Ask Kairo'),'What are the assumptions in my paper?');await userEvent.click(screen.getByRole('button',{name:'Send to Kairo'}));
 const button=await screen.findByRole('button',{name:/Recorded population study/});expect(screen.getByText('Sources consulted')).toBeTruthy();await userEvent.click(button);expect(open).toHaveBeenCalledWith(source);
});
