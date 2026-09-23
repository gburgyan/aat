export type Outcome = 'passed' | 'failed' | 'error' | 'skipped' | 'aborted' | 'stopped';

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
  oas?: OASSummary;
}

/** A run's OpenAPI validation, from summary.json: the mode and what was checked. */
export interface OASSummary {
  mode: string;
  validatedRequests: number;
  validatedResponses: number;
  violations: number;
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
  retriedOn?: string[];
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
  retriedOn?: string[];
  repeatStop?: string;
  iterations?: IterationSummary[];
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
  knownIssue?: KnownIssueDetail;
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

// One request of a repeated step, as its step detail lists it, without bodies.
export interface IterationSummary {
  index: number;
  status?: number;
  grpcCode?: string; // the gRPC status name, shown in place of status where set
  durationMs: number;
  durationDisplay: string;
  untilMet?: boolean;
  retryCount?: number;
  error?: string;
  oasErrorCount?: number;
  outputs?: Record<string, unknown>; // scalar outputs only
}

// One request of a repeated step, with its bodies, headers, inputs, outputs, and OpenAPI validation.
export interface IterationDetail {
  stepId: string;
  index: number;
  count: number;
  status?: number;
  durationMs: number;
  durationDisplay: string;
  startTime?: string;
  untilMet?: boolean;
  retryCount?: number;
  error?: string;
  repeatStop?: string;
  inputs?: Record<string, unknown>;
  outputs?: Record<string, unknown>;
  request?: RequestDetail;
  response?: ResponseDetail;
  oasValidation?: OASValidationDetail;
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
  originalUrl?: string;
  headers?: HeaderEntry[];
  body?: unknown;
  formFields?: FormField[]; // a form-encoded body's fields, in the order they were sent
  protocol?: string; // "grpc" for a gRPC call; empty or "http" otherwise
  rpc?: string; // a gRPC method as the wire names it: "shop.v1.Carts/CreateCart"
  target?: string; // the gRPC service the call went to: "grpc://host:port"
}

export interface ResponseDetail {
  status: number;
  headers?: HeaderEntry[];
  body?: unknown;
  formFields?: FormField[];
  trailers?: HeaderEntry[]; // a gRPC call's trailing metadata
  grpcCode?: string; // the gRPC status name, shown in place of the HTTP status it maps to
  grpcMessage?: string;
  grpcDetails?: unknown[];
}

// One field of a form-encoded body, decoded.
export interface FormField {
  name: string;
  value: string;
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
  raw?: boolean;
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
  poolRef?: string;
  tried?: unknown[];
}

export interface LLMCallDetail {
  messages: LLMMessageDetail[];
  model: string;
  temperature: number;
  maxTokens?: number;
  thinkingBudget?: number;
  reasoningEffort?: string;
  thinkingContent?: string;
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
  expected: string[];
  actual: string;
  passed: boolean;
}

/** A step's knownIssue entry: why a failed step sits inside a passed run. */
export interface KnownIssueDetail {
  until: string;
  reason: string;
  url?: string;
  applied?: boolean;
  expired?: boolean;
  resolved?: boolean;
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
