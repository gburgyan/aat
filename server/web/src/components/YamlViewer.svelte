<script lang="ts">
  interface Props {
    content: string;
  }

  let { content }: Props = $props();

  let copyLabel = $state('Copy');

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
  <pre class="json-viewer">{content}</pre>
</div>
