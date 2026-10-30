import React from 'react';
import { afterEach, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import LiveTask, { draftPreview, partialDraftField } from './LiveTask';
afterEach(cleanup);
it('renders useful partial code without waiting for valid final JSON',()=>{
 const text='{"methodology":"Compare the methods","code":"def run_experiment(inputs, rows):\\n    value = 1';
 expect(partialDraftField(text,'code')).toBe('def run_experiment(inputs, rows):\n    value = 1');expect(draftPreview(text,'plan')).toContain('value = 1');
 render(<LiveTask kind="plan" task={{phase:'working',text,events:[{at:1,message:'Writing experiment code'}]}}/>);expect(screen.getByText('Writing experiment code')).toBeTruthy();expect(screen.getByText(/def run_experiment/)).toBeTruthy();expect(screen.getByText('Draft preview · validated before saving')).toBeTruthy();
});
it('handles incomplete escapes and extracts document sections',()=>{
 expect(partialDraftField('{"message":"Hello\\u03','message')).toBe('Hello');expect(partialDraftField('{"message":"Hello\\nworld"}','message')).toBe('Hello\nworld');expect(draftPreview('{"title":"A study","abstract":"The measured values','document')).toContain('ABSTRACT\nThe measured values');
});
