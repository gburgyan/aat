<script lang="ts">
  import type { StepDetail, LLMCallDetail } from '../lib/types';
  import { fetchStep } from '../lib/api';
  import { navigate } from '../lib/router';
  import { formatDuration, httpStatusCategory } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import Tabs from '../components/Tabs.svelte';
  import JsonViewer from '../components/JsonViewer.svelte';
  import HeadersTable from '../components/HeadersTable.svelte';
  import AssertionsTable from '../components/AssertionsTable.svelte';
  import ResolutionsTable from '../components/ResolutionsTable.svelte';
  import SelectionsTable from '../components/SelectionsTable.svelte';
  import ExtractionsTable from '../components/ExtractionsTable.svelte';
  import YamlViewer from '../components/YamlViewer.svelte';
  import LlmCallPanel from '../components/LlmCallPanel.svelte';
  import ErrorPanel from '../components/ErrorPanel.svelte';

  interface Props {
    runId: string;
    stepId: string;
  }

  let { runId, stepId }: Props = $props();

  let step = $state<StepDetail | null>(null);
  let loading = $state(true);
  let error = $state('');
  let activeTab = $state('');

  async function load(rid: string, sid: string) {
    loading = true;
    error = '';
    step = null;
    try {
      step = await fetchStep(rid, sid);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load step';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load(runId, stepId);
  });

  interface TabDef {
    id: string;
    label: string;
  }

  let tabs = $derived.by((): TabDef[] => {
    if (!step) return [];
    const t: TabDef[] = [];
    if (step.request) t.push({ id: 'request', label: 'Request' });
    if (step.response) t.push({ id: 'response', label: 'Response' });
    if (step.extractions && step.extractions.length > 0)
      t.push({ id: 'extractions', label: `Extractions (${step.extractions.length})` });
    if (step.validation)
      t.push({
        id: 'assertions',
        label: `Assertions (${step.assertionPassedCount}/${step.assertionCount})`,
      });
    if (step.resolutions && step.resolutions.length > 0)
      t.push({ id: 'resolutions', label: `Resolutions (${step.resolutions.length})` });
    if (step.selections && step.selections.length > 0)
      t.push({ id: 'selections', label: `Selections (${step.selections.length})` });
    if (llmCalls.length > 0)
      t.push({ id: 'llm', label: `LLM (${llmCalls.length})` });
    if (hasErrors)
      t.push({ id: 'errors', label: 'Errors' });
    if (step.planStepYaml)
      t.push({ id: 'plan', label: 'Plan' });
    return t;
  });

  interface LLMCallContext {
    context: string;
    call: LLMCallDetail;
  }

  let llmCalls = $derived.by((): LLMCallContext[] => {
    if (!step) return [];
    const calls: LLMCallContext[] = [];
    if (step.resolutions) {
      for (const r of step.resolutions) {
        if (r.llmCall) {
          calls.push({ context: `Resolution: ${r.inputName}`, call: r.llmCall });
        }
      }
    }
    if (step.selections) {
      for (const s of step.selections) {
        if (s.llmCall) {
          calls.push({ context: `Selection: ${s.inputName}`, call: s.llmCall });
        }
      }
    }
    return calls;
  });

  let hasErrors = $derived(
    !!(step?.errorClassification || step?.expectFailure || step?.responseBodyError ||
      (step?.relaxations && step.relaxations.length > 0)),
  );

  // Set initial active tab when tabs change
  $effect(() => {
    if (tabs.length > 0 && !tabs.find((t) => t.id === activeTab)) {
      activeTab = tabs[0].id;
    }
  });

  let statusCat = $derived(httpStatusCategory(step?.status));

  function readPref(key: string, fallback: boolean): boolean {
    try {
      const v = localStorage.getItem(`aat:${key}`);
      return v === null ? fallback : v === '1';
    } catch { return fallback; }
  }

  function savePref(key: string, e: Event) {
    const open = (e.currentTarget as HTMLDetailsElement).open;
    try { localStorage.setItem(`aat:${key}`, open ? '1' : '0'); } catch {}
  }
</script>

