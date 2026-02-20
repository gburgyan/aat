<script lang="ts">
  import type {
    ErrorClassDetail,
    ExpectFailureDetail,
    ResponseBodyErrorDetail,
    RelaxationDetail,
  } from '../lib/types';

  interface Props {
    errorClassification?: ErrorClassDetail;
    expectFailure?: ExpectFailureDetail;
    responseBodyError?: ResponseBodyErrorDetail;
    relaxations?: RelaxationDetail[];
  }

  let {
    errorClassification,
    expectFailure,
    responseBodyError,
    relaxations,
  }: Props = $props();

  function categoryBadgeClass(cat: string): string {
    switch (cat) {
      case 'client':
        return 'badge-warning';
      case 'server':
        return 'badge-error';
      case 'network':
      case 'timeout':
        return 'badge-error';
      default:
        return 'badge-muted';
    }
  }
</script>

{#if errorClassification}
  <div class="error-section">
    <h4 class="error-section-title">Error Classification</h4>
    <div class="error-detail-grid">
      <span class="error-detail-label">Category</span>
      <span><span class="badge badge-sm {categoryBadgeClass(errorClassification.category)}">{errorClassification.category}</span></span>
      <span class="error-detail-label">Detail</span>
      <span>{errorClassification.detail}</span>
      <span class="error-detail-label">Action</span>
      <span>{errorClassification.action}</span>
      {#if errorClassification.retryAttempt > 0}
        <span class="error-detail-label">Retry</span>
        <span>Attempt {errorClassification.retryAttempt}</span>
      {/if}
    </div>
  </div>
{/if}

{#if expectFailure}
  <div class="error-section">
    <h4 class="error-section-title">Expected Failure</h4>
    <div class="error-detail-grid">
      <span class="error-detail-label">Expected</span>
      <span>
        {#each expectFailure.expected as code, i}
          <span class="badge badge-sm badge-muted" style="margin-right: 0.25rem;">{code}</span>
        {/each}
      </span>
      <span class="error-detail-label">Actual</span>
      <span>
        <span class="badge badge-sm {expectFailure.passed ? 'badge-success' : 'badge-error'}">{expectFailure.actual}</span>
      </span>
      <span class="error-detail-label">Result</span>
      <span>
        <span class="badge badge-sm {expectFailure.passed ? 'badge-success' : 'badge-error'}">
          {expectFailure.passed ? 'PASSED' : 'FAILED'}
        </span>
      </span>
    </div>
  </div>
{/if}

{#if responseBodyError}
  <div class="error-section">
    <h4 class="error-section-title">Response Body Error</h4>
    <div class="error-detail-grid">
      <span class="error-detail-label">Rule Path</span>
      <span class="dt-mono">{responseBodyError.rulePath}</span>
      <span class="error-detail-label">Rule</span>
      <span>{responseBodyError.rule}</span>
      {#if responseBodyError.message}
        <span class="error-detail-label">Message</span>
        <span>{responseBodyError.message}</span>
      {/if}
      {#if responseBodyError.code}
        <span class="error-detail-label">Code</span>
        <span class="dt-mono">{responseBodyError.code}</span>
      {/if}
      {#if responseBodyError.category}
        <span class="error-detail-label">Category</span>
        <span>{responseBodyError.category}</span>
      {/if}
    </div>
  </div>
{/if}

{#if relaxations && relaxations.length > 0}
  <div class="error-section">
    <h4 class="error-section-title">Relaxations</h4>
    <table class="detail-table">
      <thead>
        <tr>
          <th>Constraint</th>
          <th>Input Ref</th>
          <th>Reason</th>
          <th>Depth</th>
        </tr>
      </thead>
      <tbody>
        {#each relaxations as r, i (i)}
          <tr style="border-left: 3px solid var(--color-warning);">
            <td class="dt-mono">{r.constraintName}</td>
            <td class="dt-mono dt-muted">{r.inputRef}</td>
            <td>{r.reason}</td>
            <td>{r.depth}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}
