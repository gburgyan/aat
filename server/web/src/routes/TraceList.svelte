<script lang="ts">
  import type { TraceListEntry } from '../lib/types';
  import { fetchTraces } from '../lib/api';
  import { navigate, encPath } from '../lib/router';
  import { formatDuration, timeAgo, formatTimestamp } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';

  let traces = $state<TraceListEntry[]>([]);
  let loading = $state(true);
  let error = $state('');

  async function load() {
    loading = true;
    error = '';
    try {
      traces = await fetchTraces();
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load traces';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load();
  });

  function goToTrace(traceId: string) {
    navigate(`/traces/${encPath(traceId)}`);
  }

  function handleRowKeydown(e: KeyboardEvent, traceId: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      goToTrace(traceId);
    }
  }

  function truncatePrompt(prompt: string, max = 80): string {
    if (prompt.length <= max) return prompt;
    return prompt.slice(0, max) + '...';
  }
</script>

{#if loading}
  <LoadingSpinner />
{:else if error}
  <div class="error-message">
    <p>{error}</p>
    <button onclick={load}>Retry</button>
  </div>
{:else if traces.length === 0}
  <div class="empty-state">
    <p>No traces found</p>
    <p class="empty-hint">Run <code>aat prompt --trace</code> to generate traces.</p>
  </div>
{:else}
  <table class="run-table">
    <thead>
      <tr>
        <th>Status</th>
        <th>Prompt</th>
        <th>Workflow</th>
        <th>LLM Calls</th>
        <th>Duration</th>
        <th>When</th>
      </tr>
    </thead>
    <tbody>
      {#each traces as trace (trace.traceId)}
        <tr
          class="run-row"
          role="link"
          tabindex="0"
          onclick={() => goToTrace(trace.traceId)}
          onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, trace.traceId)}
        >
          <td>
            {#if trace.hasError}
              <span class="trace-status-dot trace-status-error" title="Error"></span>
            {:else}
              <span class="trace-status-dot trace-status-ok" title="OK"></span>
            {/if}
          </td>
          <td class="cell-plan" title={trace.prompt}>{truncatePrompt(trace.prompt)}</td>
          <td class="cell-workflow">{trace.workflowName || '-'}</td>
          <td class="cell-steps">{trace.llmCallCount}</td>
          <td class="cell-duration">{formatDuration(trace.totalDurationMs)}</td>
          <td class="cell-when" title={formatTimestamp(trace.timestamp)}>{timeAgo(trace.timestamp)}</td>
        </tr>
      {/each}
    </tbody>
  </table>
{/if}

<style>
  .trace-status-dot {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
  }
  .trace-status-ok {
    background: var(--color-success, #22c55e);
  }
  .trace-status-error {
    background: var(--color-danger, #ef4444);
  }
  .cell-workflow {
    color: var(--color-text-muted, #6b7280);
    font-size: 0.875rem;
  }
  .empty-hint {
    color: var(--color-text-muted, #6b7280);
    font-size: 0.875rem;
    margin-top: 0.5rem;
  }
  .empty-hint code {
    background: var(--color-surface, #1e1e1e);
    padding: 0.125rem 0.375rem;
    border-radius: 3px;
    font-size: 0.8125rem;
  }
</style>
