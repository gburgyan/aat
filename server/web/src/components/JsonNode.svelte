<script lang="ts">
  import JsonNode from './JsonNode.svelte';

  interface Props {
    value: unknown;
    keyName?: string;
    indent?: number;
    defaultDepth?: number;
    depth?: number;
    path?: string;
    isLast?: boolean;
    expandAll?: number;
  }

  let {
    value,
    keyName,
    indent = 0,
    defaultDepth = 2,
    depth = 0,
    path = '',
    isLast = true,
    expandAll = 0,
  }: Props = $props();

  let manualOpen = $state<boolean | null>(null);
  let copyFeedback = $state('');

  // Reset manual override when expandAll changes
  $effect(() => {
    if (expandAll) {
      manualOpen = null;
    }
  });

  let open = $derived(
    manualOpen !== null ? manualOpen : depth < defaultDepth,
  );

  function toggle() {
    manualOpen = !open;
  }

  function handleClick(fn: () => void) {
    return (e: MouseEvent) => { fn(); };
  }

  function handleClickKey(e: KeyboardEvent, fn: () => void) {
    if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); fn(); }
  }

  let isObject = $derived(value !== null && typeof value === 'object' && !Array.isArray(value));
  let isArray = $derived(Array.isArray(value));
  let isComplex = $derived(isObject || isArray);

  let entries = $derived(
    isObject ? Object.entries(value as Record<string, unknown>) : [],
  );
  let items = $derived(isArray ? (value as unknown[]) : []);

  let preview = $derived(
    isArray
      ? `[${items.length} item${items.length !== 1 ? 's' : ''}]`
      : isObject
        ? `{${entries.length} key${entries.length !== 1 ? 's' : ''}}`
        : '',
  );

  function childPath(key: string | number): string {
    if (!path && path !== '') return String(key);
    if (typeof key === 'number') return `${path}.${key}`;
    // Escape dots in key names for gjson
    if (/[.#*?\[\]]/.test(String(key))) {
      return `${path}.${String(key).replace(/\./g, '\\.')}`;
    }
    return path ? `${path}.${key}` : String(key);
  }

  async function copyPath() {
    try {
      await navigator.clipboard.writeText(path);
      copyFeedback = 'path';
      setTimeout(() => (copyFeedback = ''), 1000);
    } catch {}
  }

  async function copyValue() {
    try {
      const text = isComplex
        ? JSON.stringify(value, null, 2)
        : value === null
          ? 'null'
          : String(value);
      await navigator.clipboard.writeText(text);
      copyFeedback = 'value';
      setTimeout(() => (copyFeedback = ''), 1000);
    } catch {}
  }

  function formatPrimitive(val: unknown): { text: string; cls: string } {
    if (val === null) return { text: 'null', cls: 'json-null' };
    if (typeof val === 'boolean') return { text: String(val), cls: 'json-bool' };
    if (typeof val === 'number') return { text: String(val), cls: 'json-number' };
    if (typeof val === 'string') {
      return { text: `"${val}"`, cls: 'json-string' };
    }
    return { text: String(val), cls: '' };
  }
</script>

<div class="json-node">
  {#if isComplex}
    <span class="json-inline">
      <span
        class="json-toggle"
        class:open
        onclick={toggle}
        role="button"
        tabindex="0"
        onkeydown={(e: KeyboardEvent) => handleClickKey(e, toggle)}
      >&#9654;</span>
      {#if keyName !== undefined}
        <span class="json-key json-clickable" role="button" tabindex="0" onclick={copyValue} onkeydown={(e: KeyboardEvent) => handleClickKey(e, copyValue)}>"{keyName}"</span><span class="json-colon">: </span>
      {/if}
      {#if open}
        <span class="json-bracket">{isArray ? '[' : '{'}</span>
      {:else}
        <span class="json-bracket json-clickable" role="button" tabindex="0" onclick={toggle} onkeydown={(e: KeyboardEvent) => handleClickKey(e, toggle)}>{isArray ? '[' : '{'}</span>
        <span class="json-collapsed-preview json-clickable" role="button" tabindex="0" onclick={toggle} onkeydown={(e: KeyboardEvent) => handleClickKey(e, toggle)}>{preview}</span>
        <span class="json-bracket json-clickable" role="button" tabindex="0" onclick={toggle} onkeydown={(e: KeyboardEvent) => handleClickKey(e, toggle)}>{isArray ? ']' : '}'}</span>
        {#if !isLast}<span class="json-comma">,</span>{/if}
      {/if}
      {#if path}
        <span class="json-path-copy" role="button" tabindex="0" onclick={copyPath} onkeydown={(e: KeyboardEvent) => handleClickKey(e, copyPath)} title="Copy path: {path}">
          {copyFeedback === 'path' ? 'copied!' : '#'}
        </span>
      {/if}
    </span>

    {#if open}
      <div class="json-children">
        {#if isArray}
          {#each items as item, i (i)}
            <JsonNode
              value={item}
              indent={indent + 1}
              {defaultDepth}
              depth={depth + 1}
              path={childPath(i)}
              isLast={i === items.length - 1}
              {expandAll}
            />
          {/each}
        {:else}
          {#each entries as [key, val], i (key)}
            <JsonNode
              value={val}
              keyName={key}
              indent={indent + 1}
              {defaultDepth}
              depth={depth + 1}
              path={childPath(key)}
              isLast={i === entries.length - 1}
              {expandAll}
            />
          {/each}
        {/if}
      </div>
      <span class="json-bracket">{isArray ? ']' : '}'}</span>{#if !isLast}<span class="json-comma">,</span>{/if}
    {/if}
  {:else}
    {@const fmt = formatPrimitive(value)}
    <span class="json-inline">
      <span class="json-toggle" style="visibility: hidden;">&#9654;</span>
      {#if keyName !== undefined}
        <span class="json-key">"{keyName}"</span><span class="json-colon">: </span>
      {/if}
      <span class="{fmt.cls} json-clickable" role="button" tabindex="0" onclick={copyValue} onkeydown={(e: KeyboardEvent) => handleClickKey(e, copyValue)} title="Click to copy value">
        {#if copyFeedback === 'value'}
          <span style="font-style: italic; color: var(--color-text-muted);">copied!</span>
        {:else}
          {fmt.text}
        {/if}
      </span>{#if !isLast}<span class="json-comma">,</span>{/if}
      {#if path}
        <span class="json-path-copy" role="button" tabindex="0" onclick={copyPath} onkeydown={(e: KeyboardEvent) => handleClickKey(e, copyPath)} title="Copy path: {path}">
          {copyFeedback === 'path' ? 'copied!' : '#'}
        </span>
      {/if}
    </span>
  {/if}
</div>
