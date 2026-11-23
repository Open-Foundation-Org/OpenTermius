<script lang="ts">
  import { X } from '@lucide/svelte';

  interface Props {
    open: boolean;
    isMac: boolean;
    onClose: () => void;
  }

  let { open, isMac, onClose }: Props = $props();

  const macShortcuts: [string, string][] = [
    ['Quick Connect / Jump to Tab', '⌘T / ⌘K / ⌘J'],
    ['Hosts Dashboard', '⌘1'],
    ['Jump to Terminal Tab 1–8', '⌘2 … ⌘9'],
    ['Next / Previous Tab', 'Ctrl+Tab / ⌘⇧] · ⌘⇧['],
    ['Close Active Tab / Split', '⌘W'],
    ['Split Right / Split Down', '⌘D / ⌘⇧D'],
    ['New Local Shell', '⌘⇧L'],
    ['Clear Terminal Buffer', '⌘⇧K'],
    ['Zoom In / Out / Reset', '⌘+ / ⌘- / ⌘0'],
    ['Toggle Sidebar', '⌘B'],
    ['Settings', '⌘,'],
    ['Shortcuts Cheatsheet', '⌘/']
  ];

  const winLinuxShortcuts: [string, string][] = [
    ['Quick Connect / Jump to Tab', 'Ctrl+J / Ctrl+Shift+K'],
    ['Hosts Dashboard', 'Ctrl+1'],
    ['Jump to Terminal Tab 1–8', 'Ctrl+2 … Ctrl+9'],
    ['Next / Previous Tab', 'Ctrl+Tab / Ctrl+Shift+Tab'],
    ['Close Active Tab / Split', 'Ctrl+Shift+W'],
    ['Split Right / Split Down', 'Ctrl+Shift+D / Ctrl+Alt+D'],
    ['New Local Shell', 'Ctrl+Shift+L'],
    ['Zoom In / Out / Reset', 'Ctrl++ / Ctrl+- / Ctrl+0'],
    ['Toggle Sidebar', 'Ctrl+Shift+B'],
    ['Settings', 'Ctrl+,'],
    ['Shortcuts Cheatsheet', 'Ctrl+/']
  ];

  let items = $derived(isMac ? macShortcuts : winLinuxShortcuts);
</script>

{#if open}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/75 backdrop-blur-xs p-4"
    onclick={(e) => {
      if (e.target === e.currentTarget) onClose();
    }}
    role="presentation"
  >
    <div class="w-full max-w-md rounded-2xl border border-[#28283a] bg-[#13131c] p-5 shadow-2xl space-y-4 text-xs">
      <div class="flex items-center justify-between border-b border-white/[0.06] pb-3">
        <h3 class="text-sm font-semibold text-white">Keyboard Shortcuts</h3>
        <button type="button" onclick={onClose} class="text-neutral-400 hover:text-white">
          <X class="h-4 w-4" />
        </button>
      </div>
      <div class="space-y-2 text-neutral-300">
        {#each items as [label, keys] (label)}
          <div class="flex items-center justify-between py-1 border-b border-white/[0.04] last:border-0">
            <span class="text-neutral-300">{label}</span>
            <kbd class="rounded border border-white/[0.1] bg-[#1a1a26] px-2 py-0.5 font-mono text-[11px] text-emerald-300">{keys}</kbd>
          </div>
        {/each}
      </div>
    </div>
  </div>
{/if}
