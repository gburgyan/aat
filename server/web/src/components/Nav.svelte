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
</script>

<nav class="nav-bar">
  <a
    class="nav-brand"
    href="/"
    onclick={(e: MouseEvent) => handleClick(e, '/')}
  >AAT</a>

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
    {/if}
  </ol>
</nav>
