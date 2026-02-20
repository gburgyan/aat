<script lang="ts">
  import type { SelectionDetail } from '../lib/types';
  import LlmCallPanel from './LlmCallPanel.svelte';

  interface Props {
    selections: SelectionDetail[];
  }

  let { selections }: Props = $props();

  function strategyBadgeClass(strategy: string): string {
    switch (strategy) {
      case 'llm':
        return 'badge-purple';
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
        <td class="dt-mono dt-muted">{s.sourceNode}.{s.sourceField}</td>
        <td>{s.sourceSize}</td>
        <td class="dt-mono dt-muted">
          {s.filterExpr ?? ''}
          {#if s.filterRelaxed}
            <span class="badge badge-sm badge-warning" style="margin-left: 0.25rem;">relaxed</span>
          {/if}
        </td>
        <td>{s.filteredSize}</td>
        <td><span class="badge badge-sm {strategyBadgeClass(s.strategy)}">{s.strategy}</span></td>
        <td>{s.selectedIndex}</td>
        <td class="dt-muted">{s.selectionName ?? ''}</td>
      </tr>
      {#if s.llmCall}
        <tr>
          <td colspan="8" style="padding: 0.5rem 0.65rem;">
            <LlmCallPanel call={s.llmCall} collapsible />
          </td>
        </tr>
      {/if}
    {/each}
  </tbody>
</table>
