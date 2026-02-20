<script lang="ts">
  import { parseRoute } from './lib/router';
  import Nav from './components/Nav.svelte';
  import RunList from './routes/RunList.svelte';
  import RunDetail from './routes/RunDetail.svelte';

  let path = $state(window.location.pathname);
  let route = $derived(parseRoute(path));

  $effect(() => {
    function onPopState() {
      path = window.location.pathname;
    }
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  });
</script>

<Nav {route} />

<main>
  {#if route.view === 'run-list'}
    <RunList />
  {:else if route.view === 'run-detail'}
    <RunDetail runId={route.runId} />
  {:else if route.view === 'step-detail'}
    <div class="empty-state"><p>Step detail view coming soon: {route.runId} / {route.stepId}</p></div>
  {/if}
</main>
