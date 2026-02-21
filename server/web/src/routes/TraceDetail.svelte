<script lang="ts">
  import type { TraceDetail } from '../lib/types';
  import { fetchTrace } from '../lib/api';
  import { formatDuration, formatTimestamp } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import Tabs from '../components/Tabs.svelte';
  import LlmCallPanel from '../components/LlmCallPanel.svelte';
  import YamlViewer from '../components/YamlViewer.svelte';
  import JsonViewer from '../components/JsonViewer.svelte';

  interface Props {
    traceId: string;
  }

  let { traceId }: Props = $props();

  let trace = $state<TraceDetail | null>(null);
  let loading = $state(true);
  let error = $state('');
  let activeTab = $state('');

  async function load(id: string) {
    loading = true;
    error = '';
    trace = null;
    try {
      trace = await fetchTrace(id);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load trace';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load(traceId);
  });

  interface TabDef {
    id: string;
    label: string;
  }

  let tabs = $derived.by((): TabDef[] => {
    if (!trace) return [];
    const t: TabDef[] = [];
    t.push({ id: 'overview', label: 'Overview' });
    t.push({ id: 'selection', label: 'Selection' });
    if (trace.skeleton) t.push({ id: 'skeleton', label: 'Skeleton' });
    t.push({ id: 'valuefill', label: 'Value Fill' });
    if (trace.mergedPlanYaml || trace.finalPlanYaml || trace.templateExpandedYaml)
      t.push({ id: 'plans', label: 'Plans' });
    if (trace.validationErr || trace.retryCall)
      t.push({ id: 'validation', label: 'Validation' });
    if (trace.wrongPlanSignal)
      t.push({ id: 'wrongplan', label: 'Wrong Plan' });
    return t;
  });

  // Set initial active tab when tabs change
  $effect(() => {
    if (tabs.length > 0 && !tabs.find((t) => t.id === activeTab)) {
      activeTab = tabs[0].id;
    }
  });
</script>

