<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import type { TerminalHostConfig } from '../stores/terminal.svelte';
  import { platform } from '../stores/platform.svelte';
  import { formatErrorMessage } from '../stores/toast.svelte';
  import {
    Share2,
    RefreshCw,
    Trash2,
    AlertCircle,
    Columns2,
    Rows2,
    X,
    Keyboard
  } from 'lucide-svelte';

  interface Props {
    title: string;
    sessionId?: string;
    hostId: string;
    config?: TerminalHostConfig;
    active?: boolean;
    isSplitPane?: boolean;
    onShareMultiplayer: () => void;
    onSplitRight?: () => void;
    onSplitDown?: () => void;
    onClosePane?: () => void;
  }

  let {
    title = 'Active Terminal Session',
    sessionId = '',
    hostId,
    config,
    active = true,
    isSplitPane = false,
    onShareMultiplayer,
    onSplitRight,
    onSplitDown,
    onClosePane
  }: Props = $props();

  const activeSessionId = $derived(sessionId || hostId);

  let terminalContainer: HTMLDivElement | null = $state(null);
  let term: Terminal | null = null;
  let fitAddon: FitAddon | null = null;
  let resizeObserver: ResizeObserver | null = null;

  let unlistenData: (() => void) | null = null;
  let unlistenExit: (() => void) | null = null;

  // 4 UI States
  let connectionState = $state<'connecting' | 'connected' | 'disconnected' | 'error'>('connecting');
  let errorMessage = $state<string | null>(null);
  let showAccessoryBar = $state(true);
  let dockTop = $state<boolean>(
    typeof window !== 'undefined' && localStorage.getItem('opentermius_keybar_dock') === 'top'
  );
  let ctrlActive = $state(false);
  let altActive = $state(false);
  let btnTouch = $derived(platform.isHandheld ? 'h-9 w-9 justify-center' : 'px-2 py-1');
  let sessionStartMs = 0;

  function toggleDockPosition() {
    dockTop = !dockTop;
    if (typeof window !== 'undefined') {
      localStorage.setItem('opentermius_keybar_dock', dockTop ? 'top' : 'bottom');
    }
  }

  function recordSessionLog() {
    if (typeof window === 'undefined') return;
    sessionStartMs = Date.now();
    try {
      const raw = localStorage.getItem('opentermius_session_logs');
      const list = raw ? JSON.parse(raw) : [];
      const entry = {
        id: activeSessionId,
        hostLabel: title || config?.address || 'Local Shell',
        target: config?.address ? `${config.username || 'root'}@${config.address}:${config.port || 22}` : 'local-pty',
        mode: config?.address ? 'Native SSH' : 'Local PTY',
        startedAt: new Date().toISOString(),
        durationSec: 0
      };
      const filtered = list.filter((item: { id: string }) => item.id !== activeSessionId);
      localStorage.setItem('opentermius_session_logs', JSON.stringify([entry, ...filtered].slice(0, 100)));
    } catch {}
  }

  function finalizeSessionLog() {
    if (typeof window === 'undefined' || !sessionStartMs) return;
    const elapsedSec = Math.max(1, Math.round((Date.now() - sessionStartMs) / 1000));
    try {
      const raw = localStorage.getItem('opentermius_session_logs');
      if (!raw) return;
      const list = JSON.parse(raw);
      const updated = list.map((item: { id: string; durationSec?: number }) =>
        item.id === activeSessionId ? { ...item, durationSec: elapsedSec } : item
      );
      localStorage.setItem('opentermius_session_logs', JSON.stringify(updated));
    } catch {}
  }

  function sendTerminalInput(data: string) {
    import('@tauri-apps/api/core').then(({ invoke }) => {
      invoke('terminal_write', { sessionId: activeSessionId, data }).catch(() => {});
    });
  }

  function applyModifiers(input: string): string {
    let out = input;
    if (ctrlActive && out.length === 1) {
      const code = out.toUpperCase().charCodeAt(0);
      if (code >= 64 && code <= 95) {
        out = String.fromCharCode(code - 64);
      }
      ctrlActive = false;
    }
    if (altActive) {
      out = `\x1b${out}`;
      altActive = false;
    }
    return out;
  }

  function handleVirtualKey(key: string) {
    if (key === 'MOD_CTRL') {
      ctrlActive = !ctrlActive;
      term?.focus();
      return;
    }
    if (key === 'MOD_ALT') {
      altActive = !altActive;
      term?.focus();
      return;
    }

    let toSend = key;
    if (key === 'ESC') toSend = '\x1b';
    else if (key === 'TAB') toSend = '\t';
    else if (key === 'UP') toSend = '\x1b[A';
    else if (key === 'DOWN') toSend = '\x1b[B';
    else if (key === 'RIGHT') toSend = '\x1b[C';
    else if (key === 'LEFT') toSend = '\x1b[D';
    else if (key === 'HOME') toSend = '\x1b[H';
    else if (key === 'END') toSend = '\x1b[F';
    else if (key === 'PGUP') toSend = '\x1b[5~';
    else if (key === 'PGDN') toSend = '\x1b[6~';
    else if (key === 'CTRL_C') toSend = '\x03';
    else if (key === 'CTRL_D') toSend = '\x04';
    else if (key === 'CTRL_Z') toSend = '\x1a';
    else if (key === 'CTRL_L') toSend = '\x0c';
    else toSend = applyModifiers(toSend);

    sendTerminalInput(toSend);
    term?.focus();
  }

  // Auto-fit and focus whenever tab becomes active or viewport/bar changes
  $effect(() => {
    const _vh = platform.visualHeight;
    const _h = platform.height;
    const _bar = showAccessoryBar;
    const _dock = dockTop;
    if (active && fitAddon && term) {
      setTimeout(() => {
        try {
          fitAddon?.fit();
        } catch {}
      }, 40);
    }
  });

  async function connectNative() {
    connectionState = 'connecting';
    errorMessage = null;

    if (config?.address) {
      term?.writeln(`\x1b[1;36mConnecting to ${config.username ? config.username + '@' : ''}${config.address}:${config.port || 22}...\x1b[0m\r\n`);
    }

    try {
      const { invoke } = await import('@tauri-apps/api/core');
      const { listen } = await import('@tauri-apps/api/event');

      unlistenData?.();
      unlistenExit?.();

      unlistenData = await listen<string>(`terminal-data:${activeSessionId}`, (event) => {
        term?.write(event.payload);
      });

      unlistenExit = await listen(`terminal-exit:${activeSessionId}`, () => {
        connectionState = 'disconnected';
        term?.writeln('\r\n\x1b[1;31mSession disconnected.\x1b[0m\r\n');
      });

      const cols = term?.cols && term.cols > 0 ? term.cols : 80;
      const rows = term?.rows && term.rows > 0 ? term.rows : 24;

      await invoke('terminal_open', {
        config: {
          session_id: activeSessionId,
          host: config?.address,
          port: config?.port || 22,
          username: config?.username,
          password: config?.password,
          private_key: config?.privateKey,
          passphrase: config?.passphrase,
          cols,
          rows
        }
      });

      connectionState = 'connected';
      recordSessionLog();
    } catch (err: unknown) {
      connectionState = 'error';
      errorMessage = formatErrorMessage(err, 'Failed to start terminal session');
      term?.writeln(`\r\n\x1b[1;31m● Connection error: ${errorMessage}\x1b[0m\r\n`);
    }
  }

  onMount(() => {
    if (!terminalContainer) return;

    term = new Terminal({
      cursorBlink: true,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      fontSize: platform.isMobile ? 12 : 14,
      lineHeight: 1.25,
      theme: {
        background: '#0a0a0c',
        foreground: '#e2e8f0',
        cursor: '#10b981',
        selectionBackground: '#10b98140',
        black: '#1a1a24',
        red: '#f43f5e',
        green: '#10b981',
        yellow: '#f59e0b',
        blue: '#3b82f6',
        magenta: '#d946ef',
        cyan: '#06b6d4',
        white: '#f8fafc'
      }
    });

    fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.open(terminalContainer);

    setTimeout(() => {
      try {
        fitAddon?.fit();
      } catch {}
    }, 20);

    term.onData((data) => {
      sendTerminalInput(applyModifiers(data));
    });

    term.onResize(({ cols, rows }) => {
      import('@tauri-apps/api/core').then(({ invoke }) => {
        invoke('terminal_resize', { sessionId: activeSessionId, cols, rows }).catch(() => {});
      });
    });

    // Auto-fit on window resize
    resizeObserver = new ResizeObserver(() => {
      try {
        fitAddon?.fit();
      } catch {}
    });
    resizeObserver.observe(terminalContainer);

    const handleSnippetEvent = (e: Event) => {
      if (!active || isSplitPane) return;
      const script = (e as CustomEvent<{ script: string }>).detail?.script;
      if (script) {
        sendTerminalInput(script.endsWith('\n') ? script : `${script}\n`);
        term?.focus();
      }
    };
    window.addEventListener('opentermius:run-snippet', handleSnippetEvent);

    connectNative();

    return () => {
      window.removeEventListener('opentermius:run-snippet', handleSnippetEvent);
    };
  });

  onDestroy(() => {
    finalizeSessionLog();
    import('@tauri-apps/api/core').then(({ invoke }) => {
      invoke('terminal_close', { sessionId: activeSessionId }).catch(() => {});
    });
    unlistenData?.();
    unlistenExit?.();
    resizeObserver?.disconnect();
    term?.dispose();
  });

  function handleClear() {
    term?.clear();
  }

  function handleReconnect() {
    import('@tauri-apps/api/core')
      .then(({ invoke }) => invoke('terminal_close', { sessionId: activeSessionId }))
      .catch(() => {})
      .finally(() => {
        connectNative();
      });
  }
