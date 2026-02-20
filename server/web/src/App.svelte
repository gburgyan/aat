<script lang="ts">
  let status = $state("...");

  async function checkHealth() {
    try {
      const res = await fetch("/health");
      const data = await res.json();
      status = data.status ?? "unknown";
    } catch {
      status = "unreachable";
    }
  }
</script>

<main>
  <h1>AAT</h1>
  <p>Adaptive API Testing</p>
  <button onclick={checkHealth}>Check health</button>
  <p>Status: <code>{status}</code></p>
</main>
