<script lang="ts">
  import JsonNode from './JsonNode.svelte';

  interface Props {
    data: unknown;
    defaultDepth?: number;
  }

  let { data, defaultDepth = 2 }: Props = $props();

  let copyLabel = $state('Copy');
  let expandAllCounter = $state(0);
  let collapseMode = $state(false);

  async function copyToClipboard() {
    try {
      await navigator.clipboard.writeText(
        typeof data === 'string' ? data : JSON.stringify(data, null, 2),
      );
      copyLabel = 'Copied!';
      setTimeout(() => (copyLabel = 'Copy'), 1500);
    } catch {
      copyLabel = 'Failed';
      setTimeout(() => (copyLabel = 'Copy'), 1500);
    }
  }

  function expandAll() {
    collapseMode = false;
    expandAllCounter++;
  }

  function collapseAll() {
    collapseMode = true;
    expandAllCounter++;
  }

  // Use a key to force re-render on expand/collapse all
  let treeKey = $derived(`${expandAllCounter}-${collapseMode}`);
  let effectiveDepth = $derived(collapseMode ? 0 : 100);
</script>

<div class="json-viewer-wrapper">
  <div class="json-toolbar">
    <button class="json-toolbar-btn" onclick={expandAll}>Expand</button>
    <button class="json-toolbar-btn" onclick={collapseAll}>Collapse</button>
    <button class="json-toolbar-btn" onclick={copyToClipboard}>{copyLabel}</button>
  </div>
  <pre class="json-viewer">{#key treeKey}<JsonNode
      value={data}
      defaultDepth={collapseMode ? 0 : (expandAllCounter > 0 && !collapseMode) ? 100 : defaultDepth}
      expandAll={expandAllCounter}
    />{/key}</pre>
</div>
