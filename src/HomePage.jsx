import React, { useState } from 'react';
import { ArrowDown, ArrowRight, ArrowUpRight, Atom, ChevronDown, ChartNoAxesCombined, Code2, Dna, FlaskConical, Orbit, TrendingUp, AudioLines } from 'lucide-react';
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
      <header className="lp-header"><a className="lp-brand" href="/" aria-label="Kairo home"><Orbit size={25} strokeWidth={1.5}/><span>Kairo</span></a><nav className={menuOpen ? 'lp-nav open' : 'lp-nav'} aria-label="Main navigation"><a href="#workflow" onClick={() => setMenuOpen(false)}>Workflow</a><a href="#voice" onClick={() => setMenuOpen(false)}>Voice</a></nav><a className="lp-nav-cta" href="/app">Open workspace<ArrowUpRight size={14}/></a><button className="lp-menu" aria-label="Toggle navigation" aria-expanded={menuOpen} onClick={() => setMenuOpen((open) => !open)}><ChevronDown size={19}/></button></header>
      <div className="lp-hero-copy" id="main-content"><h1>Make room<br/>for <em>discovery.</em></h1><p>Turn questions into experiments with open-weight AI.</p><div className="lp-hero-actions"><a className="lp-button lp-button-dark" href="/app?new=1" onClick={() => rememberQuestion('')}>Start an experiment<ArrowRight size={16}/></a><a className="lp-quiet-link" href="#workflow">See how it works<ArrowDown size={13}/></a></div></div>
    </section>
    <main className="lp-main">
      <section className="lp-tools lp-domains" aria-label="Scientific domains"><div>{[[Atom, 'Physics'], [FlaskConical, 'Chemistry'], [Dna, 'Biology'], [ChartNoAxesCombined, 'Statistics'], [Code2, 'Computer Science'], [TrendingUp, 'Economics']].map(([Icon, name]) => <span key={name}><Icon size={20} strokeWidth={1.4}/>{name}</span>)}</div></section>
      <section className="lp-platform lp-section" id="workflow"><div className="lp-section-intro"><h2>Your next <em>experiment.</em></h2></div><WorkflowCarousel onStart={rememberQuestion}/></section>
      <section className="lp-voice lp-section" id="voice" aria-labelledby="voice-heading">
        <div className="lp-voice-copy"><h2 id="voice-heading">Just ask<br/><em>Kairo.</em></h2><a className="lp-elevenlabs" href="https://elevenlabs.io/agents" target="_blank" rel="noopener noreferrer"><AudioLines size={15} strokeWidth={1.5}/><span>Powered by <strong>ElevenLabs</strong></span><ArrowUpRight size={12}/></a><a className="lp-text-link lp-voice-workspace-link" href="/app">Open voice workspace<ArrowRight size={14}/></a></div>
        <VoiceGreeting/>
      </section>
      <section className="lp-faq lp-section"><div><h2>Good to know.</h2></div><div className="lp-faq-list">{[
        ['Do I need an AI model to get started?', 'Save your own question or method anytime. Connect a model to generate a research plan.'],
        ['Can I use voice agents?', 'Yes. Open the workspace and select the microphone. The homepage microphone plays a short introduction.'],
      ].map(([question, answer]) => <details key={question}><summary>{question}<PlusIcon/></summary><p>{answer}</p></details>)}</div></section>
    </main>
    <footer className="lp-footer"><div><a className="lp-brand" href="/" aria-label="Kairo home"><Orbit size={23} strokeWidth={1.5}/><span>Kairo</span></a></div><div className="lp-footer-links"><a href="#workflow">Workflow</a><a href="#voice">Voice</a><a href="/app">Workspace<ArrowUpRight size={11}/></a></div><span>© {new Date().getFullYear()} Kairo</span></footer>
  </div>;
}
function PlusIcon() { return <span className="lp-faq-plus" aria-hidden="true">+</span>; }
