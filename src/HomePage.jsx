import React, { useState } from 'react';
import { ArrowDown, ArrowRight, ArrowUpRight, Atom, Check, ChevronDown, ChartNoAxesCombined, Code2, Dna, FileCode2, FlaskConical, Orbit, Play, TrendingUp, AudioLines } from 'lucide-react';
import './home.css';
import WorkflowCarousel from './components/WorkflowCarousel';
import VoiceGreeting from './components/VoiceGreeting';

export const pendingQuestionKey = 'kairo-pending-question';
function rememberQuestion(question) {
  try {
    if (question.trim()) sessionStorage.setItem(pendingQuestionKey, question.trim());
    else sessionStorage.removeItem(pendingQuestionKey);
  } catch { /* the workspace still opens if browser storage is unavailable */ }
}

export default function HomePage() {
  const [menuOpen, setMenuOpen] = useState(false);
  return <div className="landing-page">
    <a className="lp-skip" href="#main-content">Skip to content</a>
    <section className="lp-hero">
      <img className="lp-hero-art" src="/images/kairo-observatory.png" alt="A quiet observatory above a mountain lake, beneath a wide morning sky" fetchPriority="high"/>
      <div className="lp-hero-wash" aria-hidden="true"/>
      <header className="lp-header"><a className="lp-brand" href="/" aria-label="Kairo home"><Orbit size={25} strokeWidth={1.5}/><span>Kairo</span></a><nav className={menuOpen ? 'lp-nav open' : 'lp-nav'} aria-label="Main navigation"><a href="#platform" onClick={() => setMenuOpen(false)}>Platform</a><a href="#workflow" onClick={() => setMenuOpen(false)}>How it works</a><a href="#voice" onClick={() => setMenuOpen(false)}>Voice agents</a><a href="#explore" onClick={() => setMenuOpen(false)}>Explore</a></nav><a className="lp-nav-cta" href="/app">Open workspace<ArrowUpRight size={14}/></a><button className="lp-menu" aria-label="Toggle navigation" aria-expanded={menuOpen} onClick={() => setMenuOpen((open) => !open)}><ChevronDown size={19}/></button></header>
      <div className="lp-hero-copy" id="main-content"><h1>Make room<br/>for <em>discovery.</em></h1><p>Your question. Your method. Your experiment.<br/>Design, run, and reproduce computational research<br className="lp-desktop-break"/> with open-weight AI.</p><div className="lp-hero-actions"><a className="lp-button lp-button-dark" href="/app?new=1" onClick={() => rememberQuestion('')}>Start an experiment<ArrowRight size={16}/></a><a className="lp-quiet-link" href="#workflow">See how it works<ArrowDown size={13}/></a></div></div>
    </section>
    <main className="lp-main">
      <section className="lp-tools lp-domains" aria-label="Scientific domains"><div>{[[Atom, 'Physics'], [FlaskConical, 'Chemistry'], [Dna, 'Biology'], [ChartNoAxesCombined, 'Statistics'], [Code2, 'Computer Science'], [TrendingUp, 'Economics']].map(([Icon, name]) => <span key={name}><Icon size={20} strokeWidth={1.4}/>{name}</span>)}</div></section>
      <section className="lp-platform lp-section" id="platform"><div className="lp-section-intro"><div><h2>Less setup.<br/>More <em>what if.</em></h2></div></div><WorkflowCarousel onStart={rememberQuestion}/></section>
      <section className="lp-workflow lp-section" id="workflow"><div className="lp-workflow-heading"><h2>A thoughtful process.<br/>A clearer path forward.</h2></div><div className="lp-workflow-grid">{[
        [FlaskConical, '01', 'Ask something worth testing.', 'Start with your own research question, hypothesis, data, and constraints. The method follows your study.'],
        [Play, '02', 'Give your idea a fair test.', 'Review the method, assumptions, and custom Python code. Run it in an isolated worker with recorded inputs.'],
        [FileCode2, '03', 'Keep the evidence close.', 'Inspect the findings and limitations. Take the code, data, parameters, and report with you.'],
      ].map(([Icon, number, title]) => <article key={number}><div className="lp-step-heading"><Icon size={23} strokeWidth={1.3}/><span>{number}</span></div><h3>{title}</h3></article>)}</div></section>
      <section className="lp-voice lp-section" id="voice" aria-labelledby="voice-heading">
        <div className="lp-voice-copy"><h2 id="voice-heading">Think out loud.<br/><em>Discover together.</em></h2><a className="lp-elevenlabs" href="https://elevenlabs.io/agents" target="_blank" rel="noopener noreferrer"><AudioLines size={15} strokeWidth={1.5}/><span>Powered by <strong>ElevenLabs</strong></span><ArrowUpRight size={12}/></a></div>
        <VoiceGreeting/>
      </section>
      <section className="lp-reproducible lp-section"><div className="lp-evidence-visual" aria-label="Files included in a reproducibility bundle"><div className="lp-file-stack"><span className="lp-file-top"><span className="lp-file-dot"/>Your experiment, all together.<span>ZIP</span></span>{[['plan.json', 'The method & assumptions'], ['experiment.py', 'Your experiment code'], ['input.json', 'The exact inputs'], ['results.json', 'The computed evidence'], ['report.md', 'Findings & limitations']].map(([name]) => <div className="lp-file" key={name}><FileCode2 size={16}/><span>{name}</span><Check size={13}/></div>)}</div></div><div className="lp-evidence-copy"><h2>Discovery should<br/>leave a <em>trail.</em></h2><a className="lp-text-link" href="/app">Find your evidence<ArrowRight size={14}/></a></div></section>
      <section className="lp-faq lp-section"><div><h2>Curious about Kairo?</h2></div><div className="lp-faq-list">{[
        ['Do I need an AI model to get started?', 'No. You can save your own research questions and add manual plans without a model. Connect an open-weight model through Ollama or a compatible server to generate custom methods and Python code.'],
        ['What can I investigate?', 'Kairo accepts research questions across scientific domains. Custom Python experiments can run simulations, data analyses, and benchmarks using the libraries and data available in your worker. The system reports missing requirements explicitly. Physical experiments still require equipment and human involvement.'],
        ['Can I use voice agents?', 'The homepage microphone plays a short introduction using your browser’s voice. Full conversations with an ElevenLabs research agent require an agent connection; that integration is not connected yet.'],
        ['Can I check and reproduce the results?', 'Yes. Completed experiments provide computed results, charts, a report, and a downloadable bundle with the custom experiment code, inputs, parameters, and runtime details. Benchmark timings can vary across machines and system load.'],
      ].map(([question, answer]) => <details key={question}><summary>{question}<PlusIcon/></summary><p>{answer}</p></details>)}</div></section>
      <section className="lp-final"><h2>Follow your next<br/><em>good question.</em></h2><a className="lp-button lp-button-dark" href="/app?new=1" onClick={() => rememberQuestion('')}>Start your research<ArrowRight size={16}/></a></section>
    </main>
    <footer className="lp-footer"><div><a className="lp-brand" href="/" aria-label="Kairo home"><Orbit size={23} strokeWidth={1.5}/><span>Kairo</span></a></div><div className="lp-footer-links"><a href="#platform">Platform</a><a href="#workflow">How it works</a><a href="/app">Workspace<ArrowUpRight size={11}/></a></div><span>© {new Date().getFullYear()} Kairo</span></footer>
  </div>;
}
function PlusIcon() { return <span className="lp-faq-plus" aria-hidden="true">+</span>; }
