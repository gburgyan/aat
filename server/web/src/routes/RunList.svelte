<script lang="ts">
  import type { RunListEntry, BatchListEntry, UnifiedListEntry } from '../lib/types';
  import { fetchRuns, fetchBatches } from '../lib/api';
  import { navigate } from '../lib/router';
  import { formatDuration, timeAgo, formatTimestamp } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import OutcomeBadge from '../components/OutcomeBadge.svelte';

  let items = $state<UnifiedListEntry[]>([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      const [runs, batches] = await Promise.all([fetchRuns(), fetchBatches()]);

      const unified: UnifiedListEntry[] = [];
      for (const r of runs) {
        unified.push({ kind: 'run', entry: r, timestamp: r.timestamp });
      }
      for (const b of batches) {
        unified.push({ kind: 'batch', entry: b, timestamp: b.timestamp });
      }
      unified.sort((a, b) => b.timestamp.localeCompare(a.timestamp));
      items = unified;
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load runs';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load();
  });

  function goToRun(runId: string) {
    navigate(`/runs/${runId}`);
  }

  function goToBatch(batchId: string) {
    navigate(`/batches/${batchId}`);
  }

  function handleRowKeydown(e: KeyboardEvent, path: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      navigate(path);
    }
  }

  function displayName(run: RunListEntry): string {
    return run.planName || run.runId;
  }

  function batchLabel(batch: BatchListEntry): string {
    return batch.source || batch.batchId;
  }
</script>

{#if loading}
  <LoadingSpinner />
{:else if error}
  <div class="error-message">
    <p>{error}</p>
    <button onclick={load}>Retry</button>
  </div>
{:else if items.length === 0}
  <div class="empty-state">
    <p>No runs found</p>
  </div>
{:else}
  <table class="run-table">
    <thead>
      <tr>
        <th>Outcome</th>
        <th>Plan</th>
        <th>Steps</th>
        <th>Duration</th>
        <th>When</th>
      </tr>
    </thead>
    <tbody>
      {#each items as item (item.kind === 'run' ? item.entry.runId : item.entry.batchId)}
        {#if item.kind === 'run'}
          <tr
            class="run-row"
            role="link"
            tabindex="0"
            onclick={() => goToRun(item.entry.runId)}
            onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, `/runs/${item.entry.runId}`)}
          >
            <td><OutcomeBadge outcome={item.entry.outcome} size="sm" /></td>
            <td class="cell-plan" title={displayName(item.entry)}>
              {displayName(item.entry)}
              {#if item.entry.totalAttempts && item.entry.totalAttempts > 1}
                <span class="retry-badge" title="Took {item.entry.totalAttempts} attempts">{item.entry.totalAttempts} attempts</span>
              {/if}
            </td>
            <td class="cell-steps">
              <span class="steps-passed">{item.entry.passedCount}</span>{#if item.entry.failedCount > 0}<span class="steps-separator"> / </span><span class="steps-failed">{item.entry.failedCount}</span>{/if}
            </td>
            <td class="cell-duration">{formatDuration(item.entry.durationMs)}</td>
            <td class="cell-when" title={formatTimestamp(item.entry.timestamp)}>{timeAgo(item.entry.timestamp)}</td>
          </tr>
        {:else}
          <tr
            class="run-row batch-row"
            role="link"
            tabindex="0"
            onclick={() => goToBatch(item.entry.batchId)}
            onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, `/batches/${item.entry.batchId}`)}
          >
            <td><OutcomeBadge outcome={item.entry.outcome} size="sm" /></td>
            <td class="cell-plan" title={batchLabel(item.entry)}>
              <span class="batch-icon">batch</span>
              {batchLabel(item.entry)}
            </td>
            <td class="cell-steps">
              <span class="steps-passed">{item.entry.passedRuns}</span>{#if item.entry.failedRuns > 0}<span class="steps-separator"> / </span><span class="steps-failed">{item.entry.failedRuns}</span>{/if}{#if item.entry.errorRuns > 0}<span class="steps-separator"> / </span><span class="steps-error">{item.entry.errorRuns}</span>{/if}
              <span class="steps-separator"> of {item.entry.totalRuns} runs</span>
            </td>
            <td class="cell-duration">{formatDuration(item.entry.totalDurationMs)}</td>
            <td class="cell-when" title={formatTimestamp(item.entry.timestamp)}>{timeAgo(item.entry.timestamp)}</td>
          </tr>
        {/if}
      {/each}
    </tbody>
  </table>
{/if}

<style>
  .batch-row {
    background: rgba(99, 102, 241, 0.04);
  }
  .batch-row:hover {
    background: rgba(99, 102, 241, 0.08);
  }
  .batch-icon {
    display: inline-block;
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-primary, #6366f1);
    background: rgba(99, 102, 241, 0.12);
    padding: 0.1rem 0.35rem;
    border-radius: 3px;
    margin-right: 0.4rem;
    vertical-align: baseline;
  }
  .steps-error {
    color: var(--color-warning, #f59e0b);
  }
  .retry-badge {
    display: inline-block;
    font-size: 0.65rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    color: var(--color-warning, #f59e0b);
    background: rgba(245, 158, 11, 0.12);
    padding: 0.1rem 0.35rem;
    border-radius: 3px;
    margin-left: 0.4rem;
    vertical-align: baseline;
  }
</style>
