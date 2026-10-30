import React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import VoiceGreeting, { greeting } from './VoiceGreeting';

let speech;
beforeEach(() => {
  speech = { speak: vi.fn(), cancel: vi.fn(), getVoices: vi.fn(() => [{ lang: 'en-US', default: true, name: 'Test English' }]) };
  vi.stubGlobal('speechSynthesis', speech);
  vi.stubGlobal('SpeechSynthesisUtterance', class { constructor(text) { this.text = text; } });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe('microphone greeting', () => {
  it('speaks the requested greeting and supports replay after completion', async () => {
    const user = userEvent.setup(); render(<VoiceGreeting/>);
    expect(speech.speak).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Play Kairo greeting' }));
    expect(speech.speak).toHaveBeenCalledTimes(1);
    const utterance = speech.speak.mock.calls[0][0];
    expect(utterance.text).toBe("Hey there, I'm Kairo, your AI research companion.");
    expect(utterance.lang).toBe('en-US');
    expect(utterance.voice.name).toBe('Test English');
    expect(screen.getByRole('button', { name: 'Stop Kairo greeting' }).getAttribute('aria-pressed')).toBe('true');
    act(() => utterance.onend());
    await user.click(screen.getByRole('button', { name: 'Play Kairo greeting' }));
    expect(speech.speak).toHaveBeenCalledTimes(2);
    expect(speech.speak.mock.calls[1][0].text).toBe(greeting);
  });
  it('stops on a second click and ignores callbacks from a previous playback', async () => {
    const user = userEvent.setup(); render(<VoiceGreeting/>);
    await user.click(screen.getByRole('button', { name: 'Play Kairo greeting' }));
    const old = speech.speak.mock.calls[0][0];
    await user.click(screen.getByRole('button', { name: 'Stop Kairo greeting' }));
    expect(speech.cancel).toHaveBeenCalledTimes(2);
    await user.click(screen.getByRole('button', { name: 'Play Kairo greeting' }));
    act(() => { old.onend(); old.onerror({ error: 'not-allowed' }); });
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.getByRole('button', { name: 'Stop Kairo greeting' })).toBeTruthy();
  });
  it('handles unsupported browsers and playback errors', async () => {
    const user = userEvent.setup();
    vi.stubGlobal('speechSynthesis', undefined);
    const view = render(<VoiceGreeting/>);
    await user.click(screen.getByRole('button', { name: 'Play Kairo greeting' }));
    expect(screen.getByRole('alert').textContent).toContain('not available');
    view.unmount();
    vi.stubGlobal('speechSynthesis', speech);
    render(<VoiceGreeting/>);
    await user.click(screen.getByRole('button', { name: 'Play Kairo greeting' }));
    act(() => speech.speak.mock.calls[0][0].onerror({ error: 'audio-busy' }));
    expect(screen.getByRole('alert').textContent).toContain('could not start');
    expect(screen.getByRole('button', { name: 'Play Kairo greeting' })).toBeTruthy();
  });
  it('cancels playback when leaving the homepage', async () => {
    const user = userEvent.setup(); const view = render(<VoiceGreeting/>);
    await user.click(screen.getByRole('button', { name: 'Play Kairo greeting' }));
    view.unmount();
    expect(speech.cancel).toHaveBeenCalledTimes(2);
    expect(speech.speak.mock.calls[0][0].onend).toBeNull();
  });
});
