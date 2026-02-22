<script lang="ts">
  import type { ResolutionDetail } from '../lib/types';
  import { navigate } from '../lib/router';

  interface Props {
    resolutions: ResolutionDetail[];
    runId: string;
  }

  let { resolutions, runId }: Props = $props();

  function goToStep(e: MouseEvent, stepId: string) {
    e.preventDefault();
    navigate(`/runs/${runId}/steps/${stepId}`);
  }

  function sourceBadgeClass(source: string): string {
    switch (source) {
      case 'plan_from':
      case 'expression':
      case 'named_selection':
        return 'badge-primary';
      case 'pool':
        return 'badge-warning';
      default:
        return 'badge-muted';
    }
  }

  function formatValue(val: unknown): string {
    if (val === undefined || val === null) return '';
    if (typeof val === 'string') return val;
    return JSON.stringify(val);
  }

  function constraintIcon(ok: boolean | null | undefined): string {
    if (ok === true) return '\u2713';
    if (ok === false) return '\u2717';
    return '-';
  }

  function constraintColor(ok: boolean | null | undefined): string {
    if (ok === true) return 'var(--color-success)';
    if (ok === false) return 'var(--color-error)';
    return 'var(--color-text-muted)';
  }
</script>

<table class="detail-table">
  <thead>
    <tr>
      <th>Input</th>
      <th>Source</th>
      <th>Value</th>
      <th>From</th>
      <th style="width: 2.5rem; text-align: center;">Ok</th>
      <th>Details</th>
    </tr>
  </thead>
  <tbody>
    {#each resolutions as r, i (i)}
      <tr>
        <td class="dt-mono" style="font-weight: 600;">{r.inputName}</td>
        <td>
          <span class="badge badge-sm {sourceBadgeClass(r.source)}">{r.source}</span>
        </td>
        <td class="dt-mono">{formatValue(r.finalValue ?? r.rawValue)}</td>
        <td class="dt-mono dt-muted">
          {#if r.fromStep && r.fromOutput}
            <a href="/runs/{runId}/steps/{r.fromStep}" class="step-link" onclick={(e: MouseEvent) => goToStep(e, r.fromStep!)}>{r.fromStep}</a>.{r.fromOutput}
          {:else if r.expression}
            {r.expression}
          {/if}
        </td>
        <td style="text-align: center; font-weight: 700; color: {constraintColor(r.constraintOk)};">
          {constraintIcon(r.constraintOk)}
        </td>
        <td class="dt-muted" style="font-size: 0.8rem;">
          {#if r.constraint}
            <span class="dt-mono">{r.constraint}</span>
          {/if}
          {#if r.poolSize}
            pool {(r.poolIndex ?? 0) + 1}/{r.poolSize}
          {/if}
        </td>
      </tr>
    {/each}
  </tbody>
</table>
