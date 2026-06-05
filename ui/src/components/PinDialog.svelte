<script lang="ts">
  import { verifyPin } from "../stores/pin";
  import * as Dialog from "$lib/components/ui/dialog/index.js";
  import { Button } from "$lib/components/ui/button/index.js";
  import { Input } from "$lib/components/ui/input/index.js";

  let { open = $bindable(false) }: { open: boolean } = $props();
  let pin = $state("");
  let error = $state(false);

  $effect(() => {
    if (open) {
      pin = "";
      error = false;
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

<Dialog.Root
  {open}
  onOpenChange={(v) => {
    open = v;
  }}
>
  <Dialog.Content class="sm:max-w-[360px]">
    <Dialog.Header>
      <Dialog.Title>Enter Admin PIN</Dialog.Title>
      <Dialog.Description>
        Unlock viewing of captured request and response bodies for this session.
      </Dialog.Description>
    </Dialog.Header>
    <form onsubmit={handleSubmit} class="flex flex-col gap-4">
      <Input type="password" bind:value={pin} placeholder="PIN" autocomplete="off" />
      {#if error}
        <p class="text-sm text-destructive">Incorrect PIN</p>
      {/if}
      <Dialog.Footer>
        <Button type="button" variant="outline" onclick={() => (open = false)}>Cancel</Button>
        <Button type="submit" disabled={!pin}>Unlock</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
