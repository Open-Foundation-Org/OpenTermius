<script lang="ts">
  import { AlertCircle, CheckCircle2, Info, X } from '@lucide/svelte';
  import { toast } from '../stores/toast.svelte';
  import { platform } from '../stores/platform.svelte';
</script>

{#if toast.items.length > 0}
  <div
    class="fixed z-[100] flex flex-col gap-2 pointer-events-none
      {platform.isMobile
        ? 'left-3 right-3 bottom-[calc(4.75rem+var(--sab))]'
        : 'top-4 right-4 w-full max-w-sm'}"
    aria-live="polite"
  >
    {#each toast.items as item (item.id)}
      <div
        role="alert"
        class="pointer-events-auto flex items-start gap-2.5 rounded-2xl border px-3.5 py-3 shadow-2xl backdrop-blur-md animate-in fade-in slide-in-from-bottom-2 sm:slide-in-from-top-2 duration-150
          {item.kind === 'error'
            ? 'border-rose-500/35 bg-[#1b1116]/95 text-rose-100'
            : item.kind === 'success'
              ? 'border-emerald-500/35 bg-[#101c16]/95 text-emerald-100'
              : 'border-indigo-500/35 bg-[#121420]/95 text-indigo-100'}"
      >
        {#if item.kind === 'error'}
          <AlertCircle class="h-4.5 w-4.5 text-rose-400 shrink-0 mt-0.5" />
        {:else if item.kind === 'success'}
          <CheckCircle2 class="h-4.5 w-4.5 text-emerald-400 shrink-0 mt-0.5" />
        {:else}
          <Info class="h-4.5 w-4.5 text-indigo-400 shrink-0 mt-0.5" />
        {/if}

        <p class="flex-1 text-xs leading-relaxed break-words">{item.message}</p>

        <button
          type="button"
          onclick={() => toast.dismiss(item.id)}
          class="flex h-6 w-6 shrink-0 items-center justify-center rounded-lg text-neutral-400 hover:text-white hover:bg-white/10 transition"
          aria-label="Dismiss notification"
        >
          <X class="h-3.5 w-3.5" />
        </button>
      </div>
    {/each}
  </div>
{/if}

{#if toast.confirmRequest}
  <div
    class="fixed inset-0 z-[110] flex items-center justify-center bg-black/75 p-4 backdrop-blur-xs"
    role="presentation"
    onclick={() => toast.resolveConfirm(false)}
  >
    <div
      class="w-full max-w-sm rounded-2xl border border-[#2a2a3c] bg-[#14141e] p-5 shadow-2xl space-y-4 animate-in fade-in zoom-in-95 duration-150"
      role="alertdialog"
      aria-modal="true"
      tabindex="-1"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => { if (e.key === 'Escape') toast.resolveConfirm(false); }}
    >
      <div class="flex items-start gap-3">
        <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-rose-500/15 text-rose-400 border border-rose-500/25">
          <AlertCircle class="h-4.5 w-4.5" />
        </div>
        <div class="min-w-0 space-y-1">
          <h3 class="text-sm font-semibold text-white">Confirm Action</h3>
          <p class="text-xs text-neutral-300 leading-relaxed">{toast.confirmRequest.message}</p>
        </div>
      </div>

      <div class="flex justify-end gap-2 pt-2 border-t border-[#222230]">
        <button
          type="button"
          onclick={() => toast.resolveConfirm(false)}
          class="rounded-lg bg-[#20202c] px-3.5 py-2 text-xs font-semibold text-neutral-300 hover:bg-[#2a2a3a] transition cursor-pointer"
        >
          Cancel
        </button>
        <button
          type="button"
          onclick={() => toast.resolveConfirm(true)}
          class="rounded-lg bg-rose-600 px-4 py-2 text-xs font-semibold text-white hover:bg-rose-500 transition cursor-pointer active:scale-95"
        >
          {toast.confirmRequest.confirmLabel}
        </button>
      </div>
    </div>
  </div>
{/if}
