<script lang="ts">
  interface Props {
    data: unknown;
    maxInitialLines?: number;
  }

  let { data, maxInitialLines = 30 }: Props = $props();

  let expanded = $state(false);
  let copyLabel = $state('Copy');

  function renderValue(val: unknown, indent: number): string {
    const pad = '  '.repeat(indent);
    const padInner = '  '.repeat(indent + 1);

    if (val === null) {
      return '<span class="json-null">null</span>';
    }
    if (typeof val === 'boolean') {
      return `<span class="json-bool">${val}</span>`;
    }
    if (typeof val === 'number') {
      return `<span class="json-number">${val}</span>`;
    }
    if (typeof val === 'string') {
      const escaped = val
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;');
      return `<span class="json-string">"${escaped}"</span>`;
    }
    if (Array.isArray(val)) {
      if (val.length === 0) return '[]';
      const items = val.map(
        (v) => `${padInner}${renderValue(v, indent + 1)}`,
      );
      return `[\n${items.join(',\n')}\n${pad}]`;
    }
    if (typeof val === 'object') {
      const entries = Object.entries(val as Record<string, unknown>);
      if (entries.length === 0) return '{}';
      const lines = entries.map(
        ([k, v]) => {
          const escapedKey = k.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
          return `${padInner}<span class="json-key">"${escapedKey}"</span>: ${renderValue(v, indent + 1)}`;
        },
      );
      return `{\n${lines.join(',\n')}\n${pad}}`;
    }
    return String(val);
  }

  let fullHtml = $derived(renderValue(data, 0));
  let lines = $derived(fullHtml.split('\n'));
  let totalLines = $derived(lines.length);
  let needsCollapse = $derived(totalLines > maxInitialLines);
  let displayHtml = $derived(
    needsCollapse && !expanded
      ? lines.slice(0, maxInitialLines).join('\n') + '\n...'
      : fullHtml,
  );

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
</script>

<div class="json-viewer-wrapper">
  <button class="json-copy-btn" onclick={copyToClipboard}>{copyLabel}</button>
  <pre class="json-viewer">{@html displayHtml}</pre>
  {#if needsCollapse && !expanded}
    <button class="json-show-more" onclick={() => (expanded = true)}>
      Show all ({totalLines} lines)
    </button>
  {/if}
  {#if needsCollapse && expanded}
    <button class="json-show-more" onclick={() => (expanded = false)}>
      Collapse
    </button>
  {/if}
</div>
