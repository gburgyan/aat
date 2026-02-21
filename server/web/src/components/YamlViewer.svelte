<script lang="ts">
  interface Props {
    content: string;
  }

  let { content }: Props = $props();

  let copyLabel = $state('Copy');

  function escapeHtml(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function highlightYaml(text: string): string {
    return text.split('\n').map((line) => {
      // Full-line comment
      if (/^\s*#/.test(line)) {
        return `<span class="yaml-comment">${escapeHtml(line)}</span>`;
      }

      // Anchors and tags inline
      let result = '';
      let remaining = line;

      // Key: value pattern
      const keyMatch = remaining.match(/^(\s*)([\w.\-/]+)(\s*:\s*)(.*)/);
      if (keyMatch) {
        const [, indent, key, colon, value] = keyMatch;
        result += escapeHtml(indent);
        result += `<span class="yaml-key">${escapeHtml(key)}</span>`;
        result += escapeHtml(colon);
        result += highlightValue(value);
        return result;
      }

      // Quoted key: value pattern
      const quotedKeyMatch = remaining.match(/^(\s*)("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')(\s*:\s*)(.*)/);
      if (quotedKeyMatch) {
        const [, indent, key, colon, value] = quotedKeyMatch;
        result += escapeHtml(indent);
        result += `<span class="yaml-key">${escapeHtml(key)}</span>`;
        result += escapeHtml(colon);
        result += highlightValue(value);
        return result;
      }

      // List item
      const listMatch = remaining.match(/^(\s*-\s+)(.*)/);
      if (listMatch) {
        const [, prefix, value] = listMatch;
        // Check if the value itself is a key: value
        const innerKeyMatch = value.match(/^([\w.\-/]+)(\s*:\s*)(.*)/);
        if (innerKeyMatch) {
          const [, key, colon, val] = innerKeyMatch;
          return escapeHtml(prefix) + `<span class="yaml-key">${escapeHtml(key)}</span>` + escapeHtml(colon) + highlightValue(val);
        }
        return escapeHtml(prefix) + highlightValue(value);
      }

      // Bare list item (- value)
      const bareListMatch = remaining.match(/^(\s*-\s*)(.*)/);
      if (bareListMatch) {
        const [, prefix, value] = bareListMatch;
        return escapeHtml(prefix) + highlightValue(value);
      }

      return escapeHtml(line);
    }).join('\n');
  }

  function highlightValue(val: string): string {
    const trimmed = val.trim();

    // Empty value
    if (trimmed === '') return '';

    // Inline comment at end
    const commentIdx = findInlineComment(val);
    if (commentIdx >= 0) {
      const before = val.slice(0, commentIdx);
      const comment = val.slice(commentIdx);
      return highlightValue(before) + `<span class="yaml-comment">${escapeHtml(comment)}</span>`;
    }

    // Null
    if (/^(null|~)$/i.test(trimmed)) {
      return `<span class="yaml-null">${escapeHtml(val)}</span>`;
    }

    // Boolean
    if (/^(true|false|yes|no|on|off)$/i.test(trimmed)) {
      return `<span class="yaml-bool">${escapeHtml(val)}</span>`;
    }

    // Number
    if (/^-?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(trimmed) || /^0x[\da-fA-F]+$/.test(trimmed) || /^0o[0-7]+$/.test(trimmed)) {
      return `<span class="yaml-number">${escapeHtml(val)}</span>`;
    }

    // Quoted string
    if (/^".*"$/.test(trimmed) || /^'.*'$/.test(trimmed)) {
      return `<span class="yaml-string">${escapeHtml(val)}</span>`;
    }

    // Anchor/alias
    if (/^[&*]\w/.test(trimmed)) {
      return `<span class="yaml-anchor">${escapeHtml(val)}</span>`;
    }

    // Tag
    if (/^!/.test(trimmed)) {
      return `<span class="yaml-tag">${escapeHtml(val)}</span>`;
    }

    // Unquoted string
    return `<span class="yaml-string">${escapeHtml(val)}</span>`;
  }

  function findInlineComment(val: string): number {
    // Find # that's preceded by whitespace and not inside quotes
    let inSingle = false;
    let inDouble = false;
    for (let i = 0; i < val.length; i++) {
      const ch = val[i];
      if (ch === '"' && !inSingle) inDouble = !inDouble;
      else if (ch === "'" && !inDouble) inSingle = !inSingle;
      else if (ch === '#' && !inSingle && !inDouble && i > 0 && val[i - 1] === ' ') {
        return i;
      }
    }
    return -1;
  }

  let highlighted = $derived(highlightYaml(content));

  async function copyToClipboard() {
    try {
      await navigator.clipboard.writeText(content);
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
  <pre class="json-viewer">{@html highlighted}</pre>
</div>
