import type { RunListEntry, RunDetail, StepDetail, TraceListEntry, TraceDetail, BatchListEntry, BatchDetail, ApiError, RenameResponse } from './types';

export class ApiRequestError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiRequestError';
    this.status = status;
    this.code = code;
  }
}

async function request<T>(url: string): Promise<T> {
  const res = await fetch(url);
  if (!res.ok) {
    let code = 'unknown';
    let message = res.statusText;
    try {
      const body: ApiError = await res.json();
      code = body.code;
      message = body.error;
    } catch {
      // use defaults
    }
    throw new ApiRequestError(res.status, code, message);
  }
  return res.json() as Promise<T>;
}

async function mutate<T>(url: string, method: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    let code = 'unknown';
    let message = res.statusText;
    try {
      const err: ApiError = await res.json();
      code = err.code;
      message = err.error;
    } catch {
      // use defaults
    }
    throw new ApiRequestError(res.status, code, message);
  }
  return res.json() as Promise<T>;
}

export function fetchRuns(limit = 50, saved?: boolean): Promise<RunListEntry[]> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (saved) params.set('saved', 'true');
  return request<RunListEntry[]>(`/api/runs?${params}`);
}

export function fetchRun(id: string): Promise<RunDetail> {
  return request<RunDetail>(`/api/runs/${encodeURIComponent(id)}`);
}

export function fetchAttempt(runId: string, attempt: number): Promise<RunDetail> {
  return request<RunDetail>(`/api/runs/${encodeURIComponent(runId)}/attempts/${attempt}`);
}

export function fetchStep(runId: string, stepId: string): Promise<StepDetail> {
  return request<StepDetail>(
    `/api/runs/${encodeURIComponent(runId)}/steps/${encodeURIComponent(stepId)}`,
  );
}

export function fetchAttemptStep(runId: string, attempt: number, stepId: string): Promise<StepDetail> {
  return request<StepDetail>(
    `/api/runs/${encodeURIComponent(runId)}/attempts/${attempt}/steps/${encodeURIComponent(stepId)}`,
  );
}

export function fetchBatches(limit = 50, saved?: boolean): Promise<BatchListEntry[]> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (saved) params.set('saved', 'true');
  return request<BatchListEntry[]>(`/api/batches?${params}`);
}

export function fetchBatch(id: string): Promise<BatchDetail> {
  return request<BatchDetail>(`/api/batches/${encodeURIComponent(id)}`);
}

export function fetchTraces(limit = 50): Promise<TraceListEntry[]> {
  return request<TraceListEntry[]>(`/api/traces?limit=${limit}`);
}

export function fetchTrace(id: string): Promise<TraceDetail> {
  return request<TraceDetail>(`/api/traces/${encodeURIComponent(id)}`);
}

export function renameRun(id: string, name: string): Promise<RenameResponse> {
  return mutate<RenameResponse>(`/api/runs/${encodeURIComponent(id)}/name`, 'PUT', { name });
}

export function unnameRun(id: string): Promise<RenameResponse> {
  return mutate<RenameResponse>(`/api/runs/${encodeURIComponent(id)}/name`, 'DELETE');
}

export function renameBatch(id: string, name: string): Promise<RenameResponse> {
  return mutate<RenameResponse>(`/api/batches/${encodeURIComponent(id)}/name`, 'PUT', { name });
}

export function unnameBatch(id: string): Promise<RenameResponse> {
  return mutate<RenameResponse>(`/api/batches/${encodeURIComponent(id)}/name`, 'DELETE');
}
