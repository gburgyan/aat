<script lang="ts">
  import type { RunDetail } from '../lib/types';
  import { fetchRun } from '../lib/api';
  import { formatDuration, timeAgo, formatTimestamp } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import OutcomeBadge from '../components/OutcomeBadge.svelte';
  import StepTimeline from '../components/StepTimeline.svelte';

  interface Props {
    runId: string;
  }

  let { runId }: Props = $props();

  let run = $state<RunDetail | null>(null);
  let loading = $state(true);
  let error = $state('');

  async function load(id: string) {
    loading = true;
    error = '';
    run = null;
    try {
      run = await fetchRun(id);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load run';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load(runId);
  });

  let displayName = $derived(run?.planName || runId);
</script>

{#if loading}
  <LoadingSpinner />
{:else if error}
  <div class="error-message">
    <p>{error}</p>
    <button onclick={() => load(runId)}>Retry</button>
  </div>
{:else if run}
  <div class="run-detail-header">
    <div class="run-detail-title-row">
      <OutcomeBadge outcome={run.outcome} size="md" />
      <h1 class="run-detail-title">{displayName}</h1>
    </div>

    {#if run.error}
      <div class="run-detail-error">{run.error}</div>
    {/if}

    <div class="run-detail-meta">
      <div class="run-detail-meta-item">
        <span class="meta-label">Duration</span>
        <span class="meta-value meta-mono">{formatDuration(run.durationMs)}</span>
      </div>
      <div class="run-detail-meta-item">
        <span class="meta-label">When</span>
        <span class="meta-value" title={formatTimestamp(run.timestamp)}>{timeAgo(run.timestamp)}</span>
      </div>
      <div class="run-detail-meta-item">
        <span class="meta-label">Steps</span>
        <span class="meta-value">
          <span class="steps-passed">{run.passedCount}</span>
          {#if run.failedCount > 0}<span class="steps-separator"> / </span><span class="steps-failed">{run.failedCount}</span>{/if}
          <span class="steps-separator">of&nbsp;</span>{run.stepCount}
        </span>
      </div>
      {#if run.environment}
        <div class="run-detail-meta-item">
          <span class="meta-label">Environment</span>
          <span class="meta-value">{run.environment}</span>
        </div>
      {/if}
      {#if run.toolVersion}
        <div class="run-detail-meta-item">
          <span class="meta-label">Tool</span>
          <span class="meta-value meta-mono">{run.toolVersion}</span>
        </div>
      {/if}
    </div>
  </div>

  <StepTimeline steps={run.steps} {runId} sectionLabel="Steps" />

  {#if run.cleanup && run.cleanup.length > 0}
    <StepTimeline steps={run.cleanup} {runId} sectionLabel="Cleanup" muted />
  {/if}
{/if}
