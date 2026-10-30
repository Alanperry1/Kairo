import React, { useEffect, useRef, useState } from 'react';
import { ArrowUpRight, Braces, ChevronLeft, ChevronRight, FileCode2, Orbit, Pause, Play, ShieldCheck, Sparkles } from 'lucide-react';

const SLIDE_INTERVAL = 5000;
const stages = [
  { id: 'design', label: 'Design', Icon: Sparkles, title: 'A method built around your question.', steps: [['Define the study', 'Your hypothesis, variables, and assumptions.'], ['Choose an approach', 'A simulation, analysis, benchmark, or custom computation.'], ['Review the plan', 'Inspect the method and code before anything runs.']] },
  { id: 'execute', label: 'Execute', Icon: Play, title: 'Your method, put into practice.', steps: [['Check the requirements', 'Make missing data and libraries explicit.'], ['Run your computation', 'Custom Python in an isolated worker.'], ['Keep a record', 'Track inputs, parameters, progress, and errors.']] },
  { id: 'evaluate', label: 'Evaluate', Icon: FileCode2, title: 'Evidence you can look into.', steps: [['Inspect the findings', 'Computed quantities and appropriate visualizations.'], ['Understand the limits', 'Review assumptions, uncertainty, and diagnostics.'], ['Take it with you', 'Download the code, inputs, results, and report.']] },
];

