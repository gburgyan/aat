import type { RunListEntry, RunDetail, StepDetail, TraceListEntry, TraceDetail, BatchListEntry, BatchDetail, ApiError } from './types';

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

export function fetchRuns(limit = 50): Promise<RunListEntry[]> {
  return request<RunListEntry[]>(`/api/runs?limit=${limit}`);
}

export function fetchRun(id: string): Promise<RunDetail> {
  return request<RunDetail>(`/api/runs/${encodeURIComponent(id)}`);
}

export function fetchStep(runId: string, stepId: string): Promise<StepDetail> {
  return request<StepDetail>(
    `/api/runs/${encodeURIComponent(runId)}/steps/${encodeURIComponent(stepId)}`,
  );
}

export function fetchBatches(limit = 50): Promise<BatchListEntry[]> {
  return request<BatchListEntry[]>(`/api/batches?limit=${limit}`);
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
