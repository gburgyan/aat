<script lang="ts">
  import type { RunListEntry } from '../lib/types';
  import { fetchRuns } from '../lib/api';
  import { navigate } from '../lib/router';
  import { formatDuration, timeAgo, formatTimestamp } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import OutcomeBadge from '../components/OutcomeBadge.svelte';

  let runs = $state<RunListEntry[]>([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      runs = await fetchRuns();
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

  function handleRowKeydown(e: KeyboardEvent, runId: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      goToRun(runId);
    }
  }

  function displayName(run: RunListEntry): string {
    return run.planName || run.runId;
  }
</script>

{#if loading}
  <LoadingSpinner />
{:else if error}
  <div class="error-message">
    <p>{error}</p>
    <button onclick={load}>Retry</button>
  </div>
{:else if runs.length === 0}
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
      {#each runs as run (run.runId)}
        <tr
          class="run-row"
          role="link"
          tabindex="0"
          onclick={() => goToRun(run.runId)}
          onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, run.runId)}
        >
          <td><OutcomeBadge outcome={run.outcome} size="sm" /></td>
          <td class="cell-plan" title={displayName(run)}>{displayName(run)}</td>
          <td class="cell-steps">
            <span class="steps-passed">{run.passedCount}</span>{#if run.failedCount > 0}<span class="steps-separator"> / </span><span class="steps-failed">{run.failedCount}</span>{/if}
          </td>
          <td class="cell-duration">{formatDuration(run.durationMs)}</td>
          <td class="cell-when" title={formatTimestamp(run.timestamp)}>{timeAgo(run.timestamp)}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}
