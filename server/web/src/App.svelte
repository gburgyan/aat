<script lang="ts">
  import { parseRoute } from './lib/router';
  import Nav from './components/Nav.svelte';
  import RunList from './routes/RunList.svelte';
  import RunDetail from './routes/RunDetail.svelte';
  import StepDetail from './routes/StepDetail.svelte';
  import BatchDetail from './routes/BatchDetail.svelte';
  import TraceList from './routes/TraceList.svelte';
  import TraceDetail from './routes/TraceDetail.svelte';

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
  {:else if route.view === 'attempt-detail'}
    <RunDetail runId={route.runId} attempt={route.attempt} />
  {:else if route.view === 'step-detail'}
    <StepDetail runId={route.runId} stepId={route.stepId} />
  {:else if route.view === 'attempt-step-detail'}
    <StepDetail runId={route.runId} stepId={route.stepId} attempt={route.attempt} />
  {:else if route.view === 'batch-detail'}
    <BatchDetail batchId={route.batchId} />
  {:else if route.view === 'trace-list'}
    <TraceList />
  {:else if route.view === 'trace-detail'}
    <TraceDetail traceId={route.traceId} />
  {/if}
</main>
