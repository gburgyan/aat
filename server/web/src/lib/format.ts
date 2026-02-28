/** Render a millisecond duration as a human-readable string (mirrors Go's formatDuration). */
export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) {
    if (ms % 1000 === 0) return `${ms / 1000}s`;
    return `${(ms / 1000).toFixed(1)}s`;
  }
  const mins = Math.floor(ms / 60000);
  const remainMs = ms % 60000;
  if (remainMs === 0) return `${mins}m`;
  const secs = Math.floor(remainMs / 1000);
  return `${mins}m ${secs}s`;
}

/** Relative time label: "just now", "N minutes ago", etc. */
export function timeAgo(date: string): string {
  const now = Date.now();
  const then = new Date(date).getTime();
  const diffMs = now - then;
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return 'just now';
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin} minute${diffMin === 1 ? '' : 's'} ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr} hour${diffHr === 1 ? '' : 's'} ago`;
  const diffDay = Math.floor(diffHr / 24);
  return `${diffDay} day${diffDay === 1 ? '' : 's'} ago`;
}

/** Locale-formatted timestamp. */
export function formatTimestamp(date: string): string {
  return new Date(date).toLocaleString();
}

/** Total count of all categorized issues. */
export function totalIssueCount(issues?: Record<string, number>): number {
  if (!issues) return 0;
  return Object.values(issues).reduce((sum, n) => sum + n, 0);
}

/** Tooltip text with per-category breakdown. */
export function issueTooltip(issues?: Record<string, number>): string {
  if (!issues) return '';
  return Object.entries(issues)
    .map(([cat, n]) => `${cat.toUpperCase()}: ${n}`)
    .join(', ');
}

/** Map an HTTP status code to a semantic category for CSS class selection. */
export function httpStatusCategory(
  status: number | undefined,
): 'success' | 'client-error' | 'server-error' | 'redirect' | 'info' | 'unknown' {
  if (status === undefined) return 'unknown';
  if (status >= 200 && status < 300) return 'success';
  if (status >= 400 && status < 500) return 'client-error';
  if (status >= 500) return 'server-error';
  if (status >= 300 && status < 400) return 'redirect';
  if (status >= 100 && status < 200) return 'info';
  return 'unknown';
}
