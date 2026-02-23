<script lang="ts">
  import type { RunListEntry, BatchListEntry, UnifiedListEntry } from '../lib/types';
  import { fetchRuns, fetchBatches, exportRunUrl, exportBatchUrl, importArchive } from '../lib/api';
  import { navigate, encPath } from '../lib/router';
  import { formatDuration, timeAgo, formatTimestamp } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import OutcomeBadge from '../components/OutcomeBadge.svelte';

  let items = $state<UnifiedListEntry[]>([]);
  let loading = $state(true);
  let error = $state('');
  let savedFilter = $state(false);
  let importing = $state(false);
  let importError = $state('');
  let fileInput: HTMLInputElement;

  async function load() {
    loading = true;
    error = '';
    try {
      const saved = savedFilter || undefined;
      const [runs, batches] = await Promise.all([fetchRuns(50, saved), fetchBatches(50, saved)]);

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
    // Re-run when filter changes.
    savedFilter;
    load();
  });

  function toggleFilter(saved: boolean) {
    savedFilter = saved;
  }

  function goToRun(runId: string) {
    navigate(`/runs/${encPath(runId)}`);
  }

  function goToBatch(batchId: string) {
    navigate(`/batches/${encPath(batchId)}`);
  }

  function handleRowKeydown(e: KeyboardEvent, path: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      navigate(path);
    }
  }

  function triggerImport() {
    fileInput?.click();
  }

  async function handleFileSelect(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    input.value = '';

    importing = true;
    importError = '';
    try {
      const result = await importArchive(file);
      await load();
      if (result.type === 'batch') {
        navigate(`/batches/${encPath(result.ref)}`);
      } else {
        navigate(`/runs/${encPath(result.ref)}`);
      }
    } catch (e) {
      importError = e instanceof Error ? e.message : 'Import failed';
    } finally {
      importing = false;
    }
  }

  function displayName(run: RunListEntry): string {
    return run.name || run.planName || run.runId;
  }

  function batchLabel(batch: BatchListEntry): string {
    return batch.name || batch.source || batch.batchId;
  }
</script>

<div class="filter-bar">
  <button
    class="filter-btn"
    class:active={!savedFilter}
    onclick={() => toggleFilter(false)}
  >All</button>
  <button
    class="filter-btn"
    class:active={savedFilter}
    onclick={() => toggleFilter(true)}
  >Saved</button>
  <div class="filter-spacer"></div>
  <button class="filter-btn import-btn" onclick={triggerImport} disabled={importing}>
    {importing ? 'Importing...' : 'Import'}
  </button>
  <input
    bind:this={fileInput}
    type="file"
    accept=".aar,.aab"
    style="display:none"
    onchange={handleFileSelect}
  />
