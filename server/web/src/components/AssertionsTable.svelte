<script lang="ts">
  import type { ValidationDetail } from '../lib/types';

  interface Props {
    validation: ValidationDetail;
  }

  let { validation }: Props = $props();

  let results = $derived(validation.results ?? []);
  let passedCount = $derived(results.filter((r) => r.passed).length);
  let totalCount = $derived(results.length);

  function statusIcon(r: { passed: boolean; skipped?: boolean }): string {
    if (r.skipped) return '-';
    return r.passed ? '\u2713' : '\u2717';
  }

  function rowClass(r: { passed: boolean; skipped?: boolean }): string {
    if (r.skipped) return 'assertion-row-skipped';
    return r.passed ? 'assertion-row-passed' : 'assertion-row-failed';
  }
</script>

<p style="margin-bottom: 0.75rem; font-size: 0.85rem;">
  <span class="steps-passed">{passedCount}</span>
  <span class="steps-separator">of&nbsp;</span>
  {totalCount} assertions passed
</p>

<table class="detail-table">
  <thead>
    <tr>
      <th style="width: 2.5rem;"></th>
      <th>Type</th>
      <th>Message</th>
      <th>Path</th>
      <th>Expression</th>
    </tr>
  </thead>
  <tbody>
    {#each results as r, i (i)}
      <tr class={rowClass(r)}>
        <td style="text-align: center; font-weight: 700; color: {r.skipped ? 'var(--color-text-muted)' : r.passed ? 'var(--color-success)' : 'var(--color-error)'};">
          {statusIcon(r)}
        </td>
        <td><span class="badge badge-sm badge-muted">{r.type}</span></td>
        <td>{r.message}</td>
        <td class="dt-mono dt-muted">{r.path ?? ''}</td>
        <td class="dt-mono dt-muted">{r.expr ?? ''}</td>
      </tr>
    {/each}
  </tbody>
</table>
