import React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import VoiceConversation from './VoiceConversation';

const sdk = vi.hoisted(() => ({
  startSession: vi.fn(), endSession: vi.fn(), sendContextualUpdate: vi.fn(), sendUserMessage: vi.fn(), setMuted: vi.fn(),
  status: 'disconnected', isMuted: false, isSpeaking: false, tools: {}, callbacks: {},
}));
vi.mock('@elevenlabs/react', () => ({
  ConversationProvider: ({ children, ...callbacks }) => { sdk.callbacks = callbacks; return children; },
  useConversationControls: () => sdk,
  useConversationStatus: () => ({ status: sdk.status }),
  useConversationMode: () => ({ isSpeaking: sdk.isSpeaking }),
  useConversationInput: () => ({ isMuted: sdk.isMuted, setMuted: sdk.setMuted }),
  useConversationClientTool: (name, handler) => { sdk.tools[name] = handler; },
}));
let stopTrack;
beforeEach(() => {
  vi.clearAllMocks(); sdk.status = 'disconnected'; sdk.isMuted = false; sdk.isSpeaking = false; sdk.tools = {};
  sdk.setMuted.mockImplementation(() => {
    if (sdk.status !== 'connected') throw new Error('No active conversation. Call startSession() first.');
  });
  stopTrack = vi.fn();
  Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia: vi.fn().mockResolvedValue({ getTracks: () => [{ stop: stopTrack }] }) } });
});
afterEach(() => cleanup());
const sessionURL = 'wss://api.elevenlabs.io/v1/convai/conversation?token=temporary';
function client() { return { startVoiceSession: vi.fn().mockResolvedValue({ signed_url: sessionURL }) }; }