</div>
{#if importError}
  <div class="import-error">{importError}</div>
{/if}

{#if loading}
  <LoadingSpinner />
{:else if error}
  <div class="error-message">
    <p>{error}</p>
    <button onclick={load}>Retry</button>
  </div>
{:else if items.length === 0}
  <div class="empty-state">
    {#if savedFilter}
      <p>No saved runs</p>
      <p class="empty-hint">Name a run from its detail page to save it.</p>
    {:else}
      <p>No runs found</p>
    {/if}
  </div>
{:else}
  <table class="run-table">
    <thead>
      <tr>
        <th>Outcome</th>
        <th>Name</th>
        <th>Steps</th>
        <th>Duration</th>
        <th>When</th>
        <th class="col-actions"></th>
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
            onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, `/runs/${encPath(item.entry.runId)}`)}
          >
            <td><OutcomeBadge outcome={item.entry.outcome} size="sm" /></td>
            <td class="cell-plan" title={displayName(item.entry)}>
              {#if item.entry.name}
                <span class="saved-name">{item.entry.name}</span>
                {#if item.entry.planName}
                  <span class="plan-subtitle">{item.entry.planName}</span>
                {/if}
              {:else}
                {displayName(item.entry)}
              {/if}
              {#if item.entry.totalAttempts && item.entry.totalAttempts > 1}
                <span class="retry-badge" title="Took {item.entry.totalAttempts} attempts">{item.entry.totalAttempts} attempts</span>
              {/if}
              {#if item.entry.layers && item.entry.layers.length > 0}
                {#each item.entry.layers as layer}
                  <span class="layer-badge" title="Layer: {layer}">{layer}</span>
                {/each}
              {/if}
            </td>
            <td class="cell-steps">
              <span class="steps-passed">{item.entry.passedCount}</span>{#if item.entry.failedCount > 0}<span class="steps-separator"> / </span><span class="steps-failed">{item.entry.failedCount}</span>{/if}
            </td>
            <td class="cell-duration">{formatDuration(item.entry.durationMs)}</td>
            <td class="cell-when" title={formatTimestamp(item.entry.timestamp)}>{timeAgo(item.entry.timestamp)}</td>
            <td class="cell-actions">
              <a
                href={exportRunUrl(item.entry.runId)}
                class="download-btn"
                title="Export run"
                onclick={(e: MouseEvent) => e.stopPropagation()}
              >&#8595;</a>
            </td>
          </tr>
        {:else}
          <tr
            class="run-row batch-row"
            role="link"
            tabindex="0"
            onclick={() => goToBatch(item.entry.batchId)}
            onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, `/batches/${encPath(item.entry.batchId)}`)}
          >
            <td><OutcomeBadge outcome={item.entry.outcome} size="sm" /></td>
            <td class="cell-plan" title={batchLabel(item.entry)}>
              <span class="batch-icon">batch</span>
              {#if item.entry.name}
                <span class="saved-name">{item.entry.name}</span>
                {#if item.entry.source}
                  <span class="plan-subtitle">{item.entry.source}</span>
                {/if}
              {:else}
                {batchLabel(item.entry)}
              {/if}
              {#if item.entry.layers && item.entry.layers.length > 0}
                {#each item.entry.layers as layer}
                  <span class="layer-badge" title="Layer: {layer}">{layer}</span>
                {/each}
              {/if}
            </td>
            <td class="cell-steps">
              <span class="steps-passed">{item.entry.passedRuns}</span>{#if item.entry.failedRuns > 0}<span class="steps-separator"> / </span><span class="steps-failed">{item.entry.failedRuns}</span>{/if}{#if item.entry.errorRuns > 0}<span class="steps-separator"> / </span><span class="steps-error">{item.entry.errorRuns}</span>{/if}
              <span class="steps-separator"> of {item.entry.totalRuns} runs</span>
            </td>
            <td class="cell-duration">{formatDuration(item.entry.totalDurationMs)}</td>
            <td class="cell-when" title={formatTimestamp(item.entry.timestamp)}>{timeAgo(item.entry.timestamp)}</td>
            <td class="cell-actions">
              <a
                href={exportBatchUrl(item.entry.batchId)}
                class="download-btn"
                title="Export batch"
                onclick={(e: MouseEvent) => e.stopPropagation()}
              >&#8595;</a>
            </td>
          </tr>
        {/if}
      {/each}
    </tbody>
  </table>
{/if}

<style>
  .filter-bar {
    display: flex;
    gap: 0.25rem;
    margin-bottom: 0.75rem;
    align-items: center;
  }
  .filter-spacer {
    flex: 1;
  }
  .import-btn {
    margin-left: auto;
  }
  .import-error {
    font-size: 0.8rem;
    color: var(--color-error, #ef4444);
    margin-bottom: 0.5rem;
  }
  .filter-btn {
    padding: 0.3rem 0.75rem;
    font-size: 0.8rem;
    font-weight: 500;
    border: 1px solid var(--color-border, #374151);
    background: transparent;
    color: var(--color-text-muted, #9ca3af);
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .filter-btn:hover {
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
    color: var(--color-text, #f3f4f6);
  }
  .filter-btn.active {
    background: var(--color-primary, #6366f1);
    color: #fff;
    border-color: var(--color-primary, #6366f1);
  }
  .empty-hint {
    font-size: 0.85rem;
    color: var(--color-text-muted, #9ca3af);
    margin-top: 0.25rem;
  }
  .saved-name {
    font-weight: 600;
    color: var(--color-text, #f3f4f6);
  }
  .plan-subtitle {
    display: block;
    font-size: 0.75rem;
    color: var(--color-text-muted, #9ca3af);
    margin-top: 0.1rem;
  }
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
  .layer-badge {
    display: inline-block;
    font-size: 0.65rem;
    font-weight: 600;
    letter-spacing: 0.03em;
    color: var(--color-primary, #6366f1);
    background: rgba(99, 102, 241, 0.12);
    padding: 0.1rem 0.35rem;
    border-radius: 3px;
    margin-left: 0.4rem;
    vertical-align: baseline;
  }
  .col-actions {
    width: 2rem;
  }
  .cell-actions {
    text-align: center;
    width: 2rem;
  }
  .download-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.5rem;
    height: 1.5rem;
    font-size: 1rem;
    color: var(--color-text-muted, #9ca3af);
    text-decoration: none;
    border-radius: 3px;
    transition: all 0.15s ease;
  }
  .download-btn:hover {
    color: var(--color-primary, #6366f1);
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
  }
</style>
