<script lang="ts">
  import type { Route } from '../lib/router';
  import { navigate } from '../lib/router';

  interface Props {
    route: Route;
  }

  let { route }: Props = $props();

  function handleClick(e: MouseEvent, path: string) {
    e.preventDefault();
    navigate(path);
  }

  function handleKeydown(e: KeyboardEvent, path: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      navigate(path);
    }
  }

  let isTracesSection = $derived(
    route.view === 'trace-list' || route.view === 'trace-detail'
  );
</script>

<nav class="nav-bar">
  <a
    class="nav-brand"
    href="/"
    onclick={(e: MouseEvent) => handleClick(e, '/')}
  >AAT</a>

  <div class="nav-links">
    <a
      class="nav-link"
      class:nav-link-active={!isTracesSection}
      href="/"
      onclick={(e: MouseEvent) => handleClick(e, '/')}
    >Runs</a>
    <a
      class="nav-link"
      class:nav-link-active={isTracesSection}
      href="/traces"
      onclick={(e: MouseEvent) => handleClick(e, '/traces')}
    >Traces</a>
  </div>

  <ol class="breadcrumbs">
    {#if route.view === 'run-list'}
      <li class="breadcrumb-active">Runs</li>
    {:else if route.view === 'run-detail'}
      <li>
        <a
          href="/"
          onclick={(e: MouseEvent) => handleClick(e, '/')}
        >Runs</a>
      </li>
      <li class="breadcrumb-active">{route.runId}</li>
    {:else if route.view === 'step-detail'}
      <li>
        <a
          href="/"
          onclick={(e: MouseEvent) => handleClick(e, '/')}
        >Runs</a>
      </li>
      <li>
        <a
          href="/runs/{route.runId}"
          onclick={(e: MouseEvent) => handleClick(e, `/runs/${route.runId}`)}
        >{route.runId}</a>
      </li>
      <li class="breadcrumb-active">{route.stepId}</li>
    {:else if route.view === 'batch-detail'}
      <li>
        <a
          href="/"
          onclick={(e: MouseEvent) => handleClick(e, '/')}
        >Runs</a>
      </li>
      <li class="breadcrumb-active">{route.batchId}</li>
    {:else if route.view === 'trace-list'}
      <li class="breadcrumb-active">Traces</li>
    {:else if route.view === 'trace-detail'}
      <li>
        <a
          href="/traces"
          onclick={(e: MouseEvent) => handleClick(e, '/traces')}
        >Traces</a>
      </li>
      <li class="breadcrumb-active">{route.traceId}</li>
    {/if}
  </ol>
</nav>

<style>
  .nav-links {
    display: flex;
    gap: 0.25rem;
    margin-right: 1rem;
  }
  .nav-link {
    padding: 0.25rem 0.625rem;
    border-radius: 4px;
    font-size: 0.8125rem;
    text-decoration: none;
    color: var(--color-text-muted, #9ca3af);
    transition: color 0.15s, background 0.15s;
  }
  .nav-link:hover {
    color: var(--color-text, #e5e7eb);
    background: var(--color-surface, #1e1e1e);
  }
  .nav-link-active {
    color: var(--color-text, #e5e7eb);
    background: var(--color-surface, #1e1e1e);
  }
</style>