export default function WorkflowCarousel({ onStart }) {
  const [current, setCurrent] = useState(0);
  const [question, setQuestion] = useState('');
  const [playing, setPlaying] = useState(() => !window.matchMedia?.('(prefers-reduced-motion: reduce)')?.matches);
  const [hovered, setHovered] = useState(false);
  const [visible, setVisible] = useState(true);
  const [pageVisible, setPageVisible] = useState(document.visibilityState !== 'hidden');
  const container = useRef(null);
  const touch = useRef(null);

  function goTo(index) {
    setCurrent((index + stages.length) % stages.length);
    setPlaying(false);
  }

  useEffect(() => {
    const listener = () => setPageVisible(document.visibilityState !== 'hidden');
    document.addEventListener('visibilitychange', listener);
    return () => document.removeEventListener('visibilitychange', listener);
  }, []);
  useEffect(() => {
    const query = window.matchMedia?.('(prefers-reduced-motion: reduce)');
    const listener = (event) => { if (event.matches) setPlaying(false); };
    query?.addEventListener?.('change', listener);
    return () => query?.removeEventListener?.('change', listener);
  }, []);
  useEffect(() => {
    if (typeof IntersectionObserver === 'undefined') return;
    const observer = new IntersectionObserver(([entry]) => setVisible(entry.isIntersecting), { threshold: .25 });
    observer.observe(container.current);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (!playing || hovered || !visible || !pageVisible) return;
    const timer = setTimeout(() => setCurrent((index) => (index + 1) % stages.length), SLIDE_INTERVAL);
    return () => clearTimeout(timer);
  }, [current, playing, hovered, visible, pageVisible]);

  return <section ref={container} className="lp-carousel" id="explore" role="region" aria-roledescription="carousel" aria-label="Research workflow preview" onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)} onFocusCapture={(event) => { if (!event.target.closest('[data-carousel-play]')) setPlaying(false); }}>
    <div className="lp-example-tabs" role="tablist" aria-label="Explore the research workflow">{stages.map((item, index) => <button role="tab" aria-selected={current === index} aria-controls={`workflow-${item.id}`} id={`stage-${item.id}`} key={item.id} tabIndex={current === index ? 0 : -1} onKeyDown={(event) => {
      const next = event.key === 'ArrowRight' ? (index + 1) % stages.length : event.key === 'ArrowLeft' ? (index + stages.length - 1) % stages.length : event.key === 'Home' ? 0 : event.key === 'End' ? stages.length - 1 : -1;
      if (next >= 0) { event.preventDefault(); goTo(next); document.getElementById(`stage-${stages[next].id}`)?.focus(); }
    }} onClick={() => goTo(index)}><item.Icon size={15}/>{item.label}</button>)}</div>
    <div className="lp-carousel-viewport" onTouchStart={(event) => {
      if (event.target.closest('textarea, input, select, button, a')) { touch.current = null; return; }
      const point = event.touches[0];
      touch.current = { x: point.clientX, y: point.clientY };
    }} onTouchEnd={(event) => {
      if (!touch.current) return;
      const point = event.changedTouches[0];
      const dx = point.clientX - touch.current.x;
      const dy = point.clientY - touch.current.y;
      if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy) * 1.2) goTo(current + (dx < 0 ? 1 : -1));
      touch.current = null;
    }} onTouchCancel={() => { touch.current = null; }}>
      <div className="lp-carousel-track" style={{ transform: `translateX(calc(-${current * 100}% - ${current * 24}px))` }}>
        {stages.map((item, index) => <div className={`lp-preview lp-carousel-slide lp-workflow-${item.id}`} id={`workflow-${item.id}`} key={item.id} role="tabpanel" aria-labelledby={`stage-${item.id}`} aria-hidden={index !== current} inert={index !== current ? true : undefined}>
          <div className="lp-preview-top"><span><Orbit size={15}/>Kairo <span className="lp-preview-divider">/</span> Your experiment</span><span className="lp-preview-caption">Workflow preview</span></div>
          <div className="lp-card-stage"><span><item.Icon size={18}/>{item.label}</span><span>{String(index + 1).padStart(2, '0')} / 03</span></div>
          <div className="lp-preview-content">
          <div className="lp-preview-plan">
            {item.id === 'design' ? <><label className="lp-small-label" htmlFor="homepage-question">START WITH YOUR QUESTION</label><textarea id="homepage-question" className="lp-question-input" value={question} onChange={(event) => setQuestion(event.target.value)} maxLength={4000} rows={3} placeholder="What would you like to investigate?"/></> : <div className="lp-slide-question"><span className="lp-small-label">YOUR RESEARCH QUESTION</span><p>{question.trim() || 'A question worth investigating. A method shaped around it.'}</p><button type="button" onClick={() => { goTo(0); document.getElementById('homepage-question')?.focus(); }}>Edit question<ChevronLeft size={10}/></button></div>}
            <div className="lp-general-steps">{item.steps.map(([title], step) => <div className="lp-plan-step" key={title}><span className="lp-step-number">{step + 1}</span><div><strong>{title}</strong></div></div>)}</div>
            <a className="lp-text-link" href="/app?new=1" onClick={() => onStart(question)}>{question.trim() ? 'Plan my experiment' : 'Start a custom experiment'}<ArrowUpRight size={14}/></a>
          </div>
          <div className="lp-preview-result lp-general-preview"><h3>{item.title}</h3>
            {item.id === 'design' && <><dl className="lp-plan-outline"><div><dt>Question</dt><dd>{question.trim() || 'Defined by you'}</dd></div><div><dt>Domain</dt><dd>Shaped by your study</dd></div><div><dt>Method</dt><dd>AI-generated or manually specified</dd></div><div><dt>Code</dt><dd>Custom Python, open for review</dd></div></dl></>}
            {item.id === 'execute' && <><div className="lp-runtime-preview"><span><Braces size={16}/>Custom experiment interface</span><pre>{'def run_experiment(inputs, dataset_rows):\n    ...'}</pre><div><ShieldCheck size={13}/>Isolated Python worker</div></div></>}
            {item.id === 'evaluate' && <><div className="lp-output-preview">{[['experiment.py', 'Your experiment code'], ['input.json', 'Exact inputs and parameters'], ['results.json', 'Computed evidence'], ['report.md', 'Findings and limitations']].map(([name]) => <div key={name}><FileCode2 size={14}/><span>{name}</span></div>)}</div></>}
          </div>
          </div>
        </div>)}
      </div>
    </div>
    <div className="lp-carousel-controls"><button data-carousel-play className="lp-carousel-play" type="button" aria-label={playing ? 'Pause slideshow' : 'Play slideshow'} onClick={() => setPlaying((value) => !value)}>{playing ? <Pause size={12}/> : <Play size={12}/>}<span>{playing ? 'Auto-play' : 'Paused'}</span></button><span className="lp-carousel-position" aria-live={playing ? 'off' : 'polite'} aria-atomic="true"><strong>{String(current + 1).padStart(2, '0')}</strong><span>/</span>03</span><div className="lp-carousel-arrows"><button type="button" aria-label="Previous workflow card" onClick={() => goTo(current - 1)}><ChevronLeft size={16}/></button><button type="button" aria-label="Next workflow card" onClick={() => goTo(current + 1)}><ChevronRight size={16}/></button></div></div>
  </section>;
}
