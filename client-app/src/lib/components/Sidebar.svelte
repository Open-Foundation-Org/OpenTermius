<script lang="ts">
  import {
    Server,
    Terminal,
    FileCode2,
    FolderLock,
    ScrollText,
    Settings,
    Users,
    Key,
    UserCheck,
    ChevronDown,
    Check,
    Network,
    PanelLeftClose,
    Folder
  } from '@lucide/svelte';
  import { api, type ApiVault } from '../api/client';
  import { platform } from '../stores/platform.svelte';

  interface Props {
    activeNav: string;
    onNavChange: (nav: string) => void;
    activeVault?: ApiVault | null;
    vaults?: ApiVault[];
    onSelectVault?: (vault: ApiVault) => void;
    isCollapsed?: boolean;
    onToggleCollapse?: () => void;
    isMobileOpen?: boolean;
    onCloseMobile?: () => void;
    gatewayOnline?: boolean;
    onSwitchToGateway?: () => void;
  }

  let {
    activeNav = 'hosts',
    onNavChange,
    activeVault,
    vaults = [],
    onSelectVault,
    isCollapsed = false,
    onToggleCollapse,
    isMobileOpen = false,
    onCloseMobile,
    gatewayOnline = true,
    onSwitchToGateway
  }: Props = $props();

  function handleNavClick(navId: string) {
    onNavChange(navId);
    if (onCloseMobile) {
      onCloseMobile();
    }
  }

  let isVaultDropdownOpen = $state(false);

  const navSections = [
    {
      label: 'Connections',
      items: [
        { id: 'hosts', label: 'Hosts', icon: Server },
        { id: 'terminal', label: 'Terminal', icon: Terminal },
        { id: 'sftp', label: 'SFTP Browser', icon: Folder }
      ]
    },
    {
      label: 'Credentials & Network',
      items: [
        { id: 'keychain', label: 'SSH Keys', icon: Key },
        { id: 'identities', label: 'Identities', icon: UserCheck },
        { id: 'tunnels', label: 'Port Forwarding', icon: Network },
        { id: 'snippets', label: 'Snippets', icon: FileCode2 }
      ]
    },
    {
      label: 'Workspace',
      items: [
        { id: 'vaults', label: 'Vaults', icon: FolderLock },
        { id: 'logs', label: 'Audit Logs', icon: ScrollText },
        { id: 'settings', label: 'Settings', icon: Settings }
      ]
    }
  ];
</script>

