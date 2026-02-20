export type Route =
  | { view: 'run-list' }
  | { view: 'run-detail'; runId: string }
  | { view: 'step-detail'; runId: string; stepId: string };

const runDetailRe = /^\/runs\/([^/]+)$/;
const stepDetailRe = /^\/runs\/([^/]+)\/steps\/([^/]+)$/;

export function parseRoute(path: string): Route {
  let m: RegExpMatchArray | null;

  m = path.match(stepDetailRe);
  if (m) return { view: 'step-detail', runId: m[1], stepId: m[2] };

  m = path.match(runDetailRe);
  if (m) return { view: 'run-detail', runId: m[1] };

  return { view: 'run-list' };
}

export function navigate(path: string): void {
  history.pushState(null, '', path);
  window.dispatchEvent(new PopStateEvent('popstate'));
}
