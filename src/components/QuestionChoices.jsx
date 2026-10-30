import React from 'react';
import { ArrowUpRight, Check } from 'lucide-react';

export function validQuestion(question) {
  return question && typeof question.prompt === 'string' && question.prompt.trim().length > 0 && question.prompt.length <= 1200 && Array.isArray(question.options) && question.options.length >= 2 && question.options.length <= 5 && question.options.every((option) => typeof option === 'string' && option.trim() && option.length <= 160) && new Set(question.options.map((option) => option.trim().toLowerCase())).size === question.options.length;
}
export default function QuestionChoices({ question, onAnswer, disabled, answer }) {
  if (!validQuestion(question)) return null;
  return <section className="kairo-question" aria-label="Kairo follow-up question"><p>{question.prompt}</p><div className="kairo-options" role="group" aria-label={question.prompt}>{question.options.map((option) => <button className={answer === option ? 'is-chosen' : ''} type="button" key={option} disabled={disabled || !!answer} onClick={() => onAnswer(option)}>{option}{answer === option ? <Check size={14}/> : <ArrowUpRight size={13}/>}</button>)}</div>{!disabled && !answer && <small>Or tell Kairo in your own words.</small>}{answer && <small className="kairo-chosen">Answered: {answer}</small>}</section>;
}
