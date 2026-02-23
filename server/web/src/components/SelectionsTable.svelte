<script lang="ts">
  import type { SelectionDetail } from '../lib/types';
  import { navigate } from '../lib/router';

  interface Props {
    selections: SelectionDetail[];
    runId: string;
    attempt?: number;
  }

  let { selections, runId, attempt }: Props = $props();

  function stepHref(stepId: string): string {
    return attempt
      ? `/runs/${runId}/attempts/${attempt}/steps/${stepId}`
      : `/runs/${runId}/steps/${stepId}`;
  }

  function goToStep(e: MouseEvent, stepId: string) {
    e.preventDefault();
    navigate(stepHref(stepId));
  }

  function strategyBadgeClass(strategy: string): string {
    switch (strategy) {
      case 'first':
      case 'last':
      case 'random':
        return 'badge-muted';
      default:
        return 'badge-primary';
    }
  }
</script>

<table class="detail-table">
  <thead>
    <tr>
      <th>Input</th>
      <th>Source</th>
      <th>Size</th>
      <th>Filter</th>
      <th>Filtered</th>
      <th>Strategy</th>
      <th>Index</th>
      <th>Name</th>
    </tr>
  </thead>
  <tbody>
    {#each selections as s, i (i)}
      <tr>
        <td class="dt-mono" style="font-weight: 600;">{s.inputName}</td>
        <td class="dt-mono dt-muted">
          {#if s.sourceStep}
            <a href={stepHref(s.sourceStep!)} class="step-link" onclick={(e: MouseEvent) => goToStep(e, s.sourceStep!)}>{s.sourceNode}</a>.{s.sourceField}
          {:else}
            {s.sourceNode}.{s.sourceField}
          {/if}
        </td>
        <td>{s.sourceSize}</td>
        <td class="dt-mono dt-muted">
          {s.filterExpr ?? ''}
        </td>
        <td>{s.filteredSize}</td>
        <td><span class="badge badge-sm {strategyBadgeClass(s.strategy)}">{s.strategy}</span></td>
        <td>{s.selectedIndex}</td>
        <td class="dt-muted">{s.selectionName ?? ''}</td>
      </tr>
    {/each}
  </tbody>
</table>
