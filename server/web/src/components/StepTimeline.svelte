<script lang="ts">
  import type { StepSummary } from '../lib/types';
  import { navigate } from '../lib/router';
  import { formatDuration, httpStatusCategory } from '../lib/format';

  interface Props {
    steps: StepSummary[];
    runId: string;
    sectionLabel?: string;
    muted?: boolean;
  }

  let { steps, runId, sectionLabel, muted = false }: Props = $props();

  // Total span = last step's (offset + duration), used for Gantt positioning
  let totalSpanMs = $derived.by(() => {
    let max = 1;
    for (const s of steps) {
      const end = (s.offsetMs ?? 0) + (s.durationMs ?? 0);
      if (end > max) max = end;
    }
    return max;
  });

  function goToStep(stepId: string) {
    navigate(`/runs/${runId}/steps/${stepId}`);
  }

  function handleKeydown(e: KeyboardEvent, stepId: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      goToStep(stepId);
    }
  }
</script>

{#if sectionLabel}
  <h2 class="timeline-section-label" class:muted>{sectionLabel}</h2>
{/if}

<div class="step-timeline" class:muted>
  {#each steps as step, i (step.stepId)}
    <div
      class="step-entry"
      role="link"
      tabindex="0"
      onclick={() => goToStep(step.stepId)}
      onkeydown={(e: KeyboardEvent) => handleKeydown(e, step.stepId)}
    >
      <div class="step-indicator">
        <div class="step-dot" class:step-dot-passed={step.passed} class:step-dot-failed={!step.passed}></div>
        {#if i < steps.length - 1}
          <div class="step-connector"></div>
        {/if}
      </div>

      <div class="step-content">
        <div class="step-header">
          <span class="step-name">{step.stepId}</span>
          <code class="step-node">{step.node}</code>
        </div>

        <div class="step-meta">
          {#if step.status !== undefined}
            <span class="step-status step-status-{httpStatusCategory(step.status)}">{step.status}</span>
          {/if}
          <span class="step-duration">{formatDuration(step.durationMs)}</span>
          <span class="step-assertions">
            <span class="steps-passed">{step.assertionPassedCount}</span><span class="steps-separator"> / </span>{step.assertionCount}
          </span>
          {#if step.retryCount && step.retryCount > 0}
            <span class="step-retry-badge">{step.retryCount} retry</span>
          {/if}
          {#if step.hasLLMCalls}
            <span class="step-llm-badge">LLM</span>
          {/if}
        </div>

        {#if step.displayOutputs && step.displayOutputs.length > 0}
          <div class="step-outputs">
            {#each step.displayOutputs as output}
              <span class="step-output">
                <span class="step-output-label">{output.label}:</span>
                <span class="step-output-value">{output.value ?? ''}</span>
              </span>
            {/each}
          </div>
        {/if}

        {#if step.error}
          <div class="step-error">{step.error}</div>
        {/if}

        {#if step.durationMs}
          {@const leftPct = ((step.offsetMs ?? 0) / totalSpanMs) * 100}
          {@const widthPct = Math.max((step.durationMs / totalSpanMs) * 100, 0.5)}
          <div class="step-duration-bar-track">
            <div
              class="step-duration-bar"
              style="margin-left: {leftPct}%; width: {widthPct}%"
            ></div>
          </div>
        {/if}
      </div>
    </div>
  {/each}
</div>
