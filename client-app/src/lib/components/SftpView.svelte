<script lang="ts">
  import { api, type ApiSftpFile, type SftpHostConfig } from '../api/client';
  import { platform } from '../stores/platform.svelte';
  import { toast } from '../stores/toast.svelte';
  import SftpPane, { type SftpHostItem } from './SftpPane.svelte';
  import { ArrowRight, ArrowLeft, Copy, Scissors, Folder, FileText } from '@lucide/svelte';

  interface Props {
    hosts: SftpHostItem[];
    initialHostId?: string;
  }

  let { hosts = [], initialHostId = '' }: Props = $props();

  let leftTargetId = $state<string>(platform.isNativeTauri ? 'local' : '');
  let rightTargetId = $state<string>('');

  let leftPath = $state('');
  let rightPath = $state('');
  let leftSelection = $state<string[]>([]);
  let rightSelection = $state<string[]>([]);
  let leftItems = $state<ApiSftpFile[]>([]);
  let rightItems = $state<ApiSftpFile[]>([]);
  let refreshCounter = $state(0);

  let isTransferring = $state(false);
  let transferStatus = $state<string | null>(null);
  let mobileActivePane = $state<'left' | 'right'>('right');

  $effect(() => {
    if (!leftTargetId) {
      if (platform.isNativeTauri) {
        leftTargetId = 'local';
      } else if (hosts.length > 0) {
        leftTargetId = hosts[0].id;
      }
    }
    if (initialHostId && initialHostId !== rightTargetId) {
      rightTargetId = initialHostId;
    } else if (!rightTargetId && hosts.length > 0) {
      rightTargetId = hosts[0].id;
    }
  });

  function hostConfigFor(targetId: string): SftpHostConfig | null {
    const h = hosts.find((item) => item.id === targetId);
    if (!h) return null;
    return {
      address: h.address,
      port: h.port || 22,
      username: h.username,
      authType: h.authType || 'password',
      password: h.password,
      privateKey: h.privateKey,
      passphrase: h.passphrase
    };
  }

  async function readBytesFromTarget(targetId: string, filePath: string): Promise<Uint8Array> {
    if (targetId === 'local') {
      const { invoke } = await import('@tauri-apps/api/core');
      const arr = await invoke<number[]>('local_fs_read_bytes', { path: filePath });
      return new Uint8Array(arr);
    }
    const cfg = hostConfigFor(targetId);
    if (!cfg) throw new Error('Source host not found');
    const blob = await api.sftpDownloadBlob(cfg, filePath);
    return new Uint8Array(await blob.arrayBuffer());
  }

  async function writeBytesToTarget(
    targetId: string,
    destDir: string,
    filename: string,
    bytes: Uint8Array
  ): Promise<void> {
    if (targetId === 'local') {
      const { invoke } = await import('@tauri-apps/api/core');
      const base = destDir.endsWith('/') ? destDir.slice(0, -1) : destDir;
      await invoke('local_fs_write_bytes', {
        path: `${base}/${filename}`,
        data: Array.from(bytes)
      });
      return;
    }
    const cfg = hostConfigFor(targetId);
    if (!cfg) throw new Error('Destination host not found');
    const copyBuffer = new Uint8Array(bytes).buffer;
    const file = new File([copyBuffer], filename);
    await api.sftpUpload(cfg, destDir, file);
  }

  async function deleteFromTarget(targetId: string, filePath: string): Promise<void> {
    if (targetId === 'local') {
      const { invoke } = await import('@tauri-apps/api/core');
      await invoke('local_fs_delete', { path: filePath });
      return;
    }
    const cfg = hostConfigFor(targetId);
    if (!cfg) throw new Error('Source host not found');
    await api.sftpDelete(cfg, filePath);
  }

  async function listDirFromTarget(targetId: string, dirPath: string): Promise<ApiSftpFile[]> {
    if (targetId === 'local') {
      const { invoke } = await import('@tauri-apps/api/core');
      const res = await invoke<{ path: string; items: ApiSftpFile[] }>('local_fs_list', { path: dirPath });
      return res.items || [];
    }
    const cfg = hostConfigFor(targetId);
    if (!cfg) throw new Error('Source host not found');
    const res = await api.sftpList(cfg, dirPath);
    return res.items || [];
  }

  async function mkdirOnTarget(targetId: string, dirPath: string): Promise<void> {
    if (targetId === 'local') {
      const { invoke } = await import('@tauri-apps/api/core');
      await invoke('local_fs_mkdir', { path: dirPath });
      return;
    }
    const cfg = hostConfigFor(targetId);
    if (!cfg) throw new Error('Destination host not found');
    await api.sftpMkdir(cfg, dirPath);
  }

  function joinPath(base: string, name: string): string {
    const clean = base.endsWith('/') && base.length > 1 ? base.slice(0, -1) : base;
    return clean === '/' ? `/${name}` : `${clean}/${name}`;
  }

  async function transferSingleEntry(
    srcTarget: string,
    dstTarget: string,
    item: ApiSftpFile,
    dstDir: string,
    move: boolean
  ): Promise<void> {
    if (!item.isDir) {
      transferStatus = `${move ? 'Moving' : 'Copying'} ${item.name}...`;
      const bytes = await readBytesFromTarget(srcTarget, item.path);
      await writeBytesToTarget(dstTarget, dstDir, item.name, bytes);
      if (move) {
        await deleteFromTarget(srcTarget, item.path);
      }
      return;
    }

    const newDir = joinPath(dstDir, item.name);
    transferStatus = `${move ? 'Moving' : 'Copying'} folder ${item.name}...`;
    try {
      await mkdirOnTarget(dstTarget, newDir);
    } catch {
      // Destination folder may already exist
    }

    const children = await listDirFromTarget(srcTarget, item.path);
    for (const child of children) {
      await transferSingleEntry(srcTarget, dstTarget, child, newDir, move);
    }

    if (move) {
      await deleteFromTarget(srcTarget, item.path);
    }
  }

  async function runTransfer(
    srcTarget: string,
    dstTarget: string,
    entries: ApiSftpFile[],
    dstDir: string,
    move: boolean
  ) {
    if (!srcTarget || !dstTarget || !dstDir || entries.length === 0 || isTransferring) return;

    const validEntries = entries.filter((item) => {
      if (srcTarget === dstTarget) {
        if (dstDir === item.path || dstDir.startsWith(`${item.path}/`)) return false;
        const parent = item.path.slice(0, item.path.lastIndexOf('/')) || '/';
        if (parent === dstDir) return false;
      }
      return true;
    });
    if (validEntries.length === 0) return;

    isTransferring = true;
    try {
      for (const entry of validEntries) {
        await transferSingleEntry(srcTarget, dstTarget, entry, dstDir, move);
      }
      leftSelection = [];
      rightSelection = [];
      refreshCounter += 1;
      toast.success(`${move ? 'Moved' : 'Copied'} ${validEntries.length} item${validEntries.length > 1 ? 's' : ''}`);
    } catch (err: unknown) {
      toast.error(err, 'Transfer failed');
    } finally {
      isTransferring = false;
      transferStatus = null;
    }
  }

  async function transferFiles(direction: 'left-to-right' | 'right-to-left', move: boolean) {
    const srcTarget = direction === 'left-to-right' ? leftTargetId : rightTargetId;
    const dstTarget = direction === 'left-to-right' ? rightTargetId : leftTargetId;
    const dstPath = direction === 'left-to-right' ? rightPath : leftPath;
    const selected = direction === 'left-to-right' ? leftSelection : rightSelection;
    const srcItems = direction === 'left-to-right' ? leftItems : rightItems;
    const entries = srcItems.filter((item) => selected.includes(item.path));
    await runTransfer(srcTarget, dstTarget, entries, dstPath, move);
  }

  async function handleDropTransfer(
    toPane: 'left' | 'right',
    payload: { fromPane: 'left' | 'right'; paths: string[]; destDir: string; move?: boolean }
  ) {
    const srcTarget = payload.fromPane === 'left' ? leftTargetId : rightTargetId;
    const dstTarget = toPane === 'left' ? leftTargetId : rightTargetId;
    const srcItems = payload.fromPane === 'left' ? leftItems : rightItems;
    const entries = srcItems.filter((item) => payload.paths.includes(item.path));
    const isMove = payload.move ?? payload.fromPane === toPane;
    await runTransfer(srcTarget, dstTarget, entries, payload.destDir, isMove);
  }

  interface SftpDragState {
    fromPane: 'left' | 'right';
    paths: string[];
    label: string;
    isDir: boolean;
    pointerId: number;
    pointerType: string;
    startX: number;
    startY: number;
    x: number;
    y: number;
    active: boolean;
    moveMode: boolean;
    hoverPane: 'left' | 'right' | null;
    hoverFolderPath: string | null;
  }

  let dragState = $state<SftpDragState | null>(null);
  let suppressClickUntil = 0;

  function suppressNextClick(): boolean {
    return Date.now() < suppressClickUntil;
  }

  function startPaneDrag(pane: 'left' | 'right', e: PointerEvent, item: ApiSftpFile) {
    if (isTransferring) return;
    if (e.pointerType === 'touch') return; // Touch uses checkboxes + Copy/Move bar so vertical scroll stays smooth
    const selected = pane === 'left' ? leftSelection : rightSelection;
    const paths = selected.includes(item.path) && selected.length > 0 ? [...selected] : [item.path];
    dragState = {
      fromPane: pane,
      paths,
      label: paths.length > 1 ? `${paths.length} items` : item.name,
      isDir: paths.length === 1 && item.isDir,
      pointerId: e.pointerId,
      pointerType: e.pointerType,
      startX: e.clientX,
      startY: e.clientY,
      x: e.clientX,
      y: e.clientY,
      active: false,
      moveMode: e.shiftKey,
      hoverPane: null,
      hoverFolderPath: null
    };
  }

  function updateDragHover(clientX: number, clientY: number, shiftKey: boolean) {
    if (!dragState) return;
    const els = document.elementsFromPoint(clientX, clientY);
    let hoverPane: 'left' | 'right' | null = null;
    let hoverFolderPath: string | null = null;

    for (const el of els) {
      if (!(el instanceof HTMLElement)) continue;
      if (!hoverFolderPath && el.dataset.sftpFolder && (el.dataset.sftpFolderPane === 'left' || el.dataset.sftpFolderPane === 'right')) {
        hoverFolderPath = el.dataset.sftpFolder;
        hoverPane = el.dataset.sftpFolderPane;
        break;
      }
      if (!hoverPane && (el.dataset.sftpPane === 'left' || el.dataset.sftpPane === 'right')) {
        hoverPane = el.dataset.sftpPane;
        break;
      }
    }

    dragState.hoverPane = hoverPane;
    dragState.hoverFolderPath = hoverFolderPath;
    dragState.moveMode = shiftKey || (hoverPane === dragState.fromPane && Boolean(hoverFolderPath));
  }

  function handleWindowPointerMove(e: PointerEvent) {
    if (!dragState || e.pointerId !== dragState.pointerId) return;
    dragState.x = e.clientX;
    dragState.y = e.clientY;
    if (!dragState.active) {
      if (Math.hypot(e.clientX - dragState.startX, e.clientY - dragState.startY) < 6) return;
      dragState.active = true;
    }
    updateDragHover(e.clientX, e.clientY, e.shiftKey);
  }

  function handleWindowPointerUp(e: PointerEvent) {
    if (!dragState || e.pointerId !== dragState.pointerId) return;
    const current = dragState;
    dragState = null;

    if (!current.active) return;
    suppressClickUntil = Date.now() + 250;

    if (!current.hoverPane) return;
    const destDir = current.hoverFolderPath || (current.hoverPane === 'left' ? leftPath : rightPath);
    if (!destDir) return;
    if (current.hoverPane === current.fromPane && !current.hoverFolderPath) return;

    void handleDropTransfer(current.hoverPane, {
      fromPane: current.fromPane,
      paths: current.paths,
      destDir,
      move: current.moveMode
    });
  }

  function handleWindowKeyChange(e: KeyboardEvent) {
    if (!dragState?.active) return;
    dragState.moveMode =
      e.shiftKey || (dragState.hoverPane === dragState.fromPane && Boolean(dragState.hoverFolderPath));
  }
