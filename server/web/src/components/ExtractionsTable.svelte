<script lang="ts">
  import type { ExtractionDetail } from '../lib/types';
  import { navigate } from '../lib/router';
  import JsonViewer from './JsonViewer.svelte';

  interface Props {
    extractions: ExtractionDetail[];
    runId: string;
    attempt?: number;
  }

  let { extractions, runId, attempt }: Props = $props();

  function stepHref(stepId: string): string {
    return attempt
      ? `/runs/${runId}/attempts/${attempt}/steps/${stepId}`
      : `/runs/${runId}/steps/${stepId}`;
  }

  function goToStep(e: MouseEvent, stepId: string) {
    e.preventDefault();
    navigate(stepHref(stepId));
  }

  function isSimpleValue(val: unknown): boolean {
    return val === null || val === undefined || typeof val === 'string' || typeof val === 'number' || typeof val === 'boolean';
  }

  function formatSimple(val: unknown): string {
    if (val === undefined || val === null) return '';
    return String(val);
  }

  function viaBadgeClass(via: string): string {
    return via === 'selection' ? 'badge-primary' : 'badge-muted';
  }
</script>

<table class="detail-table">
  <thead>
    <tr>
      <th>Output</th>
      <th>Value</th>
      <th>Consumed By</th>
    </tr>
  </thead>
  <tbody>
    {#each extractions as ext, i (i)}
      <tr>
        <td class="dt-mono" style="font-weight: 600;">{ext.name}</td>
        <td>
          {#if isSimpleValue(ext.value)}
            <span class="dt-mono">{formatSimple(ext.value)}</span>
          {:else}
            <JsonViewer data={ext.value} defaultDepth={1} />
          {/if}
        </td>
        <td>
          {#if ext.consumers && ext.consumers.length > 0}
            {#each ext.consumers as c, ci (ci)}
              {#if ci > 0}<br />{/if}
              <a href={stepHref(c.stepId)} class="step-link" onclick={(e: MouseEvent) => goToStep(e, c.stepId)}>{c.stepId}</a>
              <span class="dt-muted">.{c.inputName}</span>
              <span class="badge badge-sm {viaBadgeClass(c.via)}" style="margin-left: 0.25rem;">{c.via}</span>
            {/each}
          {:else}
            <span class="dt-muted">-</span>
          {/if}
        </td>
      </tr>
    {/each}
  </tbody>
</table>
