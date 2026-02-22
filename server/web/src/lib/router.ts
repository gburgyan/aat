export type Route =
  | { view: 'run-list' }
  | { view: 'run-detail'; runId: string }
  | { view: 'step-detail'; runId: string; stepId: string }
  | { view: 'batch-detail'; batchId: string }
  | { view: 'trace-list' }
  | { view: 'trace-detail'; traceId: string };

const runDetailRe = /^\/runs\/([^/]+)$/;
const stepDetailRe = /^\/runs\/([^/]+)\/steps\/([^/]+)$/;
const batchDetailRe = /^\/batches\/([^/]+)$/;
const traceDetailRe = /^\/traces\/([^/]+)$/;

export function parseRoute(path: string): Route {
  let m: RegExpMatchArray | null;

  m = path.match(stepDetailRe);
  if (m) return { view: 'step-detail', runId: m[1], stepId: m[2] };

  m = path.match(runDetailRe);
  if (m) return { view: 'run-detail', runId: m[1] };

  m = path.match(batchDetailRe);
  if (m) return { view: 'batch-detail', batchId: m[1] };

  m = path.match(traceDetailRe);
  if (m) return { view: 'trace-detail', traceId: m[1] };

  if (path === '/traces') return { view: 'trace-list' };

  return { view: 'run-list' };
}

export function navigate(path: string): void {
  history.pushState(null, '', path);
  window.dispatchEvent(new PopStateEvent('popstate'));
}