{#if loading}
  <LoadingSpinner />
{:else if error}
  <div class="error-message">
    <p>{error}</p>
    <button onclick={() => load(traceId)}>Retry</button>
  </div>
{:else if trace}
  <div class="step-detail-header">
    <div class="step-detail-title-row">
      {#if trace.error}
        <div class="step-dot step-dot-failed"></div>
      {:else}
        <div class="step-dot step-dot-passed"></div>
      {/if}
      <h1 class="step-detail-title">{trace.prompt}</h1>
    </div>

    {#if trace.error}
      <div class="step-detail-error">{trace.error}</div>
    {/if}

    <div class="step-detail-meta">
      <div class="step-detail-meta-item">
        <span class="meta-label">Trace ID</span>
        <span class="meta-value meta-mono">{trace.traceId}</span>
      </div>
      <div class="step-detail-meta-item">
        <span class="meta-label">Timestamp</span>
        <span class="meta-value">{formatTimestamp(trace.timestamp)}</span>
      </div>
      <div class="step-detail-meta-item">
        <span class="meta-label">Duration</span>
        <span class="meta-value meta-mono">{formatDuration(trace.totalDurationMs)}</span>
      </div>
      {#if trace.workflowName}
        <div class="step-detail-meta-item">
          <span class="meta-label">Workflow</span>
          <span class="meta-value meta-mono">{trace.workflowName}</span>
        </div>
      {/if}
      {#if trace.templatePath}
        <div class="step-detail-meta-item">
          <span class="meta-label">Template</span>
          <span class="meta-value meta-mono">{trace.templatePath}</span>
        </div>
      {/if}
    </div>
  </div>

  {#if tabs.length > 0}
    <Tabs {tabs} active={activeTab} onchange={(id) => (activeTab = id)} />

    <div class="tab-panel">
      {#if activeTab === 'overview'}
        <div class="trace-overview">
          <div class="trace-section">
            <h4 class="section-heading">Prompt</h4>
            <pre class="trace-prompt-pre">{trace.prompt}</pre>
          </div>

          {#if trace.workflowName}
            <div class="trace-section">
              <h4 class="section-heading">Workflow</h4>
              <div class="step-detail-meta">
                <div class="step-detail-meta-item">
                  <span class="meta-label">Name</span>
                  <span class="meta-value meta-mono">{trace.workflowName}</span>
                </div>
                {#if trace.templatePath}
                  <div class="step-detail-meta-item">
                    <span class="meta-label">Template</span>
                    <span class="meta-value meta-mono">{trace.templatePath}</span>
                  </div>
                {/if}
              </div>
            </div>
          {/if}

          {#if trace.repetitions}
            <div class="trace-section">
              <h4 class="section-heading">Repetitions</h4>
              <JsonViewer data={trace.repetitions} />
            </div>
          {/if}
        </div>
      {/if}

      {#if activeTab === 'selection'}
        {#if trace.selectionCall}
          <h4 class="section-heading">Selection Call</h4>
          <LlmCallPanel call={trace.selectionCall} />
        {/if}

        {#if trace.selectionRetryCall}
          <h4 class="section-heading">Selection Retry Call</h4>
          <LlmCallPanel call={trace.selectionRetryCall} />
        {/if}

        {#if trace.workflowSelection}
          <h4 class="section-heading">Parsed Workflow Selection</h4>
          <JsonViewer data={trace.workflowSelection} />
        {/if}
      {/if}

      {#if activeTab === 'skeleton' && trace.skeleton}
        <div class="step-detail-meta" style="margin-bottom: 0.75rem;">
          <div class="step-detail-meta-item">
            <span class="meta-label">Duration</span>
            <span class="meta-value meta-mono">{formatDuration(trace.skeleton.durationMs)}</span>
          </div>
        </div>

        <h4 class="section-heading">Skeleton YAML</h4>
        <YamlViewer content={trace.skeleton.planYaml} />

        {#if trace.skeleton.unfedInputs && trace.skeleton.unfedInputs.length > 0}
          <h4 class="section-heading">Unfed Inputs ({trace.skeleton.unfedInputs.length})</h4>
          <ul class="trace-unfed-list">
            {#each trace.skeleton.unfedInputs as input}
              <li><code>{input}</code></li>
            {/each}
          </ul>
        {/if}
      {/if}

      {#if activeTab === 'valuefill'}
        {#if trace.planCall}
          <h4 class="section-heading">Value Fill Call</h4>
          <LlmCallPanel call={trace.planCall} />
        {/if}

        {#if trace.targetedResponse}
          <h4 class="section-heading">Parsed Response</h4>
          <JsonViewer data={trace.targetedResponse} />
        {/if}
      {/if}

      {#if activeTab === 'plans'}
        {#if trace.templateExpandedYaml}
          <h4 class="section-heading">Template Expanded</h4>
          <YamlViewer content={trace.templateExpandedYaml} />
        {/if}

        {#if trace.mergedPlanYaml}
          <h4 class="section-heading">Merged Plan</h4>
          <YamlViewer content={trace.mergedPlanYaml} />
        {/if}

        {#if trace.finalPlanYaml}
          <h4 class="section-heading">Final Plan</h4>
          <YamlViewer content={trace.finalPlanYaml} />
        {/if}
      {/if}

      {#if activeTab === 'validation'}
        {#if trace.validationErr}
          <h4 class="section-heading">Validation Error</h4>
          <pre class="trace-error-pre">{trace.validationErr}</pre>
        {/if}

        {#if trace.retryCall}
          <h4 class="section-heading">Retry Call</h4>
          <LlmCallPanel call={trace.retryCall} />
        {/if}

        {#if trace.retryValidationErr}
          <h4 class="section-heading">Retry Validation Error</h4>
          <pre class="trace-error-pre">{trace.retryValidationErr}</pre>
        {/if}
      {/if}

      {#if activeTab === 'wrongplan'}
        {#if trace.wrongPlanSignal}
          <h4 class="section-heading">Wrong Plan Signal</h4>
          <JsonViewer data={trace.wrongPlanSignal} />
        {/if}

        {#if trace.wrongPlanCall}
          <h4 class="section-heading">Wrong Plan Detection Call</h4>
          <LlmCallPanel call={trace.wrongPlanCall} />
        {/if}

        {#if trace.reselectionCall}
          <h4 class="section-heading">Re-selection Call</h4>
          <LlmCallPanel call={trace.reselectionCall} />
        {/if}
      {/if}
    </div>
  {/if}
{/if}

<style>
  .trace-overview {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }
  .trace-section {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .trace-prompt-pre {
    background: var(--color-surface, #1e1e1e);
    padding: 0.75rem 1rem;
    border-radius: 6px;
    white-space: pre-wrap;
    word-break: break-word;
    margin: 0;
    font-size: 0.875rem;
    line-height: 1.5;
  }
  .trace-unfed-list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 0.375rem;
  }
  .trace-unfed-list li {
    background: var(--color-surface, #1e1e1e);
    padding: 0.125rem 0.5rem;
    border-radius: 4px;
    font-size: 0.8125rem;
  }
  .trace-unfed-list code {
    font-size: 0.8125rem;
  }
  .trace-error-pre {
    background: var(--color-surface, #1e1e1e);
    border-left: 3px solid var(--color-danger, #ef4444);
    padding: 0.75rem 1rem;
    border-radius: 0 6px 6px 0;
    white-space: pre-wrap;
    word-break: break-word;
    margin: 0;
    font-size: 0.875rem;
    line-height: 1.5;
  }
</style>
