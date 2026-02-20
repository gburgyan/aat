<script lang="ts">
  interface Tab {
    id: string;
    label: string;
  }

  interface Props {
    tabs: Tab[];
    active: string;
    onchange: (id: string) => void;
  }

  let { tabs, active, onchange }: Props = $props();
</script>

<div class="tab-bar" role="tablist">
  {#each tabs as tab (tab.id)}
    <button
      class="tab-button"
      class:tab-active={active === tab.id}
      role="tab"
      aria-selected={active === tab.id}
      onclick={() => onchange(tab.id)}
    >{tab.label}</button>
  {/each}
</div>