describe('ElevenLabs research conversations', () => {
  it('starts only after a click, releases the permission probe, and uses a temporary session URL', async () => {
    const api = client(); const user = userEvent.setup();
    const { unmount } = render(<VoiceConversation api={api} context={{ question: 'What changes growth?' }}/>);
    expect(api.startVoiceSession).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Start conversation' }));
    await waitFor(() => expect(sdk.startSession).toHaveBeenCalledWith({ signedUrl: sessionURL, connectionType: 'websocket' }));
    expect(navigator.mediaDevices.getUserMedia).toHaveBeenCalledWith({ audio: true });
    expect(stopTrack).toHaveBeenCalledOnce();
    expect(sdk.setMuted).not.toHaveBeenCalled();
    expect(screen.queryByRole('alert')).toBeNull();
    unmount(); expect(sdk.endSession).toHaveBeenCalled();
  });
  it('starts automatically for an opted-in wake activation without a second click', async () => {
    const api = client();
    const { rerender } = render(<VoiceConversation api={api} context={{ view: 'experiment' }} autoStart/>);
    await waitFor(() => expect(sdk.startSession).toHaveBeenCalledWith({ signedUrl: sessionURL, connectionType: 'websocket' }));
    rerender(<VoiceConversation api={api} context={{ view: 'experiment', status: 'planned' }} autoStart/>);
    expect(sdk.startSession).toHaveBeenCalledOnce();
    expect(sdk.setMuted).not.toHaveBeenCalled();
  });
  it('shares current context and prepares a question without silently changing or saving it', async () => {
    const onQuestion = vi.fn(); const user = userEvent.setup();
    render(<VoiceConversation api={client()} context={{ question: 'My original question', status: 'draft' }} onQuestion={onQuestion}/>);
    expect(JSON.parse(sdk.tools.get_experiment_context()).question).toBe('My original question');
    const question = 'How does carrying capacity affect population growth?';
    act(() => sdk.tools.set_research_question({ question }));
    expect(onQuestion).not.toHaveBeenCalled();
    expect(screen.getByText(question)).toBeTruthy();
    await user.click(screen.getByRole('button', { name: 'Use question' }));
    expect(onQuestion).toHaveBeenCalledWith(question);
    expect(JSON.parse(sdk.tools.set_research_question({ question: 'x' })).error).toBeTruthy();
  });
  it('shows transcripts, supports typed messages, mutes, and ends the conversation', async () => {
    sdk.status = 'connected'; const user = userEvent.setup();
    render(<VoiceConversation api={client()} context={{ view: 'experiment' }}/>);
    act(() => { sdk.callbacks.onMessage({ role: 'user', message: 'Explain the method' }); sdk.callbacks.onMessage({ role: 'agent', message: 'Let’s examine your variables.' }); });
    expect(screen.getByRole('list', { name: 'Conversation transcript' }).textContent).toContain('Explain the method');
    await user.type(screen.getByLabelText('Message Kairo'), 'What are the assumptions?');
    await user.click(screen.getByRole('button', { name: 'Send message to Kairo' }));
    expect(sdk.sendUserMessage).toHaveBeenCalledWith('What are the assumptions?');
    await user.click(screen.getByRole('button', { name: 'Mute microphone' })); expect(sdk.setMuted).toHaveBeenCalledWith(true);
    await user.click(screen.getByRole('button', { name: 'End conversation' })); expect(sdk.endSession).toHaveBeenCalled();
  });
  it('reports permission and backend failures without connecting', async () => {
    const api = client(); navigator.mediaDevices.getUserMedia.mockRejectedValueOnce(new DOMException('denied', 'NotAllowedError'));
    const user = userEvent.setup(); render(<VoiceConversation api={api} context={{}}/>);
    await user.click(screen.getByRole('button', { name: 'Start conversation' }));
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', expect.stringContaining('Allow microphone access'));
    expect(api.startVoiceSession).not.toHaveBeenCalled();
    api.startVoiceSession.mockRejectedValueOnce(new Error('Voice is busy right now.'));
    await user.click(screen.getByRole('button', { name: 'Start conversation' }));
    await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Voice is busy'));
    expect(sdk.startSession).not.toHaveBeenCalled();
  });
  it('ignores an in-flight session request after unmount', async () => {
    let resolve; const api = { startVoiceSession: vi.fn(() => new Promise((done) => { resolve = done; })) };
    const user = userEvent.setup(); const { unmount } = render(<VoiceConversation api={api} context={{}}/>);
    await user.click(screen.getByRole('button', { name: 'Start conversation' }));
    await waitFor(() => expect(api.startVoiceSession).toHaveBeenCalled());
    unmount(); await act(async () => resolve({ signed_url: sessionURL }));
    expect(sdk.startSession).not.toHaveBeenCalled();
  });
  it('generates a saved manuscript through the voice tool and opens the actual document', async () => {
    const document = { id: 'doc_saved', kind: 'manuscript', title: 'Growth evidence' };
    const api = { ...client(), generateDocument: vi.fn().mockResolvedValue(document) }; const open = vi.fn();
    render(<VoiceConversation api={api} context={{ id: 'exp_done', status: 'completed' }} onDocumentCreated={open}/>);
    let result; await act(async () => { result = JSON.parse(await sdk.tools.generate_research_document({ kind: 'manuscript' })); });
    expect(result.saved).toBe(true); expect(api.generateDocument).toHaveBeenCalledWith('exp_done', 'manuscript', expect.any(AbortSignal));
    await userEvent.click(screen.getByRole('button', { name: 'Open draft' })); expect(open).toHaveBeenCalledWith(document);
  });
  it('does not claim a saved document when generation fails', async () => {
    const api = { ...client(), generateDocument: vi.fn().mockRejectedValue(new Error('No completed evidence')) };
    render(<VoiceConversation api={api} context={{ id: 'exp_done' }}/>);
    let result; await act(async () => { result = JSON.parse(await sdk.tools.generate_research_document({ kind: 'report' })); });
    expect(result.saved).toBeUndefined(); expect(result.error).toContain('No completed evidence');
    expect(screen.queryByRole('button', { name: 'Open draft' })).toBeNull();
  });

  it('shares actual installed languages and libraries with the agent', async () => {
    const capabilities = {data:[{id:'r',available:true,modules:['jsonlite','data.table']}]};
    const api={...client(),languages:vi.fn().mockResolvedValue(capabilities)};
    render(<VoiceConversation api={api} context={{language:'auto'}}/>);
    let result;await act(async()=>{result=JSON.parse(await sdk.tools.get_research_capabilities());});
    expect(result).toEqual(capabilities);expect(api.languages).toHaveBeenCalledOnce();
  });

  it('lets the voice agent ask a question and sends a selected answer back once', async () => {
    sdk.status='connected';render(<VoiceConversation api={client()} context={{view:'experiment'}}/>);
    let result;act(()=>{result=JSON.parse(sdk.tools.ask_research_question({question:'Which outcome matters most?',options:['Accuracy','Runtime','Memory']}));});
    expect(result.awaiting_user_answer).toBe(true);expect(sdk.sendUserMessage).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button',{name:'Runtime'}));
    expect(sdk.sendUserMessage).toHaveBeenCalledWith('For your question "Which outcome matters most?", I choose: Runtime');
    expect(screen.getByRole('button',{name:'Runtime'}).disabled).toBe(true);
    expect(JSON.parse(sdk.tools.ask_research_question({question:'Bad options?',options:['Same','same']})).error).toBeTruthy();
  });

});

