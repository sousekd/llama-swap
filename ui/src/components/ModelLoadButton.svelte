<script lang="ts">
  import type { Model } from "../lib/types";
  import { pendingLoads, onToggleLoad } from "../stores/modelLoad";
  import { Play, PowerOff, Loader2 } from "@lucide/svelte";
  import * as Dialog from "$lib/components/ui/dialog/index.js";
  import { Button } from "$lib/components/ui/button/index.js";

  interface Props {
    model: Model;
    /** "md" for list rows (size-7), "sm" for the detail header (size-5). */
    size?: "md" | "sm";
  }

  let { model, size = "md" }: Props = $props();

  let btnSize = $derived(size === "sm" ? "size-5 rounded-sm" : "size-7 rounded-md");
  let iconSize = $derived(size === "sm" ? "size-3.5" : "size-4");
  let busy = $derived(model.state === "starting" || model.state === "stopping");

  let errorOpen = $state(false);
  let errorMessage = $state("");

  async function toggle(): Promise<void> {
    errorOpen = false;
    try {
      await onToggleLoad(model);
    } catch (error) {
      errorMessage =
        error instanceof Error && error.message ? error.message : "Failed to update model";
      errorOpen = true;
    }
  }
</script>

<button
  type="button"
  class="text-muted-foreground hover:bg-accent hover:text-accent-foreground flex {btnSize} shrink-0 items-center justify-center disabled:opacity-50"
  title={model.state === "ready" ? "Unload" : $pendingLoads[model.id] ? "Cancel" : "Load"}
  aria-label={model.state === "ready" ? "Unload model" : "Load model"}
  disabled={busy}
  onclick={toggle}
>
  {#if $pendingLoads[model.id] && model.state === "stopped"}
    <Loader2 class="{iconSize} animate-spin" />
  {:else if model.state === "ready"}
    <PowerOff class={iconSize} />
  {:else if busy}
    <Loader2 class="{iconSize} animate-spin" />
  {:else}
    <Play class={iconSize} />
  {/if}
</button>

<Dialog.Root open={errorOpen} onOpenChange={(v) => (errorOpen = v)}>
  <Dialog.Content class="sm:max-w-[360px]">
    <Dialog.Header>
      <Dialog.Title>Model action failed</Dialog.Title>
      <Dialog.Description class="text-destructive break-words">
        {errorMessage}
      </Dialog.Description>
    </Dialog.Header>
    <Dialog.Footer>
      <Button variant="outline" onclick={() => (errorOpen = false)}>Close</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
