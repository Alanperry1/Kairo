// Keep shared context focused on the open experiment, with bounded payloads.
function summary(value, depth = 0) {
  if (typeof value === 'string') return value.slice(0, 2000);
  if (value === null || typeof value !== 'object') return value;
  if (depth >= 3) return '[See the workspace for the full value]';
  if (Array.isArray(value)) return value.slice(0, 12).map((item) => summary(item, depth + 1));
  return Object.fromEntries(Object.entries(value).slice(0, 16).map(([key, item]) => [key, summary(item, depth + 1)]));
}
export function experimentVoiceContext(experiment) {
  return {
    view: 'experiment', id: experiment.id, title: experiment.title,
    question: experiment.question, status: experiment.status, progress: experiment.progress,
    method: experiment.plan ? summary({ hypothesis: experiment.plan.hypothesis, methodology: experiment.plan.methodology, parameters: experiment.plan.parameters, assumptions: experiment.plan.assumptions, limitations: experiment.plan.limitations }) : null,
    results: experiment.result ? summary({ metrics: experiment.result.metrics, findings: experiment.result.findings, limitations: experiment.result.limitations }) : null,
    error: experiment.error || '',
  };
}
