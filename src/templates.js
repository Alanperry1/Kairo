export const labels = { draft: 'Draft', planning: 'Planning', planned: 'Planned', queued: 'Queued', running: 'Running', completed: 'Completed', failed: 'Failed', cancelled: 'Cancelled', needs_review: 'Needs review' };
export const domainLabels = { physics: 'Physics', statistics: 'Statistics', computer_science: 'Computer science' };
export const isBusy = (status) => ['planning', 'queued', 'running'].includes(status);
export function timeAgo(value) {
  if (!value) return '';
  const seconds = Math.max(0, (Date.now() - new Date(value).getTime()) / 1000);
  if (seconds < 60) return 'Just now';
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86400)}d ago`;
}
export const shortID = (id) => id?.replace('exp_', '').slice(0, 8).toUpperCase();
