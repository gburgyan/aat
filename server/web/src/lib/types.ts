export type Outcome = 'passed' | 'failed' | 'error';

export interface ApiError {
  error: string;
  code: string;
}

export interface RunListEntry {
  runId: string;
  timestamp: string;
  outcome: Outcome;
  stepCount: number;
  passedCount: number;
  failedCount: number;
  durationMs: number;
  planName?: string;
}

export interface RunDetail {
  runId: string;
  timestamp: string;
  outcome: Outcome;
  error?: string;
  durationMs: number;
  durationDisplay: string;
  stepCount: number;
  passedCount: number;
  failedCount: number;
  planName?: string;
  environment?: string;
  graphVersion?: string;
  toolVersion?: string;
  steps: StepSummary[];
  cleanup?: StepSummary[];
}

export interface StepSummary {
  stepId: string;
  node: string;
  status?: number;
  durationMs: number;
  durationDisplay: string;
  passed: boolean;
  assertionCount: number;
  assertionPassedCount: number;
  displayOutputs?: DisplayOutput[];
  error?: string;
  isCleanup?: boolean;
  hasSelections?: boolean;
  hasResolutions?: boolean;
  hasLLMCalls?: boolean;
  hasTransform?: boolean;
  retryCount?: number;
  offsetMs?: number;
}

export interface StepDetail {
  stepId: string;
  node: string;
  status?: number;
  durationMs: number;
  durationDisplay: string;
  passed: boolean;
  assertionCount: number;
  assertionPassedCount: number;
  displayOutputs?: DisplayOutput[];
  error?: string;
  isCleanup?: boolean;
  hasSelections?: boolean;
  hasResolutions?: boolean;
  hasLLMCalls?: boolean;
  retryCount?: number;
  startTime?: string;
  inputs?: Record<string, unknown>;
  outputs?: Record<string, unknown>;
  request?: RequestDetail;
  response?: ResponseDetail;
  validation?: ValidationDetail;
  selections?: SelectionDetail[];
  resolutions?: ResolutionDetail[];
  relaxations?: RelaxationDetail[];
  errorClassification?: ErrorClassDetail;
  expectFailure?: ExpectFailureDetail;
  responseBodyError?: ResponseBodyErrorDetail;
  transformScript?: string;
  extractions?: ExtractionDetail[];
  planStepYaml?: string;
  prevStepId?: string;
  nextStepId?: string;
}

export interface ExtractionDetail {
  name: string;
  value?: unknown;
  consumers?: OutputConsumer[];
}

export interface OutputConsumer {
  stepId: string;
  inputName: string;
  via: string; // "resolution" or "selection"
}

export interface HeaderEntry {
  name: string;
  value: string;
}

export interface DisplayOutput {
  label: string;
  name: string;
  value?: unknown;
}

export interface RequestDetail {
  method: string;
  url: string;
  headers?: HeaderEntry[];
  body?: unknown;
}

export interface ResponseDetail {
  status: number;
  headers?: HeaderEntry[];
  body?: unknown;
}

export interface ValidationDetail {
  passed: boolean;
  results?: AssertionDetail[];
}

export interface AssertionDetail {
  type: string;
  passed: boolean;
  skipped?: boolean;
  message: string;
  path?: string;
  expr?: string;
}

export interface SelectionDetail {
  inputName: string;
  sourceStep?: string;
  sourceNode: string;
  sourceField: string;
  sourceSize: number;
  filterExpr?: string;
  filteredSize: number;
  strategy: string;
  selectedIndex: number;
  selectionName?: string;
  filterRelaxed?: boolean;
  llmCall?: LLMCallDetail;
}

export interface ResolutionDetail {
  inputName: string;
  source: string;
  rawValue?: unknown;
  finalValue?: unknown;
  fromStep?: string;
  fromOutput?: string;
  expression?: string;
  constraint?: string;
  constraintOk?: boolean | null;
  poolIndex?: number;
  poolSize?: number;
  tried?: unknown[];
  relaxed?: boolean;
  relaxedConstraint?: string;
  llmCall?: LLMCallDetail;
}

export interface LLMCallDetail {
  messages: LLMMessageDetail[];
  model: string;
  response: string;
  inputTokens: number;
  outputTokens: number;
  durationMs: number;
  finishReason?: string;
  error?: string;
}

export interface LLMMessageDetail {
  role: string;
  content: string;
}

export interface RelaxationDetail {
  constraintName: string;
  inputRef: string;
  reason: string;
  depth: number;
}

export interface ErrorClassDetail {
  category: string;
  detail: string;
  action: string;
  retryAttempt: number;
}

export interface ExpectFailureDetail {
  expected: number[];
  actual: number;
  passed: boolean;
}

export interface ResponseBodyErrorDetail {
  rulePath: string;
  rule: string;
  message?: string;
  code?: string;
  category?: string;
}
