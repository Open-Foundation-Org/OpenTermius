<script lang="ts">
  import { api, type ApiSftpFile, type SftpHostConfig } from '../api/client';
  import { platform } from '../stores/platform.svelte';
  import { toast, formatErrorMessage } from '../stores/toast.svelte';
  import {
    Folder,
    FolderPlus,
    FileText,
    FileCode,
    FileArchive,
    Upload,
    Download,
    Trash2,
    RefreshCw,
    CornerLeftUp,
    Server,
    Search,
    AlertCircle,
    X,
    Laptop,
    Check,
    Eye,
    EyeOff,
    Pencil
  } from '@lucide/svelte';

  export interface SftpHostItem {
    id: string;
    label: string;
    address: string;
    port: number;
    username: string;
    authType?: 'password' | 'key';
    password?: string;
    privateKey?: string;
    passphrase?: string;
  }

  interface Props {
    paneId: 'left' | 'right';
    paneTitle: string;
    hosts: SftpHostItem[];
    targetId: string;
    onTargetChange: (id: string) => void;
    selectedPaths: string[];
    onSelectionChange: (paths: string[]) => void;
    currentPath: string;
    onPathChange: (path: string) => void;
    onItemsLoaded?: (items: ApiSftpFile[]) => void;
    onStartDrag?: (e: PointerEvent, item: ApiSftpFile) => void;
    dragHoverPane?: 'left' | 'right' | null;
    dragHoverFolderPath?: string | null;
    dragSourcePane?: 'left' | 'right' | null;
    suppressNextClick?: () => boolean;
    refreshSignal?: number;
  }

  let {
    paneId,
    paneTitle,
    hosts = [],
    targetId,
    onTargetChange,
    selectedPaths = [],
    onSelectionChange,
    currentPath = '',
    onPathChange,
    onItemsLoaded,
    onStartDrag,
    dragHoverPane = null,
    dragHoverFolderPath = null,
    dragSourcePane = null,
    suppressNextClick,
    refreshSignal = 0
  }: Props = $props();

  let isPaneDragOver = $derived(
    dragHoverPane === paneId && !dragHoverFolderPath && dragSourcePane !== paneId
  );

  function handleRowPointerDown(e: PointerEvent, item: ApiSftpFile) {
    if (e.button !== 0) return;
    if ((e.target as HTMLElement | null)?.closest('[data-no-drag]')) return;
    onStartDrag?.(e, item);
  }

  function handleRowActionClick(item: ApiSftpFile) {
    if (suppressNextClick?.()) return;
    if (item.isDir) {
      loadDirectory(item.path);
    } else {
      toggleSelect(item);
    }
  }

  let pathInputValue = $state('');
  let items = $state<ApiSftpFile[]>([]);
  let searchQuery = $state('');
  let showHidden = $state(false);
  let loadState = $state<'idle' | 'loading' | 'success' | 'empty' | 'error'>('idle');
  let errorMessage = $state<string | null>(null);
  let isUploading = $state(false);
  let isNewFolderOpen = $state(false);
  let newFolderName = $state('');
  let renamingItem = $state<ApiSftpFile | null>(null);
  let renameValue = $state('');
  let fileInputRef: HTMLInputElement | null = null;

  let isLocal = $derived(targetId === 'local');
  let currentHost = $derived(isLocal ? null : hosts.find((h) => h.id === targetId) || null);

  let filteredItems = $derived(
    items.filter((item) => {
      if (!showHidden && item.name.startsWith('.')) return false;
      return item.name.toLowerCase().includes(searchQuery.toLowerCase());
    })
  );

  function getHostConfig(): SftpHostConfig | null {
    if (!currentHost) return null;
    return {
      address: currentHost.address,
      port: currentHost.port || 22,
      username: currentHost.username,
      authType: currentHost.authType || 'password',
      password: currentHost.password,
      privateKey: currentHost.privateKey,
      passphrase: currentHost.passphrase
    };
  }

  export async function loadDirectory(targetPath: string = '') {
    if (!targetId) {
      loadState = 'idle';
      items = [];
      return;
    }

    loadState = 'loading';
    errorMessage = null;
    onSelectionChange([]);

    try {
      let res: { path: string; items: ApiSftpFile[] };
      if (isLocal) {
        const { invoke } = await import('@tauri-apps/api/core');
        res = await invoke<{ path: string; items: ApiSftpFile[] }>('local_fs_list', {
          path: targetPath
        });
      } else {
        const config = getHostConfig();
        if (!config) {
          loadState = 'idle';
          return;
        }
        res = await api.sftpList(config, targetPath);
      }

      onPathChange(res.path);
      pathInputValue = res.path;
      items = res.items || [];
      onItemsLoaded?.(items);
      loadState = items.length === 0 ? 'empty' : 'success';
    } catch (err: unknown) {
      const rawMsg = err instanceof Error ? err.message : typeof err === 'string' ? err : '';
      const prompt = toast.confirmHostKey(rawMsg);
      if (prompt) {
        const trusted = await prompt;
        if (trusted) {
          await handleTrustHostKey();
          return;
        }
        loadState = 'error';
        errorMessage = 'Connection cancelled: host key was not trusted.';
        return;
      }
      loadState = 'error';
      errorMessage = formatErrorMessage(err, 'Failed to list directory');
    }
  }

  async function handleTrustHostKey() {
    const config = getHostConfig();
    if (!config?.address) return;
    try {
      const { invoke } = await import('@tauri-apps/api/core');
      await invoke('ssh_trust_host_key', {
        host: config.address,
        port: config.port ?? 22
      });
      toast.success('Updated known host key');
      await loadDirectory(currentPath);
    } catch (err: unknown) {
      toast.error(err, 'Failed to reset host key');
    }
  }

  let lastTargetId = $state('');
  let lastRefreshSignal = $state(0);

  $effect(() => {
    if (targetId && targetId !== lastTargetId) {
      lastTargetId = targetId;
      loadDirectory('');
    }
  });

  $effect(() => {
    if (refreshSignal !== lastRefreshSignal) {
      lastRefreshSignal = refreshSignal;
      if (targetId) {
        loadDirectory(currentPath);
      }
    }
  });

  function handleNavigateUp() {
    if (!currentPath || currentPath === '/' || currentPath === '.') return;
    if (isLocal && /^[A-Za-z]:\\/.test(currentPath)) {
      const normalized = currentPath.replace(/\\+$/, '');
      const idx = normalized.lastIndexOf('\\');
      if (idx <= 2) {
        loadDirectory(normalized.slice(0, 3));
      } else {
        loadDirectory(normalized.slice(0, idx));
      }
      return;
    }
    const parts = currentPath.split('/').filter(Boolean);
    parts.pop();
    loadDirectory('/' + parts.join('/'));
  }

  function handlePathSubmit(e: SubmitEvent) {
    e.preventDefault();
    if (pathInputValue.trim()) {
      loadDirectory(pathInputValue.trim());
    }
  }

  function toggleSelect(item: ApiSftpFile) {
    if (selectedPaths.includes(item.path)) {
      onSelectionChange(selectedPaths.filter((p) => p !== item.path));
    } else {
      onSelectionChange([...selectedPaths, item.path]);
    }
  }

  async function handleDownload(item: ApiSftpFile) {
    if (isLocal) return;
    const config = getHostConfig();
    if (!config) return;
    try {
      await api.sftpDownload(config, item.path, item.name);
    } catch (err: unknown) {
      toast.error(err, 'Download failed');
    }
  }

  async function handleDelete(item: ApiSftpFile) {
    const confirmed = await toast.confirm(`Delete ${item.isDir ? 'folder' : 'file'} "${item.name}"?`);
    if (!confirmed) return;

    try {
      if (isLocal) {
        const { invoke } = await import('@tauri-apps/api/core');
        await invoke('local_fs_delete', { path: item.path });
      } else {
        const config = getHostConfig();
        if (!config) return;
        await api.sftpDelete(config, item.path);
      }
      await loadDirectory(currentPath);
    } catch (err: unknown) {
      toast.error(err, 'Delete failed');
    }
  }

  function joinChildPath(base: string, name: string): string {
    if (isLocal && /^[A-Za-z]:\\/.test(base)) {
      return `${base.replace(/\\+$/, '')}\\${name}`;
    }
    const clean = base.endsWith('/') && base.length > 1 ? base.slice(0, -1) : base;
    return clean === '/' ? `/${name}` : `${clean}/${name}`;
  }

  async function handleCreateFolder(e: SubmitEvent) {
    e.preventDefault();
    if (!newFolderName.trim()) return;
    const target = joinChildPath(currentPath, newFolderName.trim());

    try {
      if (isLocal) {
        const { invoke } = await import('@tauri-apps/api/core');
        await invoke('local_fs_mkdir', { path: target });
      } else {
        const config = getHostConfig();
        if (!config) return;
        await api.sftpMkdir(config, target);
      }
      isNewFolderOpen = false;
      newFolderName = '';
      await loadDirectory(currentPath);
    } catch (err: unknown) {
      toast.error(err, 'Failed to create folder');
    }
  }

  function openRenameModal(item: ApiSftpFile) {
    renamingItem = item;
    renameValue = item.name;
  }

  async function handleRenameSubmit(e: SubmitEvent) {
    e.preventDefault();
    if (!renamingItem) return;
    const trimmed = renameValue.trim();
    if (!trimmed || trimmed === renamingItem.name) {
      renamingItem = null;
      return;
    }

    const newPath = joinChildPath(currentPath, trimmed);
    try {
      if (isLocal) {
        const { invoke } = await import('@tauri-apps/api/core');
        await invoke('local_fs_rename', { oldPath: renamingItem.path, newPath });
      } else {
        const config = getHostConfig();
        if (!config) return;
        await api.sftpRename(config, renamingItem.path, newPath);
      }
      renamingItem = null;
      await loadDirectory(currentPath);
      toast.success(`Renamed to "${trimmed}".`);
    } catch (err: unknown) {
      toast.error(err, 'Rename failed');
    }
  }

  async function handleUploadChange(e: Event) {
    const input = e.target as HTMLInputElement;
    if (!input.files || input.files.length === 0) return;
    const files = Array.from(input.files);

    isUploading = true;
    try {
      for (const file of files) {
        if (isLocal) {
          const { invoke } = await import('@tauri-apps/api/core');
          const bytes = Array.from(new Uint8Array(await file.arrayBuffer()));
          await invoke('local_fs_write_bytes', { path: joinChildPath(currentPath, file.name), data: bytes });
        } else {
          const config = getHostConfig();
          if (!config) return;
          await api.sftpUpload(config, currentPath, file);
        }
      }
      await loadDirectory(currentPath);
    } catch (err: unknown) {
      toast.error(err, 'Upload failed');
    } finally {
      isUploading = false;
      input.value = '';
    }
  }

  function formatSize(bytes: number, isDir: boolean): string {
    if (isDir) return '—';
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  function getFileIcon(item: ApiSftpFile) {
    if (item.isDir) return Folder;
    const ext = item.name.split('.').pop()?.toLowerCase() || '';
    if (['zip', 'tar', 'gz', 'tgz', 'rar', '7z'].includes(ext)) return FileArchive;
    if (['ts', 'js', 'go', 'py', 'rs', 'json', 'yaml', 'yml', 'sh', 'svelte', 'html', 'css', 'sql'].includes(ext)) {
      return FileCode;
    }
    return FileText;
  }
</script>

<div class="flex flex-col h-full rounded-xl border border-[#22222c] bg-[#0e0e13] overflow-hidden">
  <!-- Pane Header: Target Selector + Actions -->
  <div class="flex items-center justify-between gap-2 border-b border-[#1c1c26] bg-[#13131a] px-3 py-2 shrink-0">
    <div class="flex items-center gap-2 min-w-0 flex-1">
      <span class="text-[10px] font-semibold uppercase tracking-wider text-neutral-500 shrink-0">{paneTitle}</span>
      <div class="flex items-center gap-1.5 rounded-lg border border-[#262635] bg-[#0c0c10] px-2 py-1 min-w-0 flex-1">
        {#if isLocal}
          <Laptop class="h-3.5 w-3.5 text-indigo-400 shrink-0" />
        {:else}
          <Server class="h-3.5 w-3.5 text-emerald-400 shrink-0" />
        {/if}
        <select
          value={targetId}
          onchange={(e) => onTargetChange((e.currentTarget as HTMLSelectElement).value)}
          class="bg-transparent text-xs font-medium text-white outline-none w-full truncate"
        >
          {#if platform.isNativeTauri}
            <option value="local" class="bg-[#121218]">Local Machine</option>
          {/if}
          {#each hosts as h (h.id)}
            <option value={h.id} class="bg-[#121218]">
              {h.label} ({h.username}@{h.address})
            </option>
          {/each}
        </select>
      </div>
    </div>

    <div class="flex items-center gap-1 shrink-0">
      <button
        type="button"
        onclick={() => (isNewFolderOpen = true)}
        disabled={!targetId}
        class="flex h-9 w-9 sm:h-7 sm:w-7 items-center justify-center rounded-lg border border-[#242432] bg-[#171720] text-neutral-300 hover:text-white disabled:opacity-40 transition"
        title="New Folder"
      >
        <FolderPlus class="h-4 w-4 sm:h-3.5 sm:w-3.5" />
      </button>

      <input
        bind:this={fileInputRef}
        type="file"
        multiple
        class="hidden"
        onchange={handleUploadChange}
      />
      <button
        type="button"
        onclick={() => fileInputRef?.click()}
        disabled={!targetId || isUploading}
        class="flex h-9 w-9 sm:h-7 sm:w-7 items-center justify-center rounded-lg border border-[#242432] bg-[#171720] text-neutral-300 hover:text-white disabled:opacity-40 transition"
        title="Upload from OS Picker"
      >
        <Upload class="h-4 w-4 sm:h-3.5 sm:w-3.5" />
      </button>

      <button
        type="button"
        onclick={() => loadDirectory(currentPath)}
        disabled={!targetId || loadState === 'loading'}
        class="flex h-9 w-9 sm:h-7 sm:w-7 items-center justify-center rounded-lg border border-[#242432] bg-[#171720] text-neutral-300 hover:text-white disabled:opacity-40 transition"
        title="Refresh"
      >
        <RefreshCw class="h-4 w-4 sm:h-3.5 sm:w-3.5 {loadState === 'loading' ? 'animate-spin' : ''}" />
      </button>
    </div>
  </div>

  <!-- Path + Search Bar -->
  <div class="flex items-center gap-1.5 border-b border-[#1c1c26] bg-[#101016] px-2.5 py-1.5 shrink-0">
    <button
      type="button"
      onclick={handleNavigateUp}
      disabled={!currentPath || currentPath === '/' || loadState === 'loading'}
      class="flex h-8 w-8 sm:h-7 sm:w-7 items-center justify-center rounded-md border border-[#242432] bg-[#16161f] text-neutral-300 hover:text-white disabled:opacity-40 transition shrink-0"
      title="Up Directory"
    >
      <CornerLeftUp class="h-4 w-4 sm:h-3.5 sm:w-3.5" />
    </button>

    <form onsubmit={handlePathSubmit} class="flex-1 min-w-0">
      <input
        type="text"
        bind:value={pathInputValue}
        placeholder="/path"
        autocapitalize="none"
        autocorrect="off"
        spellcheck={false}
        class="w-full rounded-md border border-[#242432] bg-[#0a0a0e] px-2.5 py-1.5 sm:py-1 text-xs sm:text-[11px] font-mono text-neutral-200 outline-none focus:border-emerald-500/60"
      />
    </form>

    <div class="relative w-28 sm:w-32 shrink-0">
      <Search class="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-neutral-500" />
      <input
        type="text"
        bind:value={searchQuery}
        placeholder="Filter..."
        autocapitalize="none"
        autocorrect="off"
        spellcheck={false}
        class="w-full rounded-md border border-[#242432] bg-[#0a0a0e] pl-6 pr-2 py-1.5 sm:py-1 text-xs sm:text-[11px] text-neutral-200 outline-none focus:border-emerald-500/60"
      />
    </div>

    <button
      type="button"
      onclick={() => (showHidden = !showHidden)}
      class="flex h-8 w-8 sm:h-7 sm:w-7 items-center justify-center rounded-md border transition shrink-0 cursor-pointer {showHidden ? 'border-emerald-500/40 bg-emerald-500/15 text-emerald-300' : 'border-[#242432] bg-[#16161f] text-neutral-400 hover:text-white'}"
      title={showHidden ? 'Hide dotfiles' : 'Show hidden files (dotfiles)'}
      aria-label={showHidden ? 'Hide dotfiles' : 'Show hidden files'}
    >
      {#if showHidden}
        <Eye class="h-3.5 w-3.5" />
      {:else}
        <EyeOff class="h-3.5 w-3.5" />
      {/if}
    </button>
  </div>

  <!-- File List -->
  <div
    data-sftp-pane={paneId}
    class="flex-1 overflow-y-auto transition select-none {isPaneDragOver ? 'ring-2 ring-inset ring-emerald-500/70 bg-emerald-500/5' : ''}"
    role="region"
    aria-label="{paneTitle} file list dropzone"
  >
    {#if loadState === 'loading'}
      <div class="flex h-full items-center justify-center text-xs text-neutral-400">
        Loading...
      </div>
    {:else if loadState === 'error'}
      <div class="flex h-full flex-col items-center justify-center p-6 text-center space-y-2">
        <AlertCircle class="h-6 w-6 text-rose-400" />
        <p class="text-xs text-rose-300 max-w-xs">{errorMessage}</p>
        <div class="flex flex-wrap items-center justify-center gap-2">
          {#if errorMessage?.includes('HOST_KEY_MISMATCH')}
            <button
              type="button"
              onclick={handleTrustHostKey}
              class="rounded-md bg-amber-600 px-3 py-1 text-xs font-semibold text-white hover:bg-amber-500"
            >
              Trust New Key & Retry
            </button>
          {/if}
          <button
            type="button"
            onclick={() => loadDirectory(currentPath)}
            class="rounded-md bg-[#1a1a24] border border-[#2b2b3a] px-3 py-1 text-xs text-white hover:bg-[#222230]"
          >
            Retry
          </button>
        </div>
      </div>
    {:else if loadState === 'empty' || filteredItems.length === 0}
      <div class="flex h-full flex-col items-center justify-center p-6 text-center text-xs text-neutral-500 gap-2">
        {#if isPaneDragOver}
          <span>Drop here to transfer</span>
        {:else if searchQuery.trim()}
          <span>No files match "{searchQuery.trim()}"</span>
          <button
            type="button"
            onclick={() => (searchQuery = '')}
            class="rounded-md border border-white/[0.1] bg-[#161620] px-2.5 py-1 text-[11px] text-neutral-300 hover:text-white cursor-pointer"
          >
            Clear Filter
          </button>
        {:else}
          <span>Empty directory</span>
        {/if}
      </div>
    {:else if platform.isMobile}
      <!-- Mobile Touch File List -->
      <div class="divide-y divide-[#171720]">
        {#each filteredItems as item (item.path)}
          {@const Icon = getFileIcon(item)}
          {@const isSelected = selectedPaths.includes(item.path)}
          {@const isFolderDropTarget = item.isDir && dragHoverPane === paneId && dragHoverFolderPath === item.path}
          <div
            data-sftp-folder={item.isDir ? item.path : undefined}
            data-sftp-folder-pane={item.isDir ? paneId : undefined}
            onpointerdown={(e) => handleRowPointerDown(e, item)}
            role="listitem"
            class="flex items-center gap-2.5 px-3 py-2.5 transition {isFolderDropTarget ? 'bg-emerald-500/20 ring-1 ring-inset ring-emerald-500/70' : isSelected ? 'bg-emerald-500/10' : 'active:bg-[#15151e]'}"
          >
            <button
              type="button"
              data-no-drag
              onclick={() => toggleSelect(item)}
              class="flex h-6 w-6 shrink-0 items-center justify-center rounded border transition {isSelected ? 'border-emerald-500 bg-emerald-500 text-black' : 'border-[#323244] bg-[#0c0c11] text-transparent'}"
              aria-label="Select item"
            >
              <Check class="h-3.5 w-3.5" />
            </button>

            <button
              type="button"
              onclick={() => handleRowActionClick(item)}
              class="flex flex-1 items-center gap-2.5 min-w-0 text-left py-0.5"
            >
              <Icon class="h-4.5 w-4.5 shrink-0 {item.isDir ? 'text-amber-400' : 'text-neutral-400'}" />
              <div class="min-w-0 flex-1">
                <div class="text-xs truncate {item.isDir ? 'font-medium text-white' : 'text-neutral-200'}">{item.name}</div>
                {#if !item.isDir}
                  <div class="text-[10px] font-mono text-neutral-500">{formatSize(item.size, item.isDir)}</div>
                {/if}
              </div>
            </button>

            <div class="flex items-center gap-1 shrink-0" data-no-drag>
              <button
                type="button"
                onclick={() => openRenameModal(item)}
                class="flex h-9 w-9 items-center justify-center rounded-lg text-neutral-400 active:bg-[#222230] active:text-emerald-400 transition"
                aria-label="Rename"
              >
                <Pencil class="h-4 w-4" />
              </button>
              {#if !item.isDir && !isLocal}
                <button
                  type="button"
                  onclick={() => handleDownload(item)}
                  class="flex h-9 w-9 items-center justify-center rounded-lg text-neutral-400 active:bg-[#222230] active:text-emerald-400 transition"
                  aria-label="Download"
                >
                  <Download class="h-4 w-4" />
                </button>
              {/if}
              <button
                type="button"
                onclick={() => handleDelete(item)}
                class="flex h-9 w-9 items-center justify-center rounded-lg text-neutral-400 active:bg-[#222230] active:text-rose-400 transition"
                aria-label="Delete"
              >
                <Trash2 class="h-4 w-4" />
              </button>
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <!-- Desktop File Table -->
      <table class="w-full text-left border-collapse">
        <thead class="sticky top-0 bg-[#121218] text-[10px] uppercase tracking-wider text-neutral-500 border-b border-[#1d1d27]">
          <tr>
            <th class="w-8 px-2.5 py-1.5"></th>
            <th class="py-1.5 px-2 font-medium">Name</th>
            <th class="py-1.5 px-2 font-medium text-right w-20">Size</th>
            <th class="py-1.5 px-2 font-medium text-right w-16">Actions</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-[#171720] text-xs">
          {#each filteredItems as item (item.path)}
            {@const Icon = getFileIcon(item)}
            {@const isSelected = selectedPaths.includes(item.path)}
            {@const isFolderDropTarget = item.isDir && dragHoverPane === paneId && dragHoverFolderPath === item.path}
            <tr
              data-sftp-folder={item.isDir ? item.path : undefined}
              data-sftp-folder-pane={item.isDir ? paneId : undefined}
              onpointerdown={(e) => handleRowPointerDown(e, item)}
              class="group transition cursor-grab active:cursor-grabbing {isFolderDropTarget ? 'bg-emerald-500/20 ring-1 ring-inset ring-emerald-500/70' : isSelected ? 'bg-emerald-500/10' : 'hover:bg-[#15151e]'}"
            >
              <td class="px-2.5 py-1.5" data-no-drag>
                <button
                  type="button"
                  onclick={() => toggleSelect(item)}
                  class="flex h-4 w-4 items-center justify-center rounded border transition {isSelected ? 'border-emerald-500 bg-emerald-500 text-black' : 'border-[#323244] bg-[#0c0c11] text-transparent hover:border-neutral-400'}"
                  aria-label="Select item"
                >
                  <Check class="h-3 w-3" />
                </button>
              </td>
              <td class="py-1.5 px-2 min-w-0">
                <button
                  type="button"
                  onclick={() => handleRowActionClick(item)}
                  class="flex items-center gap-2 w-full text-left min-w-0"
                >
                  <Icon class="h-3.5 w-3.5 shrink-0 {item.isDir ? 'text-amber-400' : 'text-neutral-400'}" />
                  <span class="truncate {item.isDir ? 'font-medium text-white' : 'text-neutral-200'}">
                    {item.name}
                  </span>
                </button>
              </td>
              <td class="py-1.5 px-2 text-right font-mono text-[11px] text-neutral-400 whitespace-nowrap">
                {formatSize(item.size, item.isDir)}
              </td>
              <td class="py-1.5 px-2 text-right whitespace-nowrap" data-no-drag>
                <div class="inline-flex items-center gap-1 opacity-80 group-hover:opacity-100">
                  <button
                    type="button"
                    onclick={() => openRenameModal(item)}
                    class="rounded p-1 text-neutral-400 hover:bg-[#222230] hover:text-emerald-400 transition cursor-pointer"
                    title="Rename"
                  >
                    <Pencil class="h-3.5 w-3.5" />
                  </button>
                  {#if !item.isDir && !isLocal}
                    <button
                      type="button"
                      onclick={() => handleDownload(item)}
                      class="rounded p-1 text-neutral-400 hover:bg-[#222230] hover:text-emerald-400 transition"
                      title="Download to browser"
                    >
                      <Download class="h-3.5 w-3.5" />
                    </button>
                  {/if}
                  <button
                    type="button"
                    onclick={() => handleDelete(item)}
                    class="rounded p-1 text-neutral-400 hover:bg-[#222230] hover:text-rose-400 transition"
                    title="Delete"
                  >
                    <Trash2 class="h-3.5 w-3.5" />
                  </button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</div>

<svelte:window onkeydown={(e) => {
  if (e.key === 'Escape') {
    if (renamingItem) renamingItem = null;
    if (isNewFolderOpen) isNewFolderOpen = false;
  }
}} />

{#if isNewFolderOpen}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-4 backdrop-blur-xs"
    onclick={(e) => { if (e.target === e.currentTarget) isNewFolderOpen = false; }}
    role="presentation"
  >
    <form
      onsubmit={handleCreateFolder}
      class="w-full max-w-xs rounded-2xl border border-[#262635] bg-[#14141c] p-4 space-y-3 shadow-2xl"
    >
      <div class="flex items-center justify-between">
        <h3 class="text-xs font-semibold text-white">New Folder</h3>
        <button type="button" onclick={() => (isNewFolderOpen = false)} class="text-neutral-400 hover:text-white">
          <X class="h-4 w-4" />
        </button>
      </div>
      <input
        type="text"
        bind:value={newFolderName}
        placeholder="folder-name"
        class="w-full rounded-lg border border-[#2a2a38] bg-[#0c0c10] px-3 py-2 text-xs text-white outline-none focus:border-emerald-500"
      />
      <div class="flex justify-end gap-2">
        <button
          type="button"
          onclick={() => (isNewFolderOpen = false)}
          class="rounded-lg px-3 py-1.5 text-xs text-neutral-400 hover:text-white"
        >
          Cancel
        </button>
        <button
          type="submit"
          class="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500"
        >
          Create
        </button>
      </div>
    </form>
  </div>
{/if}

{#if renamingItem}
  <div
    class="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-4 backdrop-blur-xs"
    onclick={(e) => { if (e.target === e.currentTarget) renamingItem = null; }}
    role="presentation"
  >
    <form
      onsubmit={handleRenameSubmit}
      class="w-full max-w-xs rounded-2xl border border-[#262635] bg-[#14141c] p-4 space-y-3 shadow-2xl"
    >
      <div class="flex items-center justify-between">
        <h3 class="text-xs font-semibold text-white">Rename {renamingItem.isDir ? 'Folder' : 'File'}</h3>
        <button type="button" onclick={() => (renamingItem = null)} class="text-neutral-400 hover:text-white">
          <X class="h-4 w-4" />
        </button>
      </div>
      <input
        type="text"
        bind:value={renameValue}
        class="w-full rounded-lg border border-[#2a2a38] bg-[#0c0c10] px-3 py-2 text-xs text-white outline-none focus:border-emerald-500"
        required
      />
      <div class="flex justify-end gap-2">
        <button
          type="button"
          onclick={() => (renamingItem = null)}
          class="rounded-lg px-3 py-1.5 text-xs text-neutral-400 hover:text-white"
        >
          Cancel
        </button>
        <button
          type="submit"
          class="rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500"
        >
          Rename
        </button>
      </div>
    </form>
  </div>
{/if}
