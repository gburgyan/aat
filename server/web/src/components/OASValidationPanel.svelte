<script lang="ts">
  import type { OASValidationDetail, OASPayloadDetail } from '../lib/types';

  interface Props {
    oasValidation: OASValidationDetail;
  }

  let { oasValidation }: Props = $props();

  function totalErrors(detail: OASValidationDetail): number {
    let count = 0;
    if (detail.request) count += detail.request.errorCount;
    if (detail.response) count += detail.response.errorCount;
    return count;
  }

  function payloadStatus(p: OASPayloadDetail | undefined): string {
    if (!p) return 'not validated';
    if (p.valid) return 'valid';
    if (p.errorCount > 0) return `${p.errorCount} error(s)`;
    const warnCount = p.compilationWarnings?.length ?? 0;
    return `not validated (${warnCount} schema(s) unsupported)`;
  }

  function payloadClass(p: OASPayloadDetail | undefined): string {
    if (!p) return '';
    if (p.valid) return 'oas-valid';
    if (p.errorCount > 0) return 'oas-invalid';
    return 'oas-warn';
  }
</script>

{#if oasValidation.skipped}
  <p class="oas-skipped">Skipped: {oasValidation.skipReason}</p>
{:else}
  {#if oasValidation.operationId}
    <p style="margin-bottom: 0.75rem; font-size: 0.85rem;">
      Operation: <code>{oasValidation.operationId}</code>
    </p>
  {/if}

  {#if oasValidation.request}
    <h4 class="section-heading">Request Validation</h4>
    <p class="oas-summary {payloadClass(oasValidation.request)}">
      {payloadStatus(oasValidation.request)}
    </p>
    {#if oasValidation.request.errors && oasValidation.request.errors.length > 0}
      <table class="detail-table">
        <thead>
          <tr>
            <th>Path</th>
            <th>Message</th>
          </tr>
        </thead>
        <tbody>
          {#each oasValidation.request.errors as err, i (i)}
            <tr class="assertion-row-failed">
              <td class="dt-mono dt-muted">{err.path || '-'}</td>
              <td>{err.message}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
    {#if oasValidation.request.compilationWarnings && oasValidation.request.compilationWarnings.length > 0}
      <ul class="oas-compilation-warnings">
        {#each oasValidation.request.compilationWarnings as warning}
          <li>{warning}</li>
        {/each}
      </ul>
    {/if}
  {/if}

  {#if oasValidation.response}
    <h4 class="section-heading">Response Validation</h4>
    <p class="oas-summary {payloadClass(oasValidation.response)}">
      {payloadStatus(oasValidation.response)}
    </p>
    {#if oasValidation.response.errors && oasValidation.response.errors.length > 0}
      <table class="detail-table">
        <thead>
          <tr>
            <th>Path</th>
            <th>Message</th>
          </tr>
        </thead>
        <tbody>
          {#each oasValidation.response.errors as err, i (i)}
            <tr class="assertion-row-failed">
              <td class="dt-mono dt-muted">{err.path || '-'}</td>
              <td>{err.message}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
    {#if oasValidation.response.compilationWarnings && oasValidation.response.compilationWarnings.length > 0}
      <ul class="oas-compilation-warnings">
        {#each oasValidation.response.compilationWarnings as warning}
          <li>{warning}</li>
        {/each}
      </ul>
    {/if}
  {/if}

  {#if !oasValidation.request && !oasValidation.response}
    <p class="oas-skipped">No request or response body to validate.</p>
  {/if}
{/if}

<style>
  .oas-skipped {
    color: var(--color-text-muted);
    font-style: italic;
    font-size: 0.85rem;
  }
  .oas-summary {
    font-size: 0.85rem;
    margin-bottom: 0.5rem;
    font-weight: 600;
  }
  .oas-valid {
    color: var(--color-success);
  }
  .oas-invalid {
    color: var(--color-error);
  }
  .oas-warn {
    color: var(--color-warning, #d97706);
  }
  .oas-compilation-warnings {
    color: var(--color-text-muted);
    font-size: 0.8rem;
    font-style: italic;
    margin: 0.25rem 0 0 1rem;
    padding: 0;
    list-style: none;
  }
</style>
