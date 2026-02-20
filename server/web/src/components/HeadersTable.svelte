<script lang="ts">
  import type { HeaderEntry } from '../lib/types';

  interface Props {
    headers: HeaderEntry[];
  }

  let { headers }: Props = $props();
</script>

<table class="detail-table">
  <thead>
    <tr>
      <th>Name</th>
      <th>Value</th>
    </tr>
  </thead>
  <tbody>
    {#each headers as h (h.name)}
      <tr>
        <td class="dt-mono dt-muted">{h.name}</td>
        <td class="dt-mono">{h.value}</td>
      </tr>
    {/each}
  </tbody>
</table>
