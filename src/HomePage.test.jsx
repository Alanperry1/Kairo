import React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import HomePage, { pendingQuestionKey } from './HomePage';
import Root from './Root';

vi.mock('./App', () => ({ default: () => <div>Connected research workspace</div> }));
afterEach(() => { cleanup(); sessionStorage.clear(); window.history.replaceState({}, '', '/'); });

describe('Kairo homepage', () => {
  it('provides a clear route into the existing workspace', () => {
    render(<HomePage/>);
    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('discovery');
    expect(screen.getByRole('link', { name: 'Start an experiment' }).getAttribute('href')).toBe('/app?new=1');
    expect(screen.getByRole('link', { name: 'Open workspace' }).getAttribute('href')).toBe('/app');
    expect(screen.getByRole('img', { name: /quiet observatory/ }).getAttribute('src')).toBe('/images/kairo-observatory.png');
  });
  it('explains a general research workflow rather than forcing a domain template', async () => {
    const user = userEvent.setup(); render(<HomePage/>);
    expect(screen.getByRole('tab', { name: 'Design' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.queryByRole('tab', { name: 'Physics' })).toBeNull();
    await user.click(screen.getByRole('tab', { name: 'Execute' }));
    expect(within(screen.getByRole('tabpanel')).getByText('Isolated Python worker')).toBeTruthy();
    await user.click(screen.getByRole('tab', { name: 'Evaluate' }));
    const panel = screen.getByRole('tabpanel');
    expect(within(panel).getByText('experiment.py')).toBeTruthy();
    expect(within(panel).getByText('Workflow preview')).toBeTruthy();
    await user.keyboard('{ArrowRight}');
    expect(screen.getByRole('tab', { name: 'Design' }).getAttribute('aria-selected')).toBe('true');
  });
  it('hands a custom question to the workspace without putting it in a URL', async () => {
    const user = userEvent.setup(); render(<HomePage/>);
    const question = 'How does carrying capacity affect population growth?';
    await user.type(screen.getByLabelText('START WITH YOUR QUESTION'), question);
    const link = screen.getByRole('link', { name: 'Plan my experiment' });
    expect(link.getAttribute('href')).toBe('/app?new=1');
    link.addEventListener('click', (event) => event.preventDefault());
    await user.click(link);
    expect(sessionStorage.getItem(pendingQuestionKey)).toBe(question);
  });
  it('expands FAQs and the compact navigation', async () => {
    const user = userEvent.setup(); render(<HomePage/>);
    const question = screen.getByText('Do I need an AI model to get started?');
    await user.click(question);
    expect(question.closest('details').open).toBe(true);
    const menu = screen.getByRole('button', { name: 'Toggle navigation' });
    await user.click(menu);
    expect(menu.getAttribute('aria-expanded')).toBe('true');
  });
  it('serves the landing page at the root', () => {
    window.history.replaceState({}, '', '/'); render(<Root/>);
    expect(screen.getByRole('heading', { level: 1 }).textContent).toContain('discovery');
    expect(screen.queryByText('Connected research workspace')).toBeNull();
  });
  it('preserves the dashboard route', async () => {
    window.history.replaceState({}, '', '/app'); render(<Root/>);
    expect(await screen.findByText('Connected research workspace')).toBeTruthy();
  });
});