</script>

<div class="flex h-full min-h-0 flex-col bg-[#0a0a0c] overflow-hidden">
  <!-- Top Session Toolbar -->
  <div class="flex h-10 shrink-0 items-center justify-between border-b border-[#1a1a24] bg-[#0d0d12] px-3 sm:px-4">
    <div class="flex items-center gap-3">
      <div class="flex items-center gap-2">
        <span class="relative flex h-2 w-2">
          {#if connectionState === 'connected'}
            <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
            <span class="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
          {:else if connectionState === 'connecting'}
            <span class="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75"></span>
            <span class="relative inline-flex rounded-full h-2 w-2 bg-amber-500"></span>
          {:else}
            <span class="relative inline-flex rounded-full h-2 w-2 bg-rose-500"></span>
          {/if}
        </span>
        <span class="text-xs font-semibold text-white tracking-wide">{title}</span>
      </div>
    </div>

    <div class="flex items-center gap-1.5">
      {#if onSplitRight && !platform.isMobile}
        <button
          onclick={onSplitRight}
          class="flex items-center justify-center h-6 w-6 rounded bg-[#181822] text-neutral-400 hover:text-white transition"
          title="Split Right (Columns)"
          aria-label="Split Right"
        >
          <Columns2 class="h-3.5 w-3.5" />
        </button>
      {/if}

      {#if onSplitDown && !platform.isMobile}
        <button
          onclick={onSplitDown}
          class="flex items-center justify-center h-6 w-6 rounded bg-[#181822] text-neutral-400 hover:text-white transition"
          title="Split Down (Rows)"
          aria-label="Split Down"
        >
          <Rows2 class="h-3.5 w-3.5" />
        </button>
      {/if}

      <button
        onclick={() => (showAccessoryBar = !showAccessoryBar)}
        class="flex items-center gap-1 rounded {btnTouch} text-xs transition {showAccessoryBar ? 'bg-emerald-500/15 text-emerald-400 border border-emerald-500/30' : 'bg-[#181822] text-neutral-400 hover:text-white'}"
        title="Toggle virtual touch keys"
        aria-label="Toggle virtual touch keys"
      >
        <Keyboard class="h-3.5 w-3.5" />
      </button>

      <button
        onclick={handleReconnect}
        class="flex items-center gap-1 rounded bg-[#181822] {btnTouch} text-xs text-neutral-300 hover:text-white transition"
        title="Reconnect"
        aria-label="Reconnect"
      >
        <RefreshCw class="h-3.5 w-3.5" />
      </button>

      <button
        onclick={handleClear}
        class="flex items-center gap-1 rounded bg-[#181822] {btnTouch} text-xs text-neutral-300 hover:text-white transition"
        title="Clear terminal"
        aria-label="Clear terminal"
      >
        <Trash2 class="h-3.5 w-3.5" />
      </button>

      <button
        onclick={onShareMultiplayer}
        class="flex items-center gap-1 rounded bg-emerald-600/20 border border-emerald-500/30 {btnTouch} text-xs font-semibold text-emerald-400 hover:bg-emerald-600/30 transition"
        title="Share session via multiplayer link"
        aria-label="Share session via multiplayer link"
      >
        <Share2 class="h-3.5 w-3.5" />
      </button>

      {#if isSplitPane && onClosePane}
        <button
          onclick={onClosePane}
          class="flex items-center justify-center h-6 w-6 rounded bg-rose-500/20 text-rose-400 hover:bg-rose-500/30 transition ml-1"
          title="Close split pane"
          aria-label="Close split pane"
        >
          <X class="h-3.5 w-3.5" />
        </button>
      {/if}
    </div>
  </div>

  {#snippet accessoryBarSnippet()}
    {#if platform.isHandheld}
      <!-- 2-Row Ergonomic Touch Special Keys Bar for Mobile & Tablet -->
      <div class="shrink-0 bg-[#101018] {dockTop ? 'border-b' : 'border-t pb-1.5'} border-[#222232] px-2 pt-1.5 pb-1 space-y-1.5 select-none z-20">
        <!-- Row 1: Modifiers (ESC, TAB, Sticky CTRL/ALT, Shortcuts, Symbols) -->
        <div class="flex items-center gap-1.5 overflow-x-auto no-scrollbar">
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('ESC'); }}
            class="h-9 min-w-[44px] px-2.5 rounded-lg bg-[#1a1a28] border border-[#2a2a3d] text-xs font-mono font-semibold text-neutral-200 active:bg-emerald-600 active:text-white transition shrink-0"
          >
            ESC
          </button>
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('TAB'); }}
            class="h-9 min-w-[44px] px-2.5 rounded-lg bg-[#1a1a28] border border-[#2a2a3d] text-xs font-mono font-semibold text-neutral-200 active:bg-emerald-600 active:text-white transition shrink-0"
          >
            TAB
          </button>
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('MOD_CTRL'); }}
            class="h-9 min-w-[48px] px-2.5 rounded-lg border text-xs font-mono font-bold transition shrink-0 {ctrlActive ? 'bg-emerald-500 text-black border-emerald-400 shadow-sm shadow-emerald-500/30' : 'bg-[#1a1a28] border-[#2a2a3d] text-emerald-400'}"
          >
            CTRL
          </button>
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('MOD_ALT'); }}
            class="h-9 min-w-[44px] px-2.5 rounded-lg border text-xs font-mono font-bold transition shrink-0 {altActive ? 'bg-cyan-500 text-black border-cyan-400 shadow-sm shadow-cyan-500/30' : 'bg-[#1a1a28] border-[#2a2a3d] text-cyan-400'}"
          >
            ALT
          </button>
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('CTRL_C'); }}
            class="h-9 min-w-[40px] px-2.5 rounded-lg bg-[#1a1a28] border border-rose-500/30 text-xs font-mono font-semibold text-rose-300 active:bg-rose-600 active:text-white transition shrink-0"
          >
            ^C
          </button>
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('CTRL_D'); }}
            class="h-9 min-w-[40px] px-2.5 rounded-lg bg-[#1a1a28] border border-[#2a2a3d] text-xs font-mono text-neutral-300 active:bg-emerald-600 active:text-white transition shrink-0"
          >
            ^D
          </button>
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('CTRL_Z'); }}
            class="h-9 min-w-[40px] px-2.5 rounded-lg bg-[#1a1a28] border border-[#2a2a3d] text-xs font-mono text-neutral-300 active:bg-emerald-600 active:text-white transition shrink-0"
          >
            ^Z
          </button>
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('CTRL_L'); }}
            class="h-9 min-w-[40px] px-2.5 rounded-lg bg-[#1a1a28] border border-[#2a2a3d] text-xs font-mono text-neutral-300 active:bg-emerald-600 active:text-white transition shrink-0"
          >
            ^L
          </button>
          {#each ['/', '-', '|', '~', ':'] as sym}
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey(sym); }}
              class="h-9 min-w-[38px] px-2.5 rounded-lg bg-[#1a1a28] border border-[#2a2a3d] text-xs font-mono text-neutral-200 active:bg-emerald-600 active:text-white transition shrink-0"
            >
              {sym}
            </button>
          {/each}
        </div>

        <!-- Row 2: Pinned Navigation & Dedicated Arrow Keys Cluster (Always visible without scrolling) -->
        <div class="flex items-center justify-between gap-1.5">
          <div class="flex items-center gap-1 overflow-x-auto no-scrollbar">
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); toggleDockPosition(); }}
              class="h-9 px-2 rounded-lg bg-[#161622] border border-[#28283a] text-[10px] font-semibold text-neutral-400 active:text-white shrink-0"
              title="Move special keys bar to Top or Bottom"
            >
              {dockTop ? '↓ Bottom' : '↑ Top'}
            </button>
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('HOME'); }}
              class="h-9 px-2 rounded-lg bg-[#181824] border border-[#252538] text-[10px] font-mono text-neutral-300 active:bg-emerald-600 active:text-white shrink-0"
            >
              HOME
            </button>
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('END'); }}
              class="h-9 px-2 rounded-lg bg-[#181824] border border-[#252538] text-[10px] font-mono text-neutral-300 active:bg-emerald-600 active:text-white shrink-0"
            >
              END
            </button>
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('PGUP'); }}
              class="h-9 px-2 rounded-lg bg-[#181824] border border-[#252538] text-[10px] font-mono text-neutral-300 active:bg-emerald-600 active:text-white shrink-0"
            >
              PGUP
            </button>
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('PGDN'); }}
              class="h-9 px-2 rounded-lg bg-[#181824] border border-[#252538] text-[10px] font-mono text-neutral-300 active:bg-emerald-600 active:text-white shrink-0"
            >
              PGDN
            </button>
          </div>

          <!-- Pinned Arrow Keys Cluster -->
          <div class="flex items-center gap-1.5 shrink-0">
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('LEFT'); }}
              class="h-9 w-11 rounded-lg bg-[#1e1e30] border border-emerald-500/30 text-sm font-mono font-bold text-white active:bg-emerald-500 active:text-black transition flex items-center justify-center"
              aria-label="Left Arrow"
            >
              ←
            </button>
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('DOWN'); }}
              class="h-9 w-11 rounded-lg bg-[#1e1e30] border border-emerald-500/30 text-sm font-mono font-bold text-white active:bg-emerald-500 active:text-black transition flex items-center justify-center"
              aria-label="Down Arrow"
            >
              ↓
            </button>
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('UP'); }}
              class="h-9 w-11 rounded-lg bg-[#1e1e30] border border-emerald-500/30 text-sm font-mono font-bold text-white active:bg-emerald-500 active:text-black transition flex items-center justify-center"
              aria-label="Up Arrow"
            >
              ↑
            </button>
            <button
              type="button"
              onpointerdown={(e) => { e.preventDefault(); handleVirtualKey('RIGHT'); }}
              class="h-9 w-11 rounded-lg bg-[#1e1e30] border border-emerald-500/30 text-sm font-mono font-bold text-white active:bg-emerald-500 active:text-black transition flex items-center justify-center"
              aria-label="Right Arrow"
            >
              →
            </button>
          </div>
        </div>
      </div>
    {:else}
      <!-- Desktop 1-Row Compact Virtual Key Bar -->
      <div class="flex items-center gap-1.5 overflow-x-auto no-scrollbar py-1.5 px-2 bg-[#0e0e14] {dockTop ? 'border-b' : 'border-t'} border-[#1f1f2a] shrink-0 select-none">
        {#each [
          { k: 'ESC', l: 'ESC' },
          { k: 'TAB', l: 'TAB' },
          { k: 'CTRL_C', l: '^C', danger: true },
          { k: 'CTRL_D', l: '^D' },
          { k: 'CTRL_Z', l: '^Z' },
          { k: 'UP', l: '↑' },
          { k: 'DOWN', l: '↓' },
          { k: 'LEFT', l: '←' },
          { k: 'RIGHT', l: '→' },
          { k: 'HOME', l: 'HOME' },
          { k: 'END', l: 'END' },
          { k: '/', l: '/' },
          { k: '|', l: '|' },
          { k: '~', l: '~' },
          { k: '-', l: '-' },
          { k: ':', l: ':' }
        ] as btn}
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); handleVirtualKey(btn.k); }}
            class="h-7 min-w-[32px] px-2.5 rounded bg-[#181824] border border-[#252538] text-[11px] font-mono {btn.danger ? 'text-rose-300 hover:text-rose-200 active:bg-rose-600' : 'text-neutral-300 hover:text-white active:bg-emerald-600'} active:text-white transition shrink-0"
          >
            {btn.l}
          </button>
        {/each}
      </div>
    {/if}
  {/snippet}

  {#if showAccessoryBar && dockTop}
    {@render accessoryBarSnippet()}
  {/if}

  <!-- Terminal Connection States & xterm Mount Area -->
  <div class="relative flex-1 min-h-0 overflow-hidden p-1.5 sm:p-2">
    {#if connectionState === 'connecting'}
      <div class="absolute inset-0 z-10 flex flex-col items-center justify-center bg-[#0a0a0c]/80 backdrop-blur-xs gap-3">
        <RefreshCw class="h-6 w-6 animate-spin text-emerald-400" />
        <p class="text-xs font-medium text-neutral-300">
          {config?.address ? 'Initiating native SSH connection...' : 'Opening local terminal shell...'}
        </p>
      </div>
    {:else if connectionState === 'error'}
      <div class="absolute inset-0 z-10 flex flex-col items-center justify-center bg-[#0a0a0c]/90 p-6">
        <div class="max-w-md rounded-2xl border border-rose-500/30 bg-rose-950/20 p-5 text-center space-y-3">
          <AlertCircle class="h-8 w-8 text-rose-400 mx-auto" />
          <h4 class="text-sm font-bold text-white">Connection Error</h4>
          <p class="text-xs text-rose-300">{errorMessage}</p>
          <button
            onclick={handleReconnect}
            class="rounded-lg bg-rose-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 transition"
          >
            Retry Connection
          </button>
        </div>
      </div>
    {/if}

    <div bind:this={terminalContainer} class="h-full w-full"></div>
  </div>

  {#if showAccessoryBar && !dockTop}
    {@render accessoryBarSnippet()}
  {/if}
</div>
