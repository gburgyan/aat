<script lang="ts">
  import type { RunDetail } from '../lib/types';
  import { fetchRun, fetchAttempt, renameRun, unnameRun, exportRunUrl } from '../lib/api';
  import { navigate, encPath } from '../lib/router';
  import { formatDuration, timeAgo, formatTimestamp } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import OutcomeBadge from '../components/OutcomeBadge.svelte';
  import StepTimeline from '../components/StepTimeline.svelte';

  interface Props {
    runId: string;
    attempt?: number;
  }

  let { runId, attempt }: Props = $props();

  let run = $state<RunDetail | null>(null);
  let loading = $state(true);
  let error = $state('');
  let renaming = $state(false);
  let renameInput = $state('');
  let renameError = $state('');

  async function load(id: string, attemptNum?: number) {
    loading = true;
    error = '';
    run = null;
    try {
      run = attemptNum ? await fetchAttempt(id, attemptNum) : await fetchRun(id);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load run';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load(runId, attempt);
  });

  let displayName = $derived(run?.planName || runId);
  let isSaved = $derived(!!run?.name);

  function startRename() {
    renameInput = run?.name || run?.planName || '';
    renaming = true;
    renameError = '';
  }

  function cancelRename() {
    renaming = false;
    renameError = '';
  }

  async function submitRename() {
    renameError = '';
    try {
      const result = await renameRun(runId, renameInput);
      renaming = false;
      navigate(`/runs/${encPath(result.ref)}`);
    } catch (e) {
      renameError = e instanceof Error ? e.message : 'Rename failed';
    }
  }

  async function saveWithoutNaming() {
    renameError = '';
    try {
      const result = await renameRun(runId, '');
      renaming = false;
      navigate(`/runs/${encPath(result.ref)}`);
    } catch (e) {
      renameError = e instanceof Error ? e.message : 'Save failed';
    }
  }

  async function unsave() {
    try {
      const result = await unnameRun(runId);
      navigate(`/runs/${encPath(result.ref)}`);
    } catch (e) {
      error = e instanceof Error ? e.message : 'Unsave failed';
    }
  }

  function handleRenameKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      submitRename();
    } else if (e.key === 'Escape') {
      cancelRename();
    }
  }
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
      <div class="name-actions">
        {#if !attempt}
          {#if isSaved}
            <button class="name-btn edit-btn" onclick={startRename} title="Edit name">&#9998;</button>
            <button class="name-btn unsave-btn" onclick={unsave} title="Unsave">Unsave</button>
          {:else}
            <button class="name-btn save-btn" onclick={startRename} title="Save this run">Save</button>
          {/if}
          <a href={exportRunUrl(runId)} class="name-btn export-btn" title="Export as .aar">Export</a>
        {/if}
      </div>
    </div>

    {#if renaming}
      <div class="rename-form">
        <input
          type="text"
          class="rename-input"
          bind:value={renameInput}
          onkeydown={handleRenameKeydown}
          placeholder="Enter a name..."
        />
        <button class="rename-submit" onclick={submitRename}>
          {isSaved ? 'Rename' : 'Save'}
        </button>
        {#if !isSaved}
          <button class="rename-submit rename-noname" onclick={saveWithoutNaming} title="Save without naming (keeps original ID)">
            Save as-is
          </button>
        {/if}
        <button class="rename-cancel" onclick={cancelRename}>Cancel</button>
        {#if renameError}
          <span class="rename-error">{renameError}</span>
        {/if}
      </div>
    {/if}

    {#if isSaved && run.name}
      <div class="run-saved-name">
        Saved as <strong>{run.name}</strong>
      </div>
    {/if}

    {#if attempt}
      <div class="run-attempt-nav">
        <a href="/runs/{encPath(runId)}" onclick={(e: MouseEvent) => { e.preventDefault(); navigate(`/runs/${encPath(runId)}`); }}>Back to final run</a>
        &mdash; viewing attempt #{attempt}
      </div>
    {:else if run.attempts && run.attempts.length > 0}
      <div class="run-attempt-info">
        Attempt {run.attempt} of {run.attempts.length + 1}
      </div>
    {/if}

    {#if run.batchId}
      <div class="run-batch-link">
        Part of batch <a
          href="/batches/{encPath(run.batchId)}"
          onclick={(e: MouseEvent) => { e.preventDefault(); navigate(`/batches/${encPath(run.batchId)}`); }}
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
      {#if run.layers && run.layers.length > 0}
        <div class="run-detail-meta-item">
          <span class="meta-label">Layers</span>
          <span class="meta-value">
            {#each run.layers as layer}
              <span class="layer-badge">{layer}</span>
            {/each}
          </span>
        </div>
      {/if}
    </div>
  </div>

  <StepTimeline steps={run.steps} {runId} {attempt} sectionLabel="Steps" />

  {#if run.cleanup && run.cleanup.length > 0}
    <StepTimeline steps={run.cleanup} {runId} {attempt} sectionLabel="Cleanup" muted />
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
          {#each run.attempts as att (att.attempt)}
            <tr class="attempt-row-clickable"
                onclick={() => navigate(`/runs/${encPath(runId)}/attempts/${att.attempt}`)}>
              <td>#{att.attempt}</td>
              <td><OutcomeBadge outcome={att.outcome} size="sm" /></td>
              <td class="attempt-error">{att.error || '-'}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </details>
  {/if}
{/if}

<style>
  .name-actions {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    margin-left: auto;
  }
  .name-btn {
    display: inline-flex;
    align-items: center;
    padding: 0.25rem 0.6rem;
    font-size: 0.8rem;
    font-weight: 500;
    line-height: 1.4;
    border: 1px solid var(--color-border, #374151);
    background: transparent;
    color: var(--color-text-muted, #9ca3af);
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .name-btn:hover {
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
    color: var(--color-text, #f3f4f6);
  }
  .save-btn {
    color: var(--color-success, #10b981);
    border-color: var(--color-success, #10b981);
  }
  .save-btn:hover {
    background: rgba(16, 185, 129, 0.12);
  }
  .unsave-btn {
    font-size: 0.75rem;
  }
  .export-btn {
    text-decoration: none;
    font-size: 0.75rem;
  }
  .edit-btn {
    font-size: 0.9rem;
    padding: 0.2rem 0.4rem;
  }
  .rename-form {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    margin-bottom: 0.5rem;
    flex-wrap: wrap;
  }
  .rename-input {
    padding: 0.3rem 0.5rem;
    font-size: 0.85rem;
    border: 1px solid var(--color-border, #374151);
    background: var(--color-bg-secondary, #1f2937);
    color: var(--color-text, #f3f4f6);
    border-radius: 4px;
    min-width: 200px;
  }
  .rename-input:focus {
    outline: none;
    border-color: var(--color-primary, #6366f1);
  }
  .rename-submit {
    padding: 0.3rem 0.6rem;
    font-size: 0.8rem;
    font-weight: 500;
    border: 1px solid var(--color-primary, #6366f1);
    background: var(--color-primary, #6366f1);
    color: #fff;
    border-radius: 4px;
    cursor: pointer;
  }
  .rename-submit:hover {
    background: var(--color-primary-hover, #818cf8);
  }
  .rename-noname {
    background: transparent;
    color: var(--color-text-muted, #9ca3af);
    border-color: var(--color-border, #374151);
  }
  .rename-noname:hover {
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
    color: var(--color-text, #f3f4f6);
  }
  .rename-cancel {
    padding: 0.3rem 0.6rem;
    font-size: 0.8rem;
    border: 1px solid var(--color-border, #374151);
    background: transparent;
    color: var(--color-text-muted, #9ca3af);
    border-radius: 4px;
    cursor: pointer;
  }
  .rename-cancel:hover {
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
    color: var(--color-text, #f3f4f6);
  }
  .rename-error {
    font-size: 0.8rem;
    color: var(--color-error, #ef4444);
  }
  .run-saved-name {
    font-size: 0.85rem;
    color: var(--color-success, #10b981);
    margin-bottom: 0.5rem;
  }
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
  .attempt-row-clickable {
    cursor: pointer;
  }
  .attempt-row-clickable:hover {
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
  }
  .run-attempt-nav {
    font-size: 0.85rem;
    color: var(--color-text-muted, #9ca3af);
    margin-bottom: 0.5rem;
  }
  .run-attempt-nav a {
    color: var(--color-primary, #6366f1);
    text-decoration: none;
  }
  .run-attempt-nav a:hover {
    color: var(--color-primary-hover, #818cf8);
    text-decoration: underline;
  }
  .layer-badge {
    display: inline-block;
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.03em;
    color: var(--color-primary, #6366f1);
    background: rgba(99, 102, 241, 0.12);
    padding: 0.15rem 0.4rem;
    border-radius: 3px;
    margin-right: 0.3rem;
  }
</style>
