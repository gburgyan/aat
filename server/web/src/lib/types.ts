export type Outcome = 'passed' | 'failed' | 'error' | 'skipped';

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
  batchId?: string;
  attempt?: number;
  totalAttempts?: number;
  name?: string;
  layers?: string[];
  issues?: Record<string, number>;
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
  batchId?: string;
  steps: StepSummary[];
  cleanup?: StepSummary[];
  attempt?: number;
  totalAttempts?: number;
  attempts?: AttemptSummary[];
  name?: string;
  layers?: string[];
}

export interface AttemptSummary {
  attempt: number;
  outcome: Outcome;
  error?: string;
  fileName: string;
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
  hasTransform?: boolean;
  hasOasValidation?: boolean;
  oasErrorCount?: number;
  oasReqErrorCount?: number;
  oasRespErrorCount?: number;
  oasWarningCount?: number;
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
  retryCount?: number;
  startTime?: string;
  inputs?: Record<string, unknown>;
  outputs?: Record<string, unknown>;
  request?: RequestDetail;
  response?: ResponseDetail;
  validation?: ValidationDetail;
  selections?: SelectionDetail[];
  resolutions?: ResolutionDetail[];
  errorClassification?: ErrorClassDetail;
  expectFailure?: ExpectFailureDetail;
  responseBodyError?: ResponseBodyErrorDetail;
  oasValidation?: OASValidationDetail;
  transformScript?: string;
  extractions?: ExtractionDetail[];
  planStepYaml?: string;
  instantiatedStepYaml?: string;
  prevStepId?: string;
  nextStepId?: string;
  visualizers?: VisualizerHit[];
}

export interface VisualizerHit {
  id: string;
  name: string;
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
}

export interface ResolutionDetail {
  inputName: string;
  source: string;
  rawValue?: unknown;
  finalValue?: unknown;
  fromStep?: string;
  fromOutput?: string;
  fromInput?: string;
  expression?: string;
  constraint?: string;
  constraintOk?: boolean | null;
  poolIndex?: number;
  poolSize?: number;
  tried?: unknown[];
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

export interface OASValidationDetail {
  operationId?: string;
  request?: OASPayloadDetail;
  response?: OASPayloadDetail;
  skipped?: boolean;
  skipReason?: string;
}

export interface OASPayloadDetail {
  valid: boolean;
  errorCount: number;
  errors?: OASValidationErrorDetail[];
  compilationWarnings?: string[];
}

export interface OASValidationErrorDetail {
  path: string;
  message: string;
}

// --- Trace types ---

export interface TraceListEntry {
  traceId: string;
  timestamp: string;
  prompt: string;
  workflowName?: string;
  totalDurationMs: number;
  hasError: boolean;
  llmCallCount: number;
}

export interface TraceDetail {
  traceId: string;
  timestamp: string;
  prompt: string;
  selectionCall?: LLMCallDetail;
  selectionRetryCall?: LLMCallDetail;
  workflowSelection?: unknown;
  skeleton?: SkeletonDetail;
  planCall?: LLMCallDetail;
  targetedResponse?: unknown;
  mergedPlanYaml?: string;
  finalPlanYaml?: string;
  validationErr?: string;
  retryCall?: LLMCallDetail;
  retryValidationErr?: string;
  totalDurationMs: number;
  error?: string;
  wrongPlanSignal?: unknown;
  wrongPlanCall?: LLMCallDetail;
  reselectionCall?: LLMCallDetail;
  workflowName?: string;
  templatePath?: string;
  repetitions?: unknown;
  templateExpandedYaml?: string;
  recipeYaml?: string;
}

export interface SkeletonDetail {
  planYaml: string;
  unfedInputs?: string[];
  durationMs: number;
}

// --- Batch types ---

export interface BatchListEntry {
  batchId: string;
  timestamp: string;
  outcome: Outcome;
  totalRuns: number;
  passedRuns: number;
  failedRuns: number;
  errorRuns: number;
  totalDurationMs: number;
  source?: string;
  toolVersion?: string;
  name?: string;
  layers?: string[];
  issues?: Record<string, number>;
}

export interface BatchDetail {
  batchId: string;
  timestamp: string;
  outcome: Outcome;
  totalRuns: number;
  passedRuns: number;
  failedRuns: number;
  errorRuns: number;
  skippedRuns?: number;
  totalDurationMs: number;
  durationDisplay: string;
  source?: string;
  toolVersion?: string;
  runs: BatchRunSummary[];
  name?: string;
  layers?: string[];
  layerGroups?: string[][];
  issues?: Record<string, number>;
}

export interface BatchRunSummary {
  planName: string;
  runId: string;
  outcome: Outcome;
  stepCount: number;
  passedCount: number;
  failedCount: number;
  durationMs: number;
  error?: string;
  attempts?: number;
  layers?: string[];
  permutation?: string;
  skipped?: boolean;
  duplicateOf?: string;
  issues?: Record<string, number>;
}

export interface RenameResponse {
  ref: string;
  name: string;
}

export interface ImportResponse {
  ref: string;
  name: string;
  type: 'run' | 'batch';
}

export type UnifiedListEntry =
  | { kind: 'run'; entry: RunListEntry; timestamp: string }
  | { kind: 'batch'; entry: BatchListEntry; timestamp: string };