it('finds real datasets and offers workspace selection during a voice conversation',async()=>{
 sdk.status='connected';const api={startVoiceSession:vi.fn(),searchDatasets:vi.fn().mockResolvedValue({workspace:[{id:'ds_growth',name:'growth.csv'}],public:[]})};const select=vi.fn();
 render(<VoiceConversation api={api} context={{view:'new_experiment'}} onDatasetSelected={select}/>);let result;await act(async()=>{result=JSON.parse(await sdk.tools.find_datasets({query:'growth'}));});
 expect(result.requires_user_selection).toBe(true);expect(select).not.toHaveBeenCalled();await userEvent.click(screen.getByRole('button',{name:'Use dataset'}));expect(select).toHaveBeenCalledWith({id:'ds_growth',name:'growth.csv'});expect(sdk.sendUserMessage).toHaveBeenCalledWith('Use the workspace dataset growth.csv (ID: ds_growth).');
});

it('uses the streaming workspace knowledge service for voice questions',async()=>{
 const source={key:'experiment:exp_saved',kind:'experiment',id:'exp_saved',title:'Recorded study'};
 const api={...client(),chatStream:vi.fn(async(body,update)=>{update({event:'status',message:'Reading saved manuscript evidence'});update({event:'delta',text:'{"message":"The saved study reports 99.4"}'});return {message:'The saved study reports 99.4.',sources:[source]};})};
 render(<VoiceConversation api={api} context={{id:'exp_saved'}}/>);
 let result;await act(async()=>{result=JSON.parse(await sdk.tools.ask_workspace({question:'What did our last study find?'}));});
 expect(api.chatStream).toHaveBeenCalledWith({messages:[{role:'user',content:'What did our last study find?'}],experiment_id:'exp_saved'},expect.any(Function),expect.any(AbortSignal));expect(result.message).toContain('99.4');expect(result.sources).toEqual([source]);expect(screen.getByText('Reading saved manuscript evidence')).toBeTruthy();expect(screen.getByText('Task completed')).toBeTruthy();
});

it('lets voice generate a saved PDF and exposes its download',async()=>{
 const api={...client(),generateDocumentPDF:vi.fn(async(id,update)=>{update({event:'status',message:'Compiling saved LaTeX'});return {sha256:'abc'}})};
 render(<VoiceConversation api={api} context={{}}/>);let result;await act(async()=>{result=JSON.parse(await sdk.tools.generate_manuscript_pdf({document_id:'doc_saved'}))});expect(result.saved).toBe(true);expect(api.generateDocumentPDF).toHaveBeenCalledWith('doc_saved',expect.any(Function),expect.any(AbortSignal));expect(screen.getByRole('button',{name:'Download PDF'})).toBeTruthy();
});
