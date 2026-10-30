import React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import WorkflowCarousel from './WorkflowCarousel';

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });
const selected = (name) => expect(screen.getByRole('tab', { name }).getAttribute('aria-selected')).toBe('true');

describe('sliding workflow cards', () => {
  it('advances automatically and supports pausing and manual navigation', () => {
    render(<WorkflowCarousel onStart={vi.fn()}/>);
    selected('Design');
    act(() => vi.advanceTimersByTime(5000));
    selected('Execute');
    fireEvent.click(screen.getByRole('button', { name: 'Pause slideshow' }));
    act(() => vi.advanceTimersByTime(10000));
    selected('Execute');
    fireEvent.click(screen.getByRole('button', { name: 'Next workflow card' }));
    selected('Evaluate');
    fireEvent.click(screen.getByRole('button', { name: 'Previous workflow card' }));
    selected('Execute');
  });
  it('pauses while hovered and while the question is being edited', () => {
    render(<WorkflowCarousel onStart={vi.fn()}/>);
    const carousel = screen.getByRole('region', { name: 'Research workflow preview' });
    fireEvent.mouseEnter(carousel);
    act(() => vi.advanceTimersByTime(10000));
    selected('Design');
    fireEvent.mouseLeave(carousel);
    act(() => vi.advanceTimersByTime(5000));
    selected('Execute');
    fireEvent.click(screen.getByRole('tab', { name: 'Design' }));
    fireEvent.focus(screen.getByLabelText('START WITH YOUR QUESTION'));
    act(() => vi.advanceTimersByTime(10000));
    selected('Design');
    expect(screen.getByRole('button', { name: 'Play slideshow' })).toBeTruthy();
  });
  it('supports swipes while preserving the question and workspace action', () => {
    const onStart = vi.fn(); render(<WorkflowCarousel onStart={onStart}/>);
    const question = 'How does population change over time?';
    fireEvent.change(screen.getByLabelText('START WITH YOUR QUESTION'), { target: { value: question } });
    const panel = screen.getByRole('tabpanel');
    fireEvent.touchStart(panel, { touches: [{ clientX: 260, clientY: 50 }] });
    fireEvent.touchEnd(panel, { changedTouches: [{ clientX: 60, clientY: 55 }] });
    selected('Execute');
    expect(screen.getByRole('tabpanel').textContent).toContain(question);
    const link = screen.getByRole('link', { name: 'Plan my experiment' });
    link.addEventListener('click', (event) => event.preventDefault());
    fireEvent.click(link);
    expect(onStart).toHaveBeenCalledWith(question);
    fireEvent.click(screen.getByRole('button', { name: 'Previous workflow card' }));
    expect(screen.getByLabelText('START WITH YOUR QUESTION').value).toBe(question);
  });
  it('starts paused when reduced motion is requested', () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
    render(<WorkflowCarousel onStart={vi.fn()}/>);
    act(() => vi.advanceTimersByTime(15000));
    selected('Design');
    expect(screen.getByRole('button', { name: 'Play slideshow' })).toBeTruthy();
  });
});
