<script lang="ts">
  import type { BatchDetail, BatchRunSummary, Outcome } from '../lib/types';
  import { fetchBatch, renameBatch, unnameBatch, exportBatchUrl } from '../lib/api';
  import { navigate, encPath } from '../lib/router';
  import { formatDuration, timeAgo, formatTimestamp, totalIssueCount, issueTooltip } from '../lib/format';
  import LoadingSpinner from '../components/LoadingSpinner.svelte';
  import OutcomeBadge from '../components/OutcomeBadge.svelte';

  interface Props {
    batchId: string;
  }

  let { batchId }: Props = $props();

  let batch = $state<BatchDetail | null>(null);
  let loading = $state(true);
  let error = $state('');
  let renaming = $state(false);
  let renameInput = $state('');
  let renameError = $state('');
  let collapsedGroups = $state<Set<string>>(new Set());

  function readPref(key: string, fallback: boolean): boolean {
    try {
      const v = localStorage.getItem(`aat:${key}`);
      return v === null ? fallback : v === '1';
    } catch { return fallback; }
  }

  function writePref(key: string, value: boolean) {
    try { localStorage.setItem(`aat:${key}`, value ? '1' : '0'); } catch {}
  }

  function readStringPref(key: string, fallback: string): string {
    try {
      const v = localStorage.getItem(`aat:${key}`);
      return v === null ? fallback : v;
    } catch { return fallback; }
  }

  function writeStringPref(key: string, value: string) {
    try { localStorage.setItem(`aat:${key}`, value); } catch {}
  }

  let hideSkipped = $state(readPref('hideSkipped', true));

  type BatchViewMode = 'layers' | 'tests';
  let viewMode = $state<BatchViewMode>(
    readStringPref('batchViewMode', 'layers') as BatchViewMode
  );
  function setViewMode(mode: BatchViewMode) {
    viewMode = mode;
    writeStringPref('batchViewMode', mode);
  }

  // Dimension filter state: undefined = All, null = pin to (none), string = pin to value
  let groupFilters = $state<(string | null | undefined)[]>([]);

  function setGroupFilter(gi: number, raw: string) {
    const next = [...groupFilters];
    next[gi] = raw === '__all__' ? undefined : raw === '__none__' ? null : raw;
    groupFilters = next;
  }

  function clearGroupFilters() {
    groupFilters = batch?.layerGroups?.map(() => undefined) ?? [];
  }

  function toggleHideSkipped() {
    hideSkipped = !hideSkipped;
    writePref('hideSkipped', hideSkipped);
  }

  async function load(id: string) {
    loading = true;
    error = '';
    batch = null;
    collapsedGroups = new Set();
    try {
      batch = await fetchBatch(id);
      groupFilters = batch.layerGroups?.map(() => undefined) ?? [];
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
    navigate(`/runs/${encPath(runId)}`);
  }

  function handleRowKeydown(e: KeyboardEvent, runId: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      goToRun(runId);
    }
  }

  let displayLabel = $derived(batch?.source || batchId);
  let isSaved = $derived(!!batch?.name);
  let hasPermutations = $derived(
    batch?.layerGroups != null && batch.layerGroups.length > 0
  );

  interface PermutationGroup {
    label: string;
    runs: BatchRunSummary[];
    outcome: Outcome;
  }

  let permutationGroups = $derived.by((): PermutationGroup[] => {
    if (!batch || !hasPermutations) return [];
    const groupMap = new Map<string, BatchRunSummary[]>();
    for (const run of batch.runs) {
      const key = run.permutation || '(base)';
      if (!groupMap.has(key)) groupMap.set(key, []);
      groupMap.get(key)!.push(run);
    }
    // Sort groups: (base) first, then alphabetical
    const labels = [...groupMap.keys()].sort((a, b) => {
      if (a === '(base)') return -1;
      if (b === '(base)') return 1;
      return a.localeCompare(b);
    });
    return labels.map(label => {
      const runs = groupMap.get(label)!.sort((a, b) => a.planName.localeCompare(b.planName));
      const hasError = runs.some(r => r.outcome === 'error');
      const hasFailed = runs.some(r => r.outcome === 'failed');
      const hasPassed = runs.some(r => r.outcome === 'passed');
      const allSkipped = runs.every(r => r.outcome === 'skipped');
      const outcome: Outcome = hasError ? 'error' : hasFailed ? 'failed' : allSkipped ? 'skipped' : hasPassed ? 'passed' : 'skipped';
      return { label, runs, outcome };
    });
  });

  let skippedCount = $derived(batch?.skippedRuns ?? 0);
  let effectiveTotal = $derived((batch?.totalRuns ?? 0) - skippedCount);

  interface VisiblePermutationGroup extends PermutationGroup {
    totalRunCount: number;
    skippedInGroup: number;
  }

  let visiblePermutationGroups = $derived.by((): VisiblePermutationGroup[] => {
    if (!hideSkipped) {
      return permutationGroups.map(g => ({
        ...g,
        totalRunCount: g.runs.length,
        skippedInGroup: g.runs.filter(r => r.skipped).length,
      }));
    }
    return permutationGroups
      .map(g => {
        const visible = g.runs.filter(r => !r.skipped);
        return {
          ...g,
          runs: visible,
          totalRunCount: g.runs.length,
          skippedInGroup: g.runs.length - visible.length,
        };
      })
      .filter(g => g.runs.length > 0);
  });

  let visibleFlatRuns = $derived.by(() => {
    if (!batch) return [];
    const runs = hideSkipped ? batch.runs.filter(r => !r.skipped) : [...batch.runs];
    return runs.sort((a, b) => a.planName.localeCompare(b.planName));
  });

  let permutationLabels = $derived.by((): string[] => {
    if (!batch || !hasPermutations) return [];
    const labelSet = new Set<string>();
    for (const run of batch.runs) {
      labelSet.add(run.permutation || '(base)');
    }
    return [...labelSet].sort((a, b) => {
      if (a === '(base)') return -1;
      if (b === '(base)') return 1;
      return a.localeCompare(b);
    });
  });

  function decomposePermutation(label: string, groups: string[][]): (string | null)[] {
    if (label === '(base)') return groups.map(() => null);
    const tokens = new Set(label.split(', '));
    return groups.map(g => g.find(v => tokens.has(v)) ?? null);
  }

  let groupLabels = $derived(batch?.layerGroups?.map(g => g.join(' / ')) ?? []);

  let hasActiveGroupFilters = $derived(groupFilters.some(f => f !== undefined));

  let filteredPermutationLabels = $derived.by((): string[] => {
    if (!batch?.layerGroups || !hasActiveGroupFilters) return permutationLabels;
    return permutationLabels.filter(label => {
      const vals = decomposePermutation(label, batch!.layerGroups!);
      return vals.every((v, i) => {
        const f = groupFilters[i];
        if (f === undefined) return true;  // "All"
        return f === null ? v === null : v === f;
      });
    });
  });

  interface TestMatrixCell {
    run: BatchRunSummary;
  }

  interface TestMatrixRow {
    planName: string;
    cells: Map<string, TestMatrixCell>;
    overallOutcome: Outcome;
  }

  let testMatrix = $derived.by((): TestMatrixRow[] => {
    if (!batch || !hasPermutations) return [];
    const byName = new Map<string, Map<string, TestMatrixCell>>();
    for (const run of batch.runs) {
      const perm = run.permutation || '(base)';
      if (!byName.has(run.planName)) byName.set(run.planName, new Map());
      byName.get(run.planName)!.set(perm, { run });
    }
    const rows: TestMatrixRow[] = [];
    for (const [planName, cells] of byName) {
      const runs = [...cells.values()].map(c => c.run).filter(r => !r.skipped);
      const hasError = runs.some(r => r.outcome === 'error');
      const hasFailed = runs.some(r => r.outcome === 'failed');
      const hasPassed = runs.some(r => r.outcome === 'passed');
      const allSkipped = runs.every(r => r.outcome === 'skipped');
      const overallOutcome: Outcome = hasError ? 'error' : hasFailed ? 'failed' : allSkipped ? 'skipped' : hasPassed ? 'passed' : 'skipped';
      rows.push({ planName, cells, overallOutcome });
    }
    rows.sort((a, b) => a.planName.localeCompare(b.planName));
    return rows;
  });

  let representativeMap = $derived.by((): Map<string, BatchRunSummary> => {
    if (!batch) return new Map();
    const map = new Map<string, BatchRunSummary>();
    for (const run of batch.runs) {
      if (run.skipped || !run.runId) continue;
      const key = run.permutation
        ? `${run.planName} [${run.permutation}]`
        : run.planName;
      map.set(key, run);
    }
    return map;
  });

  function toggleGroup(label: string) {
    const next = new Set(collapsedGroups);
    if (next.has(label)) {
      next.delete(label);
    } else {
      next.add(label);
    }
    collapsedGroups = next;
  }

  function startRename() {
    renameInput = batch?.name || batch?.source || '';
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
      const result = await renameBatch(batchId, renameInput);
      renaming = false;
      navigate(`/batches/${encPath(result.ref)}`);
    } catch (e) {
      renameError = e instanceof Error ? e.message : 'Rename failed';
    }
  }

  async function saveWithoutNaming() {
    renameError = '';
    try {
      const result = await renameBatch(batchId, '');
      renaming = false;
      navigate(`/batches/${encPath(result.ref)}`);
    } catch (e) {
      renameError = e instanceof Error ? e.message : 'Save failed';
    }
  }

  async function unsave() {
    try {
      const result = await unnameBatch(batchId);
      navigate(`/batches/${encPath(result.ref)}`);
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
    <button onclick={() => load(batchId)}>Retry</button>
  </div>
{:else if batch}
  <div class="run-detail-header">
    <div class="run-detail-title-row">
      <OutcomeBadge outcome={batch.outcome} size="md" />
      <h1 class="run-detail-title">{displayLabel}</h1>
      <div class="name-actions">
        {#if isSaved}
          <button class="name-btn edit-btn" onclick={startRename} title="Edit name">&#9998;</button>
          <button class="name-btn unsave-btn" onclick={unsave} title="Unsave">Unsave</button>
        {:else}
          <button class="name-btn save-btn" onclick={startRename} title="Save this batch">Save</button>
        {/if}
        <a href={exportBatchUrl(batchId)} class="name-btn export-btn" title="Export as .aab">Export</a>
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

    {#if isSaved && batch.name}
      <div class="batch-saved-name">
        Saved as <strong>{batch.name}</strong>
      </div>
    {/if}

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
          <span class="steps-separator">of&nbsp;</span>{effectiveTotal}
          {#if skippedCount > 0}<span class="steps-skipped"> ({skippedCount} skipped)</span>{/if}
        </span>
      </div>
      {#if batch.toolVersion}
        <div class="run-detail-meta-item">
          <span class="meta-label">Tool</span>
          <span class="meta-value meta-mono">{batch.toolVersion}</span>
        </div>
      {/if}
      {#if batch.layers && batch.layers.length > 0}
        <div class="run-detail-meta-item">
          <span class="meta-label">Layers</span>
          <span class="meta-value">
            {#each batch.layers as layer}
              <span class="layer-badge">{layer}</span>
            {/each}
          </span>
        </div>
      {/if}
      {#if totalIssueCount(batch.issues) > 0}
        <div class="run-detail-meta-item">
          <span class="meta-label">Issues</span>
          <span class="meta-value">
            <span class="issues-badge" title={issueTooltip(batch.issues)}>
              {totalIssueCount(batch.issues)} issues
            </span>
          </span>
        </div>
      {/if}
    </div>
  </div>

  {#if skippedCount > 0 || hasPermutations}
    <div class="batch-controls">
      {#if skippedCount > 0}
        <div class="skipped-toggle">
          <label class="toggle-label">
            <input type="checkbox" checked={hideSkipped} onchange={toggleHideSkipped} />
            Hide skipped runs ({skippedCount})
          </label>
        </div>
      {/if}
      {#if hasPermutations}
        <div class="view-toggle" role="group" aria-label="View mode">
          <button
            class="view-toggle-btn"
            class:active={viewMode === 'layers'}
            onclick={() => setViewMode('layers')}
          >By Layers</button>
          <button
            class="view-toggle-btn"
            class:active={viewMode === 'tests'}
            onclick={() => setViewMode('tests')}
          >By Test</button>
        </div>
      {/if}
    </div>
  {/if}

  {#if batch.runs.length === 0}
    <div class="empty-state">
      <p>No runs in this batch</p>
    </div>
  {:else if hasPermutations && viewMode === 'layers'}
    {#each visiblePermutationGroups as group (group.label)}
      <div class="permutation-group">
        <button
          class="permutation-header"
          onclick={() => toggleGroup(group.label)}
        >
          <span class="permutation-toggle">{collapsedGroups.has(group.label) ? '\u25b6' : '\u25bc'}</span>
          <OutcomeBadge outcome={group.outcome} size="sm" />
          <span class="permutation-label">{group.label}</span>
          <span class="permutation-count">
            {group.runs.length} runs {#if group.skippedInGroup > 0}({group.skippedInGroup} skipped){/if}
          </span>
        </button>
        {#if !collapsedGroups.has(group.label)}
          <table class="run-table permutation-table">
            <thead>
              <tr>
                <th>Outcome</th>
                <th>Plan</th>
                <th>Steps</th>
                <th>Duration</th>
              </tr>
            </thead>
            <tbody>
              {#each group.runs as run, ri (run.skipped || !run.runId ? `nolink-${ri}` : run.runId)}
                {#if run.skipped || !run.runId}
                  <tr class="run-row run-row-skipped">
                    <td><OutcomeBadge outcome={run.outcome || "skipped"} size="sm" /></td>
                    <td class="cell-plan cell-dimmed" title={run.planName}>
                      {run.planName}
                      {#if run.duplicateOf}
                        <span class="dedup-info" title="Duplicate of {run.duplicateOf}">duplicate of {run.duplicateOf}</span>
                      {/if}
                      {#if run.error}
                        <span class="dedup-info">{run.error}</span>
                      {/if}
                    </td>
                    <td class="cell-steps cell-dimmed">&mdash;</td>
                    <td class="cell-duration cell-dimmed">&mdash;</td>
                  </tr>
                {:else}
                  {@const permLayers = group.label === '(base)' ? new Set<string>() : new Set(group.label.split(', '))}
                  {@const extraLayers = (run.layers ?? []).filter(l => !permLayers.has(l))}
                  <tr
                    class="run-row"
                    role="link"
                    tabindex="0"
                    onclick={() => goToRun(run.runId)}
                    onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, run.runId)}
                  >
                    <td><OutcomeBadge outcome={run.outcome} size="sm" /></td>
                    <td class="cell-plan" title={run.planName || run.runId}>
                      {run.planName || run.runId}
                      {#if run.attempts && run.attempts > 1}
                        <span class="retry-badge" title="Took {run.attempts} attempts">{run.attempts} attempts</span>
                      {/if}
                      {#if extraLayers.length > 0}
                        {#each extraLayers as layer}
                          <span class="layer-badge" title="Layer: {layer}">{layer}</span>
                        {/each}
                      {/if}
                      {#if totalIssueCount(run.issues) > 0}
                        <span class="issues-badge" title={issueTooltip(run.issues)}>
                          {totalIssueCount(run.issues)} issues
                        </span>
                      {/if}
                    </td>
                    <td class="cell-steps">
                      <span class="steps-passed">{run.passedCount}</span>{#if run.failedCount > 0}<span class="steps-separator"> / </span><span class="steps-failed">{run.failedCount}</span>{/if}
                      <span class="steps-separator"> of {run.stepCount}</span>
                    </td>
                    <td class="cell-duration">{formatDuration(run.durationMs)}</td>
                  </tr>
                {/if}
              {/each}
            </tbody>
          </table>
        {/if}
      </div>
    {/each}
  {:else if hasPermutations && viewMode === 'tests'}
    {#if batch.layerGroups && batch.layerGroups.length > 0}
      <div class="dimension-filters">
        {#each batch.layerGroups as group, gi}
          <div class="dimension-filter">
            <label class="dimension-filter-label" for="group-filter-{gi}">{groupLabels[gi]}</label>
            <select
              id="group-filter-{gi}"
              class="dimension-filter-select"
              value={groupFilters[gi] === undefined ? '__all__' : groupFilters[gi] === null ? '__none__' : groupFilters[gi]}
              onchange={(e: Event) => setGroupFilter(gi, (e.target as HTMLSelectElement).value)}
            >
              <option value="__all__">All</option>
              <option value="__none__">(none)</option>
              {#each group as val}
                <option value={val}>{val}</option>
              {/each}
            </select>
          </div>
        {/each}
        {#if hasActiveGroupFilters}
          <button class="dimension-clear-btn" onclick={clearGroupFilters}>Clear filters</button>
          <span class="dimension-count">{filteredPermutationLabels.length} of {permutationLabels.length} permutations</span>
        {/if}
      </div>
    {/if}
    {#if filteredPermutationLabels.length === 0}
      <div class="empty-state">
        <p>No permutations match the current filters</p>
        <button class="dimension-clear-btn" onclick={clearGroupFilters}>Clear filters</button>
      </div>
    {:else}
      <div class="test-matrix-wrapper">
        <table class="run-table test-matrix">
          <thead>
            <tr>
              <th class="matrix-test-name matrix-fixed-header">Test</th>
              <th class="matrix-fixed-header">Overall</th>
              {#each filteredPermutationLabels as perm}
                <th class="matrix-perm-header" title={perm}><span>{perm}</span></th>
              {/each}
            </tr>
          </thead>
          <tbody>
            {#each testMatrix as row (row.planName)}
              <tr class="matrix-row">
                <td class="matrix-test-name" title={row.planName}>{row.planName}</td>
                <td><OutcomeBadge outcome={row.overallOutcome} size="sm" /></td>
                {#each filteredPermutationLabels as perm}
                  {@const cell = row.cells.get(perm)}
                  <td class="matrix-cell">
                    {#if !cell}
                      <span class="matrix-empty">&mdash;</span>
                    {:else if cell.run.skipped || !cell.run.runId}
                      {@const rep = cell.run.duplicateOf ? representativeMap.get(cell.run.duplicateOf) : undefined}
                      {#if hideSkipped && rep}
                        <span class="matrix-proxy" title="Duplicate of {cell.run.duplicateOf}">
                          <OutcomeBadge outcome={rep.outcome} size="sm" />
                        </span>
                      {:else}
                        <span class="matrix-skipped" title={cell.run.duplicateOf ? `Duplicate of ${cell.run.duplicateOf}` : cell.run.error || 'Skipped'}>
                          <OutcomeBadge outcome={cell.run.outcome || 'skipped'} size="sm" />
                        </span>
                      {/if}
                    {:else}
                      <button
                        class="matrix-cell-btn"
                        onclick={() => goToRun(cell.run.runId)}
                        title="{cell.run.planName} — {perm}"
                      >
                        <OutcomeBadge outcome={cell.run.outcome} size="sm" />
                        {#if cell.run.attempts && cell.run.attempts > 1}
                          <span class="matrix-retry" title="Took {cell.run.attempts} attempts">!</span>
                        {/if}
                      </button>
                    {/if}
                  </td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
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
        {#each visibleFlatRuns as run, ri (run.skipped || !run.runId ? `nolink-${ri}` : run.runId)}
          {#if run.skipped || !run.runId}
            <tr class="run-row run-row-skipped">
              <td><OutcomeBadge outcome={run.outcome || "skipped"} size="sm" /></td>
              <td class="cell-plan cell-dimmed" title={run.planName}>
                {run.planName}
                {#if run.duplicateOf}
                  <span class="dedup-info" title="Duplicate of {run.duplicateOf}">duplicate of {run.duplicateOf}</span>
                {/if}
                {#if run.error}
                  <span class="dedup-info">{run.error}</span>
                {/if}
              </td>
              <td class="cell-steps cell-dimmed">&mdash;</td>
              <td class="cell-duration cell-dimmed">&mdash;</td>
            </tr>
          {:else}
            <tr
              class="run-row"
              role="link"
              tabindex="0"
              onclick={() => goToRun(run.runId)}
              onkeydown={(e: KeyboardEvent) => handleRowKeydown(e, run.runId)}
            >
              <td><OutcomeBadge outcome={run.outcome} size="sm" /></td>
              <td class="cell-plan" title={run.planName || run.runId}>
                {run.planName || run.runId}
                {#if run.attempts && run.attempts > 1}
                  <span class="retry-badge" title="Took {run.attempts} attempts">{run.attempts} attempts</span>
                {/if}
                {#if run.layers && run.layers.length > 0}
                  {#each run.layers as layer}
                    <span class="layer-badge" title="Layer: {layer}">{layer}</span>
                  {/each}
                {/if}
                {#if totalIssueCount(run.issues) > 0}
                  <span class="issues-badge" title={issueTooltip(run.issues)}>
                    {totalIssueCount(run.issues)} issues
                  </span>
                {/if}
              </td>
              <td class="cell-steps">
                <span class="steps-passed">{run.passedCount}</span>{#if run.failedCount > 0}<span class="steps-separator"> / </span><span class="steps-failed">{run.failedCount}</span>{/if}
                <span class="steps-separator"> of {run.stepCount}</span>
              </td>
              <td class="cell-duration">{formatDuration(run.durationMs)}</td>
            </tr>
          {/if}
        {/each}
      </tbody>
    </table>
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
  .batch-saved-name {
    font-size: 0.85rem;
    color: var(--color-success, #10b981);
    margin-bottom: 0.5rem;
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
  .issues-badge {
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
  .permutation-group {
    margin-bottom: 0.75rem;
  }
  .permutation-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.5rem 0.75rem;
    background: var(--color-bg-secondary, #1f2937);
    border: 1px solid var(--color-border, #374151);
    border-radius: 6px;
    cursor: pointer;
    color: var(--color-text, #f3f4f6);
    font-size: 0.9rem;
    text-align: left;
    transition: background 0.15s ease;
  }
  .permutation-header:hover {
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
  }
  .permutation-toggle {
    font-size: 0.7rem;
    color: var(--color-text-muted, #9ca3af);
    width: 1em;
    text-align: center;
  }
  .permutation-label {
    font-weight: 600;
  }
  .permutation-count {
    font-size: 0.75rem;
    color: var(--color-text-muted, #9ca3af);
    margin-left: auto;
  }
  .permutation-table {
    margin-top: 0;
    border-top: none;
    border-top-left-radius: 0;
    border-top-right-radius: 0;
  }
  .run-row-skipped {
    cursor: default;
  }
  .run-row-skipped:hover {
    background: inherit;
  }
  .cell-dimmed {
    color: var(--color-text-muted, #9ca3af);
    opacity: 0.6;
  }
  .dedup-info {
    display: inline-block;
    font-size: 0.7rem;
    color: var(--color-text-muted, #9ca3af);
    margin-left: 0.4rem;
    font-style: italic;
  }
  .steps-skipped {
    color: var(--color-text-muted, #9ca3af);
  }
  .toggle-label {
    display: inline-flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.8rem;
    color: var(--color-text-muted, #9ca3af);
    cursor: pointer;
  }
  .toggle-label input[type="checkbox"] {
    accent-color: var(--color-primary, #6366f1);
    cursor: pointer;
  }
  .batch-controls {
    display: flex;
    align-items: center;
    gap: 1rem;
    margin-bottom: 0.75rem;
    flex-wrap: wrap;
  }
  .view-toggle {
    display: inline-flex;
    border: 1px solid var(--color-border, #374151);
    border-radius: 4px;
    overflow: hidden;
  }
  .view-toggle-btn {
    padding: 0.25rem 0.6rem;
    font-size: 0.8rem;
    font-weight: 500;
    line-height: 1.4;
    border: none;
    background: transparent;
    color: var(--color-text-muted, #9ca3af);
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .view-toggle-btn:not(:last-child) {
    border-right: 1px solid var(--color-border, #374151);
  }
  .view-toggle-btn:hover {
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
    color: var(--color-text, #f3f4f6);
  }
  .view-toggle-btn.active {
    background: var(--color-primary, #6366f1);
    color: #fff;
  }
  .view-toggle-btn:focus-visible {
    outline: 2px solid var(--color-primary, #6366f1);
    outline-offset: -2px;
  }
  .test-matrix-wrapper {
    overflow-x: auto;
  }
  .test-matrix {
    white-space: nowrap;
  }
  .test-matrix .matrix-test-name {
    position: sticky;
    left: 0;
    background: var(--color-bg, #111827);
    z-index: 1;
    max-width: 260px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .test-matrix thead .matrix-test-name {
    background: var(--color-bg, #111827);
  }
  .matrix-fixed-header {
    vertical-align: bottom;
  }
  .matrix-perm-header {
    vertical-align: bottom;
    height: 160px;
    padding: 0.25rem;
    position: relative;
  }
  .matrix-perm-header span {
    display: block;
    transform: rotate(-45deg);
    transform-origin: bottom left;
    white-space: nowrap;
    font-size: 0.75rem;
    width: max-content;
    position: absolute;
    bottom: 0.4rem;
    left: 50%;
  }
  .matrix-cell {
    text-align: center;
    padding: 0.25rem 0.4rem;
  }
  .matrix-cell-btn {
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    background: none;
    border: 1px solid transparent;
    border-radius: 4px;
    padding: 0.15rem 0.3rem;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .matrix-cell-btn:hover {
    border-color: var(--color-border, #374151);
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
  }
  .matrix-cell-btn:focus-visible {
    outline: 2px solid var(--color-primary, #6366f1);
    outline-offset: 1px;
  }
  .matrix-proxy {
    opacity: 0.35;
  }
  .matrix-empty {
    color: var(--color-text-muted, #9ca3af);
    opacity: 0.4;
  }
  .matrix-skipped {
    opacity: 0.5;
  }
  .matrix-retry {
    font-size: 0.7rem;
    font-weight: 700;
    color: var(--color-warning, #f59e0b);
  }
  .matrix-row:hover .matrix-test-name {
    color: var(--color-text, #f3f4f6);
  }
  .dimension-filters {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    margin-bottom: 0.75rem;
    flex-wrap: wrap;
  }
  .dimension-filter {
    display: flex;
    align-items: center;
    gap: 0.35rem;
  }
  .dimension-filter-label {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--color-text-muted, #9ca3af);
    white-space: nowrap;
  }
  .dimension-filter-select {
    padding: 0.2rem 0.4rem;
    font-size: 0.8rem;
    border: 1px solid var(--color-border, #374151);
    background: var(--color-bg-secondary, #1f2937);
    color: var(--color-text, #f3f4f6);
    border-radius: 4px;
    cursor: pointer;
  }
  .dimension-filter-select:focus {
    outline: none;
    border-color: var(--color-primary, #6366f1);
  }
  .dimension-clear-btn {
    padding: 0.2rem 0.5rem;
    font-size: 0.75rem;
    font-weight: 500;
    border: 1px solid var(--color-border, #374151);
    background: transparent;
    color: var(--color-text-muted, #9ca3af);
    border-radius: 4px;
    cursor: pointer;
    transition: all 0.15s ease;
  }
  .dimension-clear-btn:hover {
    background: var(--color-bg-hover, rgba(99, 102, 241, 0.08));
    color: var(--color-text, #f3f4f6);
  }
  .dimension-count {
    font-size: 0.75rem;
    color: var(--color-text-muted, #9ca3af);
  }
</style>
