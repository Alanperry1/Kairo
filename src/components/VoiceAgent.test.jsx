import React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import VoiceAgent from './VoiceAgent';
import { hasWakePhrase } from '../useWakePhrase';
import { VoiceActivationProvider } from '../voice-activation';

vi.mock('./VoiceConversation', () => ({ default: ({ autoStart }) => <div data-testid="voice-session" data-autostart={String(autoStart)}>Voice panel ready</div> }));
let instances;
let visibility;
class Recognition {
  constructor() { instances.push(this); }
  start = vi.fn(() => this.onstart?.());
  abort = vi.fn();
  result(text, isFinal = true) {
    this.onresult?.({ resultIndex: 0, results: [{ 0: { transcript: text }, isFinal, length: 1 }] });
  }
}
beforeEach(() => {
  instances = []; visibility = 'visible';
  vi.stubGlobal('SpeechRecognition', Recognition);
  vi.stubGlobal('webkitSpeechRecognition', undefined);
  vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility);
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });
const props = { api: {}, available: true, context: { view: 'experiments' } };

describe('Hey Kairo activation', () => {
  it('matches the wake phrase and spelling variants without matching ordinary words', () => {
    for (const phrase of ['Hey Kairo', 'hey kario!', 'HEY, CAIRO.', 'Hey Kyro can you help?']) expect(hasWakePhrase(phrase)).toBe(true);
    for (const phrase of ['Kairo', 'they Kairo', 'Hey Kairology', 'hello researcher']) expect(hasWakePhrase(phrase)).toBe(false);
  });
  it('listens only after opt-in and opens an automatically starting session once', async () => {
    const user = userEvent.setup(); render(<VoiceAgent {...props}/>);
    expect(instances).toHaveLength(0);
    await user.click(screen.getByRole('switch', { name: 'Listen for Hey Kairo' }));
    expect(instances).toHaveLength(1);
    const recognition = instances[0];
    expect(recognition.continuous).toBe(true);
    act(() => recognition.result('ordinary speech'));
    expect(screen.queryByTestId('voice-session')).toBeNull();
    act(() => recognition.result('Hey Kairo', false));
    expect(screen.queryByTestId('voice-session')).toBeNull();
    act(() => recognition.result('Hey Kario'));
    expect((await screen.findByTestId('voice-session')).getAttribute('data-autostart')).toBe('true');
    expect(recognition.abort).toHaveBeenCalled();
    expect(instances).toHaveLength(1);
    await user.click(screen.getByRole('button', { name: 'Close voice panel' }));
    await waitFor(() => expect(instances).toHaveLength(2));
    expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('true');
    await user.click(screen.getByRole('button', { name: 'Talk to Kairo' }));
    expect((await screen.findByTestId('voice-session')).getAttribute('data-autostart')).toBe('false');
  });
  it('stops recognition when hidden, resumes when visible, and cleans up on unmount', async () => {
    const user = userEvent.setup(); const { unmount } = render(<VoiceAgent {...props}/>);
    await user.click(screen.getByRole('switch'));
    act(() => { visibility = 'hidden'; fireEvent(document, new Event('visibilitychange')); });
    expect(instances[0].abort).toHaveBeenCalled();
    act(() => instances[0].result('Hey Kairo'));
    expect(screen.queryByTestId('voice-session')).toBeNull();
    act(() => { visibility = 'visible'; fireEvent(document, new Event('visibilitychange')); });
    expect(instances).toHaveLength(2);
    unmount(); expect(instances[1].abort).toHaveBeenCalled();
  });
  it('turns off after denied microphone permission and keeps manual voice available', async () => {
    const user = userEvent.setup(); render(<VoiceAgent {...props}/>);
    await user.click(screen.getByRole('switch'));
    act(() => instances[0].onerror({ error: 'not-allowed' }));
    expect(screen.getByRole('alert').textContent).toContain('Allow microphone access');
    expect(screen.getByRole('switch').getAttribute('aria-checked')).toBe('false');
    expect(instances[0].abort).toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Talk to Kairo' })).toBeTruthy();
  });
  it('restarts after silence but cancels pending restarts when switched off', () => {
    vi.useFakeTimers(); render(<VoiceAgent {...props}/>);
    fireEvent.click(screen.getByRole('switch'));
    act(() => instances[0].onend());
    act(() => vi.advanceTimersByTime(500));
    expect(instances[0].start).toHaveBeenCalledTimes(2);
    act(() => instances[0].onend());
    fireEvent.click(screen.getByRole('switch'));
    act(() => vi.advanceTimersByTime(1000));
    expect(instances[0].start).toHaveBeenCalledTimes(2);
  });
  it('keeps listening in experiment views even when the wake controls are hidden', async () => {
    const view = (key, showWakeActivation) => <VoiceActivationProvider><VoiceAgent key={key} {...props} showWakeActivation={showWakeActivation}/></VoiceActivationProvider>;
    const { rerender } = render(view('list', true));
    fireEvent.click(screen.getByRole('switch'));
    rerender(view('detail', false));
    expect(instances[0].abort).toHaveBeenCalled();
    expect(instances).toHaveLength(2);
    expect(instances[1].start).toHaveBeenCalledOnce();
    expect(screen.queryByRole('switch')).toBeNull();
    expect(screen.queryByText('Say “Hey Kairo” to open voice')).toBeNull();
    act(() => instances[1].result('Hey Kairo'));
    expect((await screen.findByTestId('voice-session')).getAttribute('data-autostart')).toBe('true');
  });
  it('offers manual activation when browser speech recognition is unavailable', () => {
    vi.stubGlobal('SpeechRecognition', undefined);
    render(<VoiceAgent {...props}/>);
    expect(screen.queryByRole('switch')).toBeNull();
    expect(screen.getByText(/Wake phrases aren’t supported/)).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Talk to Kairo' })).toBeTruthy();
  });
});