</script>

<svelte:window
  onpointermove={handleWindowPointerMove}
  onpointerup={handleWindowPointerUp}
  onpointercancel={handleWindowPointerUp}
  onkeydown={handleWindowKeyChange}
  onkeyup={handleWindowKeyChange}
/>

<div class="flex h-full flex-col bg-[#0a0a0c] p-3 gap-2.5 overflow-hidden">
  <!-- Dual-Pane Transfer Bar -->
  <div class="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-[#20202b] bg-[#121218] px-3 py-2 shrink-0">
    <div class="flex items-center gap-2">
      <span class="text-xs font-semibold text-white">SFTP Dual-Pane</span>
      {#if transferStatus}
        <span class="rounded-full bg-emerald-500/15 border border-emerald-500/30 px-2.5 py-0.5 text-[11px] text-emerald-300 animate-pulse">
          {transferStatus}
        </span>
      {:else}
        <span class="text-[11px] text-neutral-400 hidden sm:inline">
          Drag between panes to Copy (hold Shift to Move), or drop onto a folder
        </span>
      {/if}
    </div>

    <div class="flex items-center gap-1.5">
      {#if platform.isMobile}
        <div class="flex rounded-lg border border-[#262635] bg-[#0c0c10] p-0.5 text-[11px]">
          <button
            type="button"
            onclick={() => (mobileActivePane = 'left')}
            class="rounded-md px-2.5 py-1 transition {mobileActivePane === 'left' ? 'bg-emerald-500/20 text-emerald-300 font-medium' : 'text-neutral-400'}"
          >
            Left
          </button>
          <button
            type="button"
            onclick={() => (mobileActivePane = 'right')}
            class="rounded-md px-2.5 py-1 transition {mobileActivePane === 'right' ? 'bg-emerald-500/20 text-emerald-300 font-medium' : 'text-neutral-400'}"
          >
            Right
          </button>
        </div>
      {/if}

      {#if leftSelection.length > 0}
        <button
          type="button"
          disabled={isTransferring || !rightTargetId}
          onclick={() => transferFiles('left-to-right', false)}
          class="flex items-center gap-1 rounded-lg bg-emerald-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-emerald-500 disabled:opacity-40 transition"
        >
          <Copy class="h-3 w-3" /> Copy ({leftSelection.length}) <ArrowRight class="h-3 w-3" />
        </button>
        <button
          type="button"
          disabled={isTransferring || !rightTargetId}
          onclick={() => transferFiles('left-to-right', true)}
          class="flex items-center gap-1 rounded-lg border border-amber-500/40 bg-amber-500/15 px-2.5 py-1 text-xs font-medium text-amber-300 hover:bg-amber-500/25 disabled:opacity-40 transition"
        >
          <Scissors class="h-3 w-3" /> Move <ArrowRight class="h-3 w-3" />
        </button>
      {/if}

      {#if rightSelection.length > 0}
        <button
          type="button"
          disabled={isTransferring || !leftTargetId}
          onclick={() => transferFiles('right-to-left', false)}
          class="flex items-center gap-1 rounded-lg bg-emerald-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-emerald-500 disabled:opacity-40 transition"
        >
          <ArrowLeft class="h-3 w-3" /> Copy ({rightSelection.length})
        </button>
        <button
          type="button"
          disabled={isTransferring || !leftTargetId}
          onclick={() => transferFiles('right-to-left', true)}
          class="flex items-center gap-1 rounded-lg border border-amber-500/40 bg-amber-500/15 px-2.5 py-1 text-xs font-medium text-amber-300 hover:bg-amber-500/25 disabled:opacity-40 transition"
        >
          <ArrowLeft class="h-3 w-3" /> Move
        </button>
      {/if}
    </div>
  </div>

  <!-- Panes Container -->
  {#if platform.isMobile}
    <div class="flex-1 min-h-0">
      {#if mobileActivePane === 'left'}
        <SftpPane
          paneId="left"
          paneTitle="Left"
          {hosts}
          targetId={leftTargetId}
          onTargetChange={(id) => (leftTargetId = id)}
          selectedPaths={leftSelection}
          onSelectionChange={(paths) => (leftSelection = paths)}
          currentPath={leftPath}
          onPathChange={(p) => (leftPath = p)}
          onItemsLoaded={(items) => (leftItems = items)}
          onStartDrag={(e, item) => startPaneDrag('left', e, item)}
          dragHoverPane={dragState?.active ? dragState.hoverPane : null}
          dragHoverFolderPath={dragState?.active ? dragState.hoverFolderPath : null}
          dragSourcePane={dragState?.active ? dragState.fromPane : null}
          {suppressNextClick}
          refreshSignal={refreshCounter}
        />
      {:else}
        <SftpPane
          paneId="right"
          paneTitle="Right"
          {hosts}
          targetId={rightTargetId}
          onTargetChange={(id) => (rightTargetId = id)}
          selectedPaths={rightSelection}
          onSelectionChange={(paths) => (rightSelection = paths)}
          currentPath={rightPath}
          onPathChange={(p) => (rightPath = p)}
          onItemsLoaded={(items) => (rightItems = items)}
          onStartDrag={(e, item) => startPaneDrag('right', e, item)}
          dragHoverPane={dragState?.active ? dragState.hoverPane : null}
          dragHoverFolderPath={dragState?.active ? dragState.hoverFolderPath : null}
          dragSourcePane={dragState?.active ? dragState.fromPane : null}
          {suppressNextClick}
          refreshSignal={refreshCounter}
        />
      {/if}
    </div>
  {:else}
    <div class="grid flex-1 grid-cols-2 gap-2.5 min-h-0">
      <SftpPane
        paneId="left"
        paneTitle="Left"
        {hosts}
        targetId={leftTargetId}
        onTargetChange={(id) => (leftTargetId = id)}
        selectedPaths={leftSelection}
        onSelectionChange={(paths) => (leftSelection = paths)}
        currentPath={leftPath}
        onPathChange={(p) => (leftPath = p)}
        onItemsLoaded={(items) => (leftItems = items)}
        onStartDrag={(e, item) => startPaneDrag('left', e, item)}
        dragHoverPane={dragState?.active ? dragState.hoverPane : null}
        dragHoverFolderPath={dragState?.active ? dragState.hoverFolderPath : null}
        dragSourcePane={dragState?.active ? dragState.fromPane : null}
        {suppressNextClick}
        refreshSignal={refreshCounter}
      />
      <SftpPane
        paneId="right"
        paneTitle="Right"
        {hosts}
        targetId={rightTargetId}
        onTargetChange={(id) => (rightTargetId = id)}
        selectedPaths={rightSelection}
        onSelectionChange={(paths) => (rightSelection = paths)}
        currentPath={rightPath}
        onPathChange={(p) => (rightPath = p)}
        onItemsLoaded={(items) => (rightItems = items)}
        onStartDrag={(e, item) => startPaneDrag('right', e, item)}
        dragHoverPane={dragState?.active ? dragState.hoverPane : null}
        dragHoverFolderPath={dragState?.active ? dragState.hoverFolderPath : null}
        dragSourcePane={dragState?.active ? dragState.fromPane : null}
        {suppressNextClick}
        refreshSignal={refreshCounter}
      />
    </div>
  {/if}
</div>

{#if dragState?.active}
  <div
    class="pointer-events-none fixed z-[9999] flex items-center gap-2 rounded-lg border bg-[#16161f]/95 px-2.5 py-1.5 text-xs text-white shadow-2xl backdrop-blur-sm {dragState.moveMode ? 'border-amber-500/60' : 'border-emerald-500/60'}"
    style="left: {dragState.x + 14}px; top: {dragState.y + 14}px;"
  >
    {#if dragState.isDir}
      <Folder class="h-3.5 w-3.5 text-amber-400 shrink-0" />
    {:else}
      <FileText class="h-3.5 w-3.5 text-neutral-300 shrink-0" />
    {/if}
    <span class="max-w-[180px] truncate font-medium">{dragState.label}</span>
    <span
      class="rounded px-1.5 py-0.5 text-[10px] font-semibold {dragState.moveMode ? 'bg-amber-500/20 text-amber-300' : 'bg-emerald-500/20 text-emerald-300'}"
    >
      {dragState.moveMode ? 'Move' : 'Copy'}
    </span>
  </div>
{/if}