{#if loading}
  <LoadingSpinner />
{:else if error}
  <div class="error-message">
    <p>{error}</p>
    <button onclick={() => load(runId, stepId)}>Retry</button>
  </div>
{:else if step}
  <div class="step-detail-header">
    <div class="step-detail-title-row">
      <div class="step-dot" class:step-dot-passed={step.passed} class:step-dot-failed={!step.passed}></div>
      <h1 class="step-detail-title">{step.stepId}</h1>
      {#if step.isCleanup}
        <span class="badge badge-sm badge-muted">CLEANUP</span>
      {/if}
      <div class="step-nav">
        {#if step.prevStepId}
          <a href="/runs/{runId}/steps/{step.prevStepId}" class="step-nav-btn" title="{step.prevStepId}" onclick={(e: MouseEvent) => { e.preventDefault(); navigate(`/runs/${runId}/steps/${step.prevStepId}`); }}>&larr; Prev</a>
        {:else}
          <span class="step-nav-btn step-nav-disabled">&larr; Prev</span>
        {/if}
        {#if step.nextStepId}
          <a href="/runs/{runId}/steps/{step.nextStepId}" class="step-nav-btn" title="{step.nextStepId}" onclick={(e: MouseEvent) => { e.preventDefault(); navigate(`/runs/${runId}/steps/${step.nextStepId}`); }}>Next &rarr;</a>
        {:else}
          <span class="step-nav-btn step-nav-disabled">Next &rarr;</span>
        {/if}
      </div>
    </div>

    {#if step.error}
      <div class="step-detail-error">{step.error}</div>
    {/if}

    <div class="step-detail-meta">
      <div class="step-detail-meta-item">
        <span class="meta-label">Node</span>
        <span class="meta-value meta-mono">{step.node}</span>
      </div>
      {#if step.status !== undefined}
        <div class="step-detail-meta-item">
          <span class="meta-label">Status</span>
          <span class="meta-value">
            <span class="step-status step-status-{statusCat}">{step.status}</span>
          </span>
        </div>
      {/if}
      <div class="step-detail-meta-item">
        <span class="meta-label">Duration</span>
        <span class="meta-value meta-mono">{formatDuration(step.durationMs)}</span>
      </div>
      <div class="step-detail-meta-item">
        <span class="meta-label">Assertions</span>
        <span class="meta-value">
          <span class="steps-passed">{step.assertionPassedCount}</span>
          <span class="steps-separator"> / </span>
          {step.assertionCount}
        </span>
      </div>
      {#if step.retryCount && step.retryCount > 0}
        <div class="step-detail-meta-item">
          <span class="meta-label">Retries</span>
          <span class="meta-value"><span class="step-retry-badge">{step.retryCount}</span></span>
        </div>
      {/if}
      {#if step.hasLLMCalls}
        <div class="step-detail-meta-item">
          <span class="meta-label">LLM</span>
          <span class="meta-value"><span class="step-llm-badge">LLM</span></span>
        </div>
      {/if}
    </div>

    {#if step.displayOutputs && step.displayOutputs.length > 0}
      <div class="step-outputs" style="margin-top: 0.75rem;">
        {#each step.displayOutputs as output}
          <span class="step-output">
            <span class="step-output-label">{output.label}:</span>
            <span class="step-output-value">{output.value ?? ''}</span>
          </span>
        {/each}
      </div>
    {/if}
  </div>

  {#if tabs.length > 0}
    <Tabs {tabs} active={activeTab} onchange={(id) => (activeTab = id)} />

    <div class="tab-panel">
      {#if activeTab === 'request' && step.request}
        <div class="http-method-url">
          <span class="http-method">{step.request.method}</span>
          <span class="http-url">{step.request.url}</span>
        </div>
        {#if step.request.headers && step.request.headers.length > 0}
          <details open={readPref('reqHeadersOpen', true)} ontoggle={(e: Event) => savePref('reqHeadersOpen', e)}>
            <summary class="section-heading collapsible-heading">Headers ({step.request.headers.length})</summary>
            <HeadersTable headers={step.request.headers} />
          </details>
        {/if}
        {#if step.request.body !== undefined && step.request.body !== null}
          <h4 class="section-heading">Body</h4>
          <JsonViewer data={step.request.body} />
        {/if}
      {/if}

      {#if activeTab === 'response' && step.response}
        <div class="http-method-url">
          <span class="step-status step-status-{httpStatusCategory(step.response.status)}" style="font-size: 0.9rem;">
            {step.response.status}
          </span>
        </div>
        {#if step.response.headers && step.response.headers.length > 0}
          <details open={readPref('resHeadersOpen', true)} ontoggle={(e: Event) => savePref('resHeadersOpen', e)}>
            <summary class="section-heading collapsible-heading">Headers ({step.response.headers.length})</summary>
            <HeadersTable headers={step.response.headers} />
          </details>
        {/if}
        {#if step.response.body !== undefined && step.response.body !== null}
          <h4 class="section-heading">Body</h4>
          <JsonViewer data={step.response.body} />
        {/if}
      {/if}

      {#if activeTab === 'extractions' && step.extractions}
        <ExtractionsTable extractions={step.extractions} {runId} />
      {/if}

      {#if activeTab === 'assertions' && step.validation}
        <AssertionsTable validation={step.validation} />
      {/if}

      {#if activeTab === 'resolutions' && step.resolutions}
        <ResolutionsTable resolutions={step.resolutions} {runId} />
      {/if}

      {#if activeTab === 'selections' && step.selections}
        <SelectionsTable selections={step.selections} {runId} />
      {/if}

      {#if activeTab === 'llm'}
        {#each llmCalls as lc, i (i)}
          <h4 class="section-heading">{lc.context}</h4>
          <LlmCallPanel call={lc.call} />
        {/each}
      {/if}

      {#if activeTab === 'errors'}
        <ErrorPanel
          errorClassification={step.errorClassification}
          expectFailure={step.expectFailure}
          responseBodyError={step.responseBodyError}
          relaxations={step.relaxations}
        />
      {/if}

      {#if activeTab === 'plan' && step.planStepYaml}
        <YamlViewer content={step.planStepYaml} />
      {/if}
    </div>
  {/if}
{/if}
