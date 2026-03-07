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
      setTimeout(() => (copyStates[key] = ''), 1500);
    } catch {
      copyStates[key] = 'Failed';
      setTimeout(() => (copyStates[key] = ''), 1500);
    }
  }

  function copyConversation() {
    const conversation: Record<string, unknown> = {
      model: call.model,
      temperature: call.temperature,
    };
    if (call.maxTokens) conversation.maxTokens = call.maxTokens;
    if (call.thinkingBudget) conversation.thinkingBudget = call.thinkingBudget;
    if (call.reasoningEffort) conversation.reasoningEffort = call.reasoningEffort;

    const messages = [
      ...(call.messages || []).map(m => ({ role: m.role, content: m.content })),
      ...(call.response ? [{ role: 'assistant', content: call.response }] : []),
    ];
    conversation.messages = messages;

    copyText('conversation', JSON.stringify(conversation, null, 2));
  }
</script>

{#snippet content()}
  <div class="llm-metrics">
    <div class="step-detail-meta-item">
      <span class="meta-label">Model</span>
      <span class="meta-value dt-mono">{call.model}</span>
    </div>
    <div class="step-detail-meta-item">
      <span class="meta-label">Temperature</span>
      <span class="meta-value dt-mono">{call.temperature}</span>
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
    {#if call.thinkingBudget}
      <div class="step-detail-meta-item">
        <span class="meta-label">Thinking Budget</span>
        <span class="meta-value dt-mono">{call.thinkingBudget.toLocaleString()}</span>
      </div>
    {/if}
    {#if call.reasoningEffort}
      <div class="step-detail-meta-item">
        <span class="meta-label">Reasoning Effort</span>
        <span class="meta-value">{call.reasoningEffort}</span>
      </div>
    {/if}
  </div>

  <div class="llm-conversation-actions">
    <button class="llm-conversation-copy-btn" onclick={copyConversation}>
      {copyStates['conversation'] || 'Copy Conversation'}
    </button>
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

  {#if call.thinkingContent}
    <h4 class="section-heading">Thinking</h4>
    <div class="llm-pre-wrapper">
      <button class="llm-copy-btn" onclick={() => copyText('thinking', call.thinkingContent!)}>{copyStates['thinking'] || 'Copy'}</button>
      <pre class="llm-response-pre">{call.thinkingContent}</pre>
    </div>
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
