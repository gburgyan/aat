<script lang="ts">
  import type { LLMCallDetail } from '../lib/types';
  import { formatDuration } from '../lib/format';

  interface Props {
    call: LLMCallDetail;
    collapsible?: boolean;
  }

  let { call, collapsible = false }: Props = $props();

  let copyStates: Record<string, string> = $state({});

  function roleClass(role: string): string {
    switch (role) {
      case 'system':
        return 'badge-muted';
      case 'user':
        return 'badge-primary';
      case 'assistant':
        return 'badge-success';
      default:
        return 'badge-muted';
    }
  }

  async function copyText(key: string, text: string) {
    try {
      await navigator.clipboard.writeText(text);
      copyStates[key] = 'Copied!';
      setTimeout(() => (copyStates[key] = 'Copy'), 1500);
    } catch {
      copyStates[key] = 'Failed';
      setTimeout(() => (copyStates[key] = 'Copy'), 1500);
    }
  }
</script>

{#snippet content()}
  <div class="llm-metrics">
    <div class="step-detail-meta-item">
      <span class="meta-label">Model</span>
      <span class="meta-value dt-mono">{call.model}</span>
    </div>
    <div class="step-detail-meta-item">
      <span class="meta-label">Duration</span>
      <span class="meta-value dt-mono">{formatDuration(call.durationMs)}</span>
    </div>
    <div class="step-detail-meta-item">
      <span class="meta-label">Input Tokens</span>
      <span class="meta-value dt-mono">{call.inputTokens.toLocaleString()}</span>
    </div>
    <div class="step-detail-meta-item">
      <span class="meta-label">Output Tokens</span>
      <span class="meta-value dt-mono">{call.outputTokens.toLocaleString()}</span>
    </div>
    {#if call.finishReason}
      <div class="step-detail-meta-item">
        <span class="meta-label">Finish</span>
        <span class="meta-value">{call.finishReason}</span>
      </div>
    {/if}
  </div>

  {#if call.error}
    <div class="step-detail-error" style="margin-bottom: 0.75rem;">{call.error}</div>
  {/if}

  {#if call.messages && call.messages.length > 0}
    <h4 class="section-heading">Messages</h4>
    {#each call.messages as msg, i (i)}
      <div class="llm-message">
        <div class="llm-message-role" style="background: var(--color-surface);">
          <span class="badge badge-sm {roleClass(msg.role)}">{msg.role}</span>
        </div>
        <div class="llm-pre-wrapper">
          <button class="llm-copy-btn" onclick={() => copyText(`msg-${i}`, msg.content)}>{copyStates[`msg-${i}`] || 'Copy'}</button>
          <pre class="llm-message-content">{msg.content}</pre>
        </div>
      </div>
    {/each}
  {/if}

  {#if call.response}
    <h4 class="section-heading">Response</h4>
    <div class="llm-pre-wrapper">
      <button class="llm-copy-btn" onclick={() => copyText('response', call.response)}>{copyStates['response'] || 'Copy'}</button>
      <pre class="llm-response-pre">{call.response}</pre>
    </div>
  {/if}
{/snippet}

{#if collapsible}
  <details>
    <summary class="llm-details-summary">LLM Call - {call.model} - {formatDuration(call.durationMs)}</summary>
    {@render content()}
  </details>
{:else}
  {@render content()}
{/if}
