<script lang="ts">
  import { verifyPin } from "../stores/pin";

  let { open = $bindable(false) }: { open: boolean } = $props();
  let pin = $state("");
  let error = $state(false);
  let dialogEl: HTMLDialogElement | undefined = $state();

  $effect(() => {
    if (open) {
      pin = "";
      error = false;
      dialogEl?.showModal();
    } else {
      dialogEl?.close();
    }
  });

  async function handleSubmit(e: Event) {
    e.preventDefault();
    error = false;
    const ok = await verifyPin(pin);
    if (ok) {
      open = false;
    } else {
      error = true;
    }
  }
</script>

<dialog
  bind:this={dialogEl}
  onclose={() => (open = false)}
  class="m-auto bg-surface text-txtmain rounded-lg shadow-xl p-6 backdrop:bg-black/50"
>
  <form onsubmit={handleSubmit} class="flex flex-col gap-4 min-w-[250px]">
    <h2 class="text-lg font-semibold">Enter Admin PIN</h2>
    <input
      type="password"
      bind:value={pin}
      placeholder="PIN"
      class="border border-border rounded px-3 py-2 bg-surface text-txtmain"
    />
    {#if error}
      <p class="text-red-500 text-sm">Incorrect PIN</p>
    {/if}
    <div class="flex gap-2 justify-end">
      <button type="button" onclick={() => (open = false)} class="btn btn--sm">Cancel</button>
      <button type="submit" class="btn btn--sm" disabled={!pin}>Unlock</button>
    </div>
  </form>
</dialog>
