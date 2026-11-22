<script lang="ts">
  import {
    Terminal,
    Radio,
    Pencil,
    Trash2,
    FolderInput,
    Folder,
    MoreVertical,
    Copy,
    CopyPlus
  } from '@lucide/svelte';
  import { platform } from '../stores/platform.svelte';
  import { toast } from '../stores/toast.svelte';

  interface HostItem {
    id: string;
    label: string;
    address: string;
    port: number;
    username: string;
    authType?: 'password' | 'key';
    password?: string;
    privateKey?: string;
    passphrase?: string;
    tags: string[];
    useMosh?: boolean;
    pingMs: number;
    group: string;
  }

  interface Props {
    host: HostItem;
    onConnect: (host: HostItem) => void;
    onEdit?: (host: HostItem) => void;
    onDuplicate?: (host: HostItem) => void;
    onDelete?: (host: HostItem) => void;
    onMove?: (host: HostItem) => void;
    onOpenSftp?: (host: HostItem) => void;
  }

  let { host, onConnect, onEdit, onDuplicate, onDelete, onMove, onOpenSftp }: Props = $props();
  let isMenuOpen = $state(false);

  async function copyHostTarget() {
    const cmd = host.port && host.port !== 22
      ? `ssh -p ${host.port} ${host.username}@${host.address}`
      : `ssh ${host.username}@${host.address}`;
    try {
      await navigator.clipboard.writeText(cmd);
      toast.success(`Copied: ${cmd}`);
    } catch {
      toast.error('Clipboard permission unavailable');
    }
  }
</script>

