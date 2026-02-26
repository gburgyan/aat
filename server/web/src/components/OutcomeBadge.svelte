<script lang="ts">
  import type { Outcome } from '../lib/types';

  interface Props {
    outcome: Outcome;
    size?: 'sm' | 'md';
  }

  let { outcome, size = 'md' }: Props = $props();

  const classMap: Record<Outcome, string> = {
    passed: 'badge-success',
    failed: 'badge-error',
    error: 'badge-warning',
    skipped: 'badge-skipped',
  };

  let badgeClass = $derived(`badge badge-${size} ${classMap[outcome]}`);
</script>

<span class={badgeClass}>{outcome}</span>