{#if isMobileOpen}
  <div
    class="fixed inset-0 z-40 bg-black/70 backdrop-blur-xs lg:hidden animate-in fade-in duration-150"
    onclick={onCloseMobile}
    role="button"
    tabindex="0"
    onkeydown={(e) => e.key === 'Escape' && onCloseMobile?.()}
    aria-label="Close sidebar backdrop"
  ></div>
{/if}

<aside
  class="flex h-full flex-col justify-between border-r border-white/[0.07] bg-[#0e0e11] select-none transition-all duration-200 ease-in-out shrink-0
    fixed inset-y-0 left-0 z-50 w-68 p-3.5 pb-[calc(0.875rem+var(--sab))] shadow-2xl lg:relative lg:z-auto lg:shadow-none
    {platform.isMac && platform.isNativeTauri ? 'lg:pt-9' : ''}
    {isMobileOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}
    {isCollapsed ? 'lg:w-0 lg:p-0 lg:border-r-0 lg:overflow-hidden lg:opacity-0 lg:pointer-events-none' : 'lg:w-56 lg:p-3 lg:pb-[calc(0.75rem+var(--sab))] lg:opacity-100'}
  "
>
  {#if platform.isMac && platform.isNativeTauri}
    <div data-tauri-drag-region class="hidden lg:block absolute top-0 left-0 right-0 h-8 z-10"></div>
  {/if}

  <div class="flex-1 min-h-0 overflow-y-auto no-scrollbar">
    <!-- Brand Header -->
    <div class="flex items-center justify-between px-1.5 py-1.5 mb-3">
      <div class="flex items-center gap-2 min-w-0">
        <div class="flex h-7 w-7 items-center justify-center rounded-lg bg-white/[0.05] border border-white/[0.08] text-emerald-400 shrink-0">
          <Terminal class="h-3.5 w-3.5" />
        </div>
        <span class="text-[13px] font-semibold tracking-tight text-zinc-100 truncate">OpenTermius</span>
      </div>
      {#if isMobileOpen && onCloseMobile}
        <button
          type="button"
          onclick={onCloseMobile}
          class="lg:hidden text-zinc-500 hover:text-zinc-200 p-1 rounded-md hover:bg-white/[0.05] transition shrink-0"
          aria-label="Close mobile sidebar"
        >
          <PanelLeftClose class="h-4 w-4" />
        </button>
      {:else if onToggleCollapse}
        <button
          type="button"
          onclick={onToggleCollapse}
          class="hidden lg:block text-zinc-500 hover:text-zinc-200 p-1 rounded-md hover:bg-white/[0.05] transition shrink-0"
          title="Hide Sidebar (⌘B)"
        >
          <PanelLeftClose class="h-3.5 w-3.5" />
        </button>
      {/if}
    </div>

    <!-- Active Vault Selector -->
    <div class="relative mb-4">
      <button
        type="button"
        onclick={() => (isVaultDropdownOpen = !isVaultDropdownOpen)}
        class="w-full text-left rounded-lg border border-white/[0.07] bg-[#141418] px-2.5 py-2 hover:border-white/[0.14] transition"
      >
        <div class="flex items-center justify-between text-[10px] font-mono uppercase tracking-wider text-zinc-500 mb-0.5">
          <span>{activeVault?.type === 'team' ? 'Team Vault' : 'Personal Vault'}</span>
        </div>
        <div class="flex items-center justify-between font-medium text-xs text-zinc-100">
          <div class="flex items-center gap-2 truncate">
            {#if activeVault?.type === 'team'}
              <Users class="h-3.5 w-3.5 text-zinc-400 shrink-0" />
            {:else}
              <FolderLock class="h-3.5 w-3.5 text-zinc-400 shrink-0" />
            {/if}
            <span class="truncate">{activeVault?.name || 'Select Vault'}</span>
          </div>
          <ChevronDown class="h-3.5 w-3.5 text-zinc-500 transition-transform {isVaultDropdownOpen ? 'rotate-180' : ''}" />
        </div>
      </button>

      {#if isVaultDropdownOpen && vaults.length > 0}
        <div class="absolute left-0 right-0 top-full mt-1 z-30 rounded-xl border border-white/[0.1] bg-[#141419] p-1 shadow-2xl space-y-0.5">
          {#each vaults as vault (vault.id)}
            <button
              type="button"
              onclick={() => {
                onSelectVault?.(vault);
                isVaultDropdownOpen = false;
              }}
              class="flex w-full items-center justify-between rounded-lg px-2.5 py-1.5 text-xs text-left transition {activeVault?.id === vault.id ? 'bg-white/[0.08] text-white font-medium' : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'}"
            >
              <div class="flex items-center gap-2 truncate">
                {#if vault.type === 'team'}
                  <Users class="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                {:else}
                  <FolderLock class="h-3.5 w-3.5 text-zinc-400 shrink-0" />
                {/if}
                <span class="truncate">{vault.name}</span>
              </div>
              {#if activeVault?.id === vault.id}
                <Check class="h-3.5 w-3.5 text-emerald-400 shrink-0" />
              {/if}
            </button>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Grouped Navigation -->
    <nav class="space-y-4">
      {#each navSections as section}
        <div>
          <div class="px-2 mb-1 text-[10px] font-semibold uppercase tracking-wider text-zinc-500">
            {section.label}
          </div>
          <div class="space-y-0.5">
            {#each section.items as item}
              {@const Icon = item.icon}
              {@const isActive = activeNav === item.id}
              <button
                type="button"
                onclick={() => handleNavClick(item.id)}
                class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-[12.5px] transition-colors {isActive ? 'bg-white/[0.08] text-white font-medium' : 'text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200'}"
              >
                <Icon class="h-3.5 w-3.5 {isActive ? 'text-emerald-400' : 'text-zinc-500'}" />
                <span>{item.label}</span>
              </button>
            {/each}
          </div>
        </div>
      {/each}
    </nav>
  </div>

  <!-- Bottom Connection Status -->
  <div class="border-t border-white/[0.06] pt-2.5">
    {#if api.isStandalone()}
      <button
        type="button"
        onclick={() => onSwitchToGateway?.()}
        class="flex w-full items-center justify-between rounded-lg bg-[#141418] px-2.5 py-1.5 border border-white/[0.06] hover:border-indigo-500/30 transition cursor-pointer"
        title="Switch to Self-Hosted Gateway"
      >
        <div class="flex items-center gap-2 truncate">
          <span class="h-1.5 w-1.5 rounded-full bg-indigo-400 shrink-0"></span>
          <span class="text-[11px] font-medium text-indigo-300 truncate">Standalone</span>
        </div>
        <span class="text-[10px] text-zinc-500 shrink-0 font-mono">local</span>
      </button>
    {:else}
      <div class="flex items-center justify-between rounded-lg px-2.5 py-1.5 border {gatewayOnline ? 'bg-emerald-500/10 border-emerald-500/25' : 'bg-amber-500/10 border-amber-500/25'}">
        <div class="flex items-center gap-2 truncate">
          <span class="h-1.5 w-1.5 rounded-full {gatewayOnline ? 'bg-emerald-400 animate-pulse' : 'bg-amber-400'} shrink-0"></span>
          <span class="text-[11px] font-medium {gatewayOnline ? 'text-emerald-300' : 'text-amber-300'} truncate">
            {gatewayOnline ? 'Online · Gateway' : 'Offline · Local'}
          </span>
        </div>
        <span class="text-[10px] text-zinc-400 shrink-0 font-mono truncate max-w-[85px]">
          {api.getApiBase().replace(/^https?:\/\//, '').split('/')[0]}
        </span>
      </div>
    {/if}
  </div>
</aside>
