<script lang="ts">
  import type { RunDetail } from '../lib/types';
  import { fetchRun } from '../lib/api';
  import { navigate } from '../lib/router';
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

    {#if run.totalAttempts && run.totalAttempts > 1}
      <div class="run-attempt-info">
        Attempt {run.attempt} of {run.totalAttempts}
      </div>
    {/if}

    {#if run.batchId}
      <div class="run-batch-link">
        Part of batch <a
          href="/batches/{run.batchId}"
          onclick={(e: MouseEvent) => { e.preventDefault(); navigate(`/batches/${run.batchId}`); }}
        >{run.batchId}</a>
      </div>
    {/if}

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

  {#if run.attempts && run.attempts.length > 0}
    <details class="attempts-section">
      <summary class="attempts-summary">
        Prior attempts ({run.attempts.length} failed)
      </summary>
      <table class="attempts-table">
        <thead>
          <tr>
            <th>Attempt</th>
            <th>Outcome</th>
            <th>Error</th>
          </tr>
        </thead>
        <tbody>
          {#each run.attempts as attempt (attempt.attempt)}
            <tr>
              <td>#{attempt.attempt}</td>
              <td><OutcomeBadge outcome={attempt.outcome} size="sm" /></td>
              <td class="attempt-error">{attempt.error || '-'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </details>
  {/if}
{/if}

<style>
  .run-batch-link {
    font-size: 0.85rem;
    color: var(--color-text-muted, #9ca3af);
    margin-bottom: 0.5rem;
  }
  .run-batch-link a {
    color: var(--color-primary, #6366f1);
    text-decoration: none;
  }
  .run-batch-link a:hover {
    color: var(--color-primary-hover, #818cf8);
    text-decoration: underline;
  }
  .run-attempt-info {
    font-size: 0.85rem;
    color: var(--color-warning, #f59e0b);
    background: rgba(245, 158, 11, 0.08);
    padding: 0.25rem 0.6rem;
    border-radius: 4px;
    margin-bottom: 0.5rem;
    display: inline-block;
    font-weight: 500;
  }
  .attempts-section {
    margin-top: 1.5rem;
  }
  .attempts-summary {
    cursor: pointer;
    font-size: 0.9rem;
    font-weight: 600;
    color: var(--color-text-muted, #9ca3af);
    padding: 0.5rem 0;
    user-select: none;
  }
  .attempts-summary:hover {
    color: var(--color-text, #f3f4f6);
  }
  .attempts-table {
    width: 100%;
    border-collapse: collapse;
    margin-top: 0.5rem;
    font-size: 0.85rem;
  }
  .attempts-table th {
    text-align: left;
    padding: 0.4rem 0.75rem;
    color: var(--color-text-muted, #9ca3af);
    border-bottom: 1px solid var(--color-border, #374151);
    font-weight: 500;
  }
  .attempts-table td {
    padding: 0.4rem 0.75rem;
    border-bottom: 1px solid var(--color-border-subtle, #1f2937);
  }
  .attempt-error {
    color: var(--color-text-muted, #9ca3af);
    max-width: 400px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
