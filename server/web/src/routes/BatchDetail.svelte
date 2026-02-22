<script lang="ts">
  import type { BatchDetail } from '../lib/types';
  import { fetchBatch } from '../lib/api';
  import { navigate } from '../lib/router';
  import { formatDuration, timeAgo, formatTimestamp } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import OutcomeBadge from '../components/OutcomeBadge.svelte';

  interface Props {
    batchId: string;
  }

  let { batchId }: Props = $props();

  let batch = $state<BatchDetail | null>(null);
  let loading = $state(true);
  let error = $state('');

  async function load(id: string) {
    loading = true;
    error = '';
    batch = null;
    try {
      batch = await fetchBatch(id);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load batch';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load(batchId);
  });

  function goToRun(runId: string) {
    navigate(`/runs/${runId}`);
  }

  function handleRowKeydown(e: KeyboardEvent, runId: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      goToRun(runId);
    }
  }

  let displayLabel = $derived(batch?.source || batchId);
</script>

{#if loading}
  <LoadingSpinner />
{:else if error}
  <div class="error-message">
    <p>{error}</p>
    <button onclick={() => load(batchId)}>Retry</button>
  </div>
{:else if batch}
  <div class="run-detail-header">
    <div class="run-detail-title-row">
      <OutcomeBadge outcome={batch.outcome} size="md" />
      <h1 class="run-detail-title">{displayLabel}</h1>
    </div>

    <div class="run-detail-meta">
      <div class="run-detail-meta-item">
        <span class="meta-label">Duration</span>
        <span class="meta-value meta-mono">{formatDuration(batch.totalDurationMs)}</span>
      </div>
      <div class="run-detail-meta-item">
        <span class="meta-label">When</span>
        <span class="meta-value" title={formatTimestamp(batch.timestamp)}>{timeAgo(batch.timestamp)}</span>
      </div>
      <div class="run-detail-meta-item">
        <span class="meta-label">Runs</span>
        <span class="meta-value">
          <span class="steps-passed">{batch.passedRuns}</span>
          {#if batch.failedRuns > 0}<span class="steps-separator"> / </span><span class="steps-failed">{batch.failedRuns}</span>{/if}
          {#if batch.errorRuns > 0}<span class="steps-separator"> / </span><span class="steps-error">{batch.errorRuns}</span>{/if}
          <span class="steps-separator">of&nbsp;</span>{batch.totalRuns}
        </span>
      </div>
      {#if batch.toolVersion}
        <div class="run-detail-meta-item">
          <span class="meta-label">Tool</span>
          <span class="meta-value meta-mono">{batch.toolVersion}</span>
        </div>
      {/if}
    </div>
  </div>

  {#if batch.runs.length === 0}
    <div class="empty-state">
      <p>No runs in this batch</p>
    </div>
  {:else}
    <table class="run-table">
      <thead>
        <tr>
          <th>Outcome</th>
          <th>Plan</th>
          <th>Steps</th>
          <th>Duration</th>
        </tr>
      </thead>
      <tbody>
        {#each batch.runs as run (run.runId)}
          <tr
            class="run-row"
            role="link"
            tabindex="0"
            onclick={() => goToRun(run.runId)}
            onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, run.runId)}
          >
            <td><OutcomeBadge outcome={run.outcome} size="sm" /></td>
            <td class="cell-plan" title={run.planName || run.runId}>{run.planName || run.runId}</td>
            <td class="cell-steps">
              <span class="steps-passed">{run.passedCount}</span>{#if run.failedCount > 0}<span class="steps-separator"> / </span><span class="steps-failed">{run.failedCount}</span>{/if}
              <span class="steps-separator"> of {run.stepCount}</span>
            </td>
            <td class="cell-duration">{formatDuration(run.durationMs)}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
{/if}

<style>
  .steps-error {
    color: var(--color-warning, #f59e0b);
  }
</style>