{#if platform.isMobile}
  <!-- Mobile Compact Host Tile -->
  <div class="relative">
    <div
      onclick={() => onConnect(host)}
      role="button"
      tabindex="0"
      onkeydown={(e) => e.key === 'Enter' && onConnect(host)}
      class="flex items-center justify-between gap-3 rounded-xl border border-white/[0.07] bg-[#131317] px-3.5 py-3 active:bg-[#19191f] active:scale-[0.99] transition cursor-pointer"
    >
      <div class="flex items-center gap-3 min-w-0 flex-1">
        <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-white/[0.04] text-zinc-300 border border-white/[0.06]">
          {#if host.useMosh}
            <Radio class="h-4.5 w-4.5 text-emerald-400" />
          {:else}
            <Terminal class="h-4.5 w-4.5 text-emerald-400" />
          {/if}
        </div>

        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <h3 class="text-sm font-semibold text-zinc-100 truncate tracking-tight">
              {host.label}
            </h3>
            {#if host.useMosh}
              <span class="shrink-0 rounded bg-white/[0.06] px-1.5 py-0.5 text-[9px] font-semibold text-zinc-300 uppercase">Mosh</span>
            {/if}
          </div>
          <p class="text-[11.5px] text-zinc-400 font-mono truncate mt-0.5">
            {host.username}@{host.address}:{host.port}
          </p>
        </div>
      </div>

      <div class="flex items-center shrink-0" onclick={(e) => e.stopPropagation()} role="presentation">
        <button
          type="button"
          onclick={() => (isMenuOpen = !isMenuOpen)}
          class="flex h-9 w-9 items-center justify-center rounded-lg text-zinc-400 hover:text-zinc-100 hover:bg-white/[0.06] active:bg-white/[0.1] transition cursor-pointer"
          aria-label="Host options"
        >
          <MoreVertical class="h-4 w-4" />
        </button>
      </div>
    </div>

    {#if isMenuOpen}
      <div
        class="fixed inset-0 z-50 flex flex-col justify-end bg-black/75 backdrop-blur-xs"
        onclick={() => (isMenuOpen = false)}
        role="presentation"
      >
        <div
          class="rounded-t-2xl border-t border-white/[0.09] bg-[#131318] px-4 pt-3 pb-[calc(1.25rem+var(--sab))] space-y-1.5 shadow-2xl"
          onclick={(e) => e.stopPropagation()}
          onkeydown={(e) => { if (e.key === 'Escape') isMenuOpen = false; }}
          role="dialog"
          tabindex="-1"
          aria-label="Host Actions"
        >
          <div class="mx-auto h-1 w-9 rounded-full bg-zinc-700"></div>
          <div class="px-1 py-2 border-b border-white/[0.07]">
            <p class="text-sm font-semibold text-zinc-100 truncate">{host.label}</p>
            <p class="text-xs font-mono text-zinc-400 truncate">{host.username}@{host.address}:{host.port}</p>
          </div>
          <div class="space-y-0.5 pt-1">
            <button
              type="button"
              onclick={() => { isMenuOpen = false; onConnect(host); }}
              class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-semibold text-emerald-400 active:bg-white/[0.06]"
            >
              <Terminal class="h-4 w-4" /> Connect SSH
            </button>
            {#if onOpenSftp}
              <button
                type="button"
                onclick={() => { isMenuOpen = false; onOpenSftp(host); }}
                class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium text-zinc-200 active:bg-white/[0.06]"
              >
                <Folder class="h-4 w-4 text-zinc-400" /> Browse SFTP
              </button>
            {/if}
            <button
              type="button"
              onclick={() => { isMenuOpen = false; void copyHostTarget(); }}
              class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-zinc-200 active:bg-white/[0.06]"
            >
              <Copy class="h-4 w-4 text-zinc-400" /> Copy SSH Command
            </button>
            {#if onEdit}
              <button
                type="button"
                onclick={() => { isMenuOpen = false; onEdit(host); }}
                class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-zinc-200 active:bg-white/[0.06]"
              >
                <Pencil class="h-4 w-4 text-zinc-400" /> Edit Host
              </button>
            {/if}
            {#if onDuplicate}
              <button
                type="button"
                onclick={() => { isMenuOpen = false; onDuplicate(host); }}
                class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-zinc-200 active:bg-white/[0.06]"
              >
                <CopyPlus class="h-4 w-4 text-zinc-400" /> Duplicate Host
              </button>
            {/if}
            {#if onMove}
              <button
                type="button"
                onclick={() => { isMenuOpen = false; onMove(host); }}
                class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm text-zinc-200 active:bg-white/[0.06]"
              >
                <FolderInput class="h-4 w-4 text-zinc-400" /> Move Host
              </button>
            {/if}
            {#if onDelete}
              <button
                type="button"
                onclick={() => { isMenuOpen = false; onDelete(host); }}
                class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium text-rose-400 active:bg-rose-950/30"
              >
                <Trash2 class="h-4 w-4" /> Delete Host
              </button>
            {/if}
          </div>
        </div>
      </div>
    {/if}
  </div>
{:else}
  <!-- Desktop Ultra-Clean Host Card (Click to Connect, single ⋮ menu) -->
  <div class="relative">
    <div
      onclick={() => onConnect(host)}
      onkeydown={(e) => e.key === 'Enter' && onConnect(host)}
      role="button"
      tabindex="0"
      class="group flex items-center justify-between gap-3 rounded-xl border border-white/[0.06] bg-[#121217] px-3.5 py-3 transition-all duration-150 hover:border-emerald-500/30 hover:bg-[#16161d] cursor-pointer"
    >
      <div class="flex items-center gap-3 min-w-0 flex-1">
        <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-white/[0.04] border border-white/[0.06] text-zinc-400 group-hover:text-emerald-400 group-hover:border-emerald-500/25 transition-colors">
          {#if host.useMosh}
            <Radio class="h-4 w-4" />
          {:else}
            <Terminal class="h-4 w-4" />
          {/if}
        </div>

        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <h3 class="text-[13px] font-semibold tracking-tight text-zinc-100 group-hover:text-white truncate">
              {host.label}
            </h3>
            {#if host.useMosh}
              <span class="shrink-0 rounded bg-white/[0.06] px-1.5 py-0.5 text-[9px] font-semibold text-zinc-300 uppercase">
                Mosh
              </span>
            {/if}
          </div>
          <p
            class="text-[11px] text-zinc-400 font-mono truncate mt-0.5"
            title={`${host.username}@${host.address}:${host.port}`}
          >
            {host.username}@{host.address}{#if host.port && host.port !== 22}:{host.port}{/if}
          </p>
          {#if host.tags && host.tags.length > 0}
            <div class="flex items-center gap-1 mt-1.5 overflow-hidden">
              {#each host.tags.slice(0, 3) as tag}
                <span class="rounded bg-white/[0.04] px-1.5 py-0.5 text-[10px] font-mono text-zinc-400 truncate">
                  {tag}
                </span>
              {/each}
            </div>
          {/if}
        </div>
      </div>

      <div class="shrink-0" onclick={(e) => e.stopPropagation()} role="presentation">
        <button
          type="button"
          onclick={() => (isMenuOpen = !isMenuOpen)}
          class="flex h-8 w-8 items-center justify-center rounded-lg text-zinc-400 hover:text-white hover:bg-white/[0.08] transition cursor-pointer"
          title="Host options"
          aria-label="Host options"
        >
          <MoreVertical class="h-4 w-4" />
        </button>
      </div>
    </div>

    {#if isMenuOpen}
      <div
        class="fixed inset-0 z-40"
        onclick={() => (isMenuOpen = false)}
        role="presentation"
      ></div>
      <div
        class="absolute right-2 top-12 z-50 w-44 rounded-xl border border-white/[0.1] bg-[#14141c] p-1.5 shadow-2xl space-y-0.5 text-xs animate-in fade-in zoom-in-95 duration-100"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => { if (e.key === 'Escape') isMenuOpen = false; }}
        role="menu"
        tabindex="-1"
      >
        <button
          type="button"
          onclick={() => { isMenuOpen = false; onConnect(host); }}
          class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-emerald-400 font-medium hover:bg-white/[0.06] transition cursor-pointer"
        >
          <Terminal class="h-3.5 w-3.5" /> Connect SSH
        </button>
        {#if onOpenSftp}
          <button
            type="button"
            onclick={() => { isMenuOpen = false; onOpenSftp(host); }}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-zinc-200 hover:bg-white/[0.06] transition cursor-pointer"
          >
            <Folder class="h-3.5 w-3.5 text-zinc-400" /> Browse SFTP
          </button>
        {/if}
        <button
          type="button"
          onclick={() => { isMenuOpen = false; void copyHostTarget(); }}
          class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-zinc-200 hover:bg-white/[0.06] transition cursor-pointer"
        >
          <Copy class="h-3.5 w-3.5 text-zinc-400" /> Copy SSH Command
        </button>
        {#if onEdit || onMove || onDelete}
          <div class="my-1 border-t border-white/[0.06]"></div>
        {/if}
        {#if onEdit}
          <button
            type="button"
            onclick={() => { isMenuOpen = false; onEdit(host); }}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-zinc-200 hover:bg-white/[0.06] transition cursor-pointer"
          >
            <Pencil class="h-3.5 w-3.5 text-zinc-400" /> Edit Host
          </button>
        {/if}
        {#if onDuplicate}
          <button
            type="button"
            onclick={() => { isMenuOpen = false; onDuplicate(host); }}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-zinc-200 hover:bg-white/[0.06] transition cursor-pointer"
          >
            <CopyPlus class="h-3.5 w-3.5 text-zinc-400" /> Duplicate Host
          </button>
        {/if}
        {#if onMove}
          <button
            type="button"
            onclick={() => { isMenuOpen = false; onMove(host); }}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-zinc-200 hover:bg-white/[0.06] transition cursor-pointer"
          >
            <FolderInput class="h-3.5 w-3.5 text-zinc-400" /> Move Host
          </button>
        {/if}
        {#if onDelete}
          <button
            type="button"
            onclick={() => { isMenuOpen = false; onDelete(host); }}
            class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-rose-400 hover:bg-rose-500/15 transition cursor-pointer"
          >
            <Trash2 class="h-3.5 w-3.5" /> Delete Host
          </button>
        {/if}
      </div>
    {/if}
  </div>
{/if}
