<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import { terminalStore, type TerminalHostConfig, type RemoteViewerConfig } from '../stores/terminal.svelte';
  import { api, type RelayParticipant } from '../api/client';
  import { platform } from '../stores/platform.svelte';
  import { toast, formatErrorMessage } from '../stores/toast.svelte';
  import {
    RefreshCw,
    Trash2,
    AlertCircle,
    Columns2,
    Rows2,
    X,
    Keyboard,
    Copy,
    ClipboardPaste,
    MoreVertical,
    Maximize2,
    Minimize2
  } from '@lucide/svelte';

  interface Props {
    title: string;
    sessionId?: string;
    hostId: string;
    config?: TerminalHostConfig;
    remoteConfig?: RemoteViewerConfig;
    active?: boolean;
    isSplitPane?: boolean;
    isFocusMode?: boolean;
    onToggleFocus?: () => void;
    onSplitRight?: () => void;
    onSplitDown?: () => void;
    onClosePane?: () => void;
  }

  let {
    title = 'Active Terminal Session',
    sessionId = '',
    hostId,
    config,
    remoteConfig,
    active = true,
    isSplitPane = false,
    isFocusMode = false,
    onToggleFocus,
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
  let isMoreMenuOpen = $state(false);
  let dockTop = $state<boolean>(
    typeof window !== 'undefined' && localStorage.getItem('opentermius_keybar_dock') === 'top'
  );
  let ctrlActive = $state(false);
  let altActive = $state(false);
  let fontSize = $state<number>(
    typeof window !== 'undefined'
      ? Number(localStorage.getItem('opentermius_term_fontsize')) || (platform.isMobile ? 12 : 14)
      : 13
  );
  let btnTouch = $derived(platform.isHandheld ? 'h-9 w-9 justify-center' : 'px-2 py-1');
  let hasSelection = $state(false);
  let showTouchMenu = $state(false);
  let selectionMode = $state(false);
  let selStartCol = $state(0);
  let selStartRow = $state(0);
  let selEndCol = $state(0);
  let selEndRow = $state(0);
  let startHandlePos = $state<{ x: number; y: number } | null>(null);
  let endHandlePos = $state<{ x: number; y: number } | null>(null);
  let activeHandle = $state<'start' | 'end' | 'new' | null>(null);
  let repeatDelayTimer: ReturnType<typeof setTimeout> | null = null;
  let repeatIntervalTimer: ReturnType<typeof setInterval> | null = null;
  let relayWs: WebSocket | null = null;
  let isRelaySharing = $state(false);
  let relayViewerCount = $state(0);
  let relayParticipants = $state<RelayParticipant[]>([]);
  let typingUsers = $state<Array<{ user_id: string; user_name: string }>>([]);
  const typingTimers = new Map<string, ReturnType<typeof setTimeout>>();
  let lastHostTypingSent = 0;
  let relayEndedByHost = false;
  const relayEncoder = new TextEncoder();
  const relayDecoder = new TextDecoder();
  let sessionStartMs = 0;

  function safeFit() {
    if (!active || !terminalContainer || !fitAddon || !term) return;
    const rect = terminalContainer.getBoundingClientRect();
    if (rect.width < 40 || rect.height < 40) return;
    try {
      fitAddon.fit();
      term.refresh(0, term.rows - 1);
    } catch {}
  }

  $effect(() => {
    if (active && term && fitAddon) {
      requestAnimationFrame(() => {
        safeFit();
        if (!platform.isMobile) term?.focus();
      });
    }
  });

  function getInitials(name: string): string {
    const parts = (name || '?').trim().split(/\s+/);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return (parts[0]?.slice(0, 2) || '?').toUpperCase();
  }

  function getAvatarTone(seed: string): string {
    const tones = ['bg-emerald-600', 'bg-indigo-600', 'bg-cyan-600', 'bg-amber-600', 'bg-rose-600', 'bg-violet-600'];
    let h = 0;
    for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) | 0;
    return tones[Math.abs(h) % tones.length];
  }

  function handleRelayControlMessage(raw: string): boolean {
    try {
      const msg = JSON.parse(raw);
      if (msg.type === 'relay_state' || msg.type === 'viewer_count') {
        relayViewerCount = Number(msg.count) || 0;
        if (Array.isArray(msg.viewers)) {
          relayParticipants = msg.viewers;
        }
        return true;
      }
      if (msg.type === 'typing' && msg.user_id && msg.user_name) {
        const uid = String(msg.user_id);
        const uname = String(msg.user_name);
        if (!typingUsers.some((u) => u.user_id === uid)) {
          typingUsers = [...typingUsers, { user_id: uid, user_name: uname }];
        }
        const prev = typingTimers.get(uid);
        if (prev) clearTimeout(prev);
        typingTimers.set(
          uid,
          setTimeout(() => {
            typingTimers.delete(uid);
            typingUsers = typingUsers.filter((u) => u.user_id !== uid);
          }, 1800)
        );
        return true;
      }
      if (msg.type === 'relay_ended') {
        relayEndedByHost = true;
        toast.info(msg.reason || 'Remote sharing session ended');
        try { relayWs?.close(); } catch {}
        relayWs = null;
        terminalStore.closeTab(activeSessionId);
        return true;
      }
    } catch {}
    return false;
  }

  function stopRelay() {
    for (const timer of typingTimers.values()) {
      clearTimeout(timer);
    }
    typingTimers.clear();
    if (relayWs) {
      try { relayWs.close(); } catch {}
      relayWs = null;
    }
    isRelaySharing = false;
    relayViewerCount = 0;
    relayParticipants = [];
    typingUsers = [];
  }

  function startHostRelay() {
    if (remoteConfig || (relayWs && relayWs.readyState <= WebSocket.OPEN)) return;
    const url = api.getRelayWsUrl(activeSessionId, 'host');
    const ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => {
      isRelaySharing = true;
      toast.success('Remote relay active');
    };
    ws.onmessage = (ev) => {
      if (typeof ev.data === 'string') {
        handleRelayControlMessage(ev.data);
        return;
      }
      if (ev.data instanceof ArrayBuffer) {
        const text = relayDecoder.decode(new Uint8Array(ev.data));
        if (text) {
          import('@tauri-apps/api/core').then(({ invoke }) => {
            invoke('terminal_write', { sessionId: activeSessionId, data: text }).catch(() => {});
          });
        }
      }
    };
    ws.onclose = () => {
      isRelaySharing = false;
      relayViewerCount = 0;
      relayParticipants = [];
      typingUsers = [];
      relayWs = null;
    };
    relayWs = ws;
  }

  function connectRemoteViewer() {
    if (!remoteConfig) return;
    connectionState = 'connecting';
    errorMessage = null;
    term?.writeln(`\x1b[1;36mJoining remote session (${remoteConfig.mode === 'control' ? 'Read-Write' : 'View-Only'})...\x1b[0m\r\n`);

    const url = api.getRelayWsUrl(remoteConfig.sessionId, 'viewer', remoteConfig.requestId);
    const ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';
    ws.onopen = () => {
      connectionState = 'connected';
      setTimeout(() => term?.focus(), 50);
    };
    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) {
        term?.write(relayDecoder.decode(new Uint8Array(ev.data)));
      } else if (typeof ev.data === 'string') {
        if (!handleRelayControlMessage(ev.data)) {
          term?.write(ev.data);
        }
      }
    };
    ws.onerror = () => {
      connectionState = 'error';
      errorMessage = 'Relay WebSocket connection failed';
    };
    ws.onclose = () => {
      relayParticipants = [];
      typingUsers = [];
      if (remoteConfig && !relayEndedByHost && connectionState !== 'error') {
        toast.info('Remote sharing session ended');
        terminalStore.closeTab(activeSessionId);
        return;
      }
      if (connectionState !== 'error') {
        connectionState = 'disconnected';
        term?.writeln('\r\n\x1b[1;33mRemote relay closed.\x1b[0m\r\n');
      }
    };
    relayWs = ws;
  }

  function stopKeyRepeat() {
    if (repeatDelayTimer) {
      clearTimeout(repeatDelayTimer);
      repeatDelayTimer = null;
    }
    if (repeatIntervalTimer) {
      clearInterval(repeatIntervalTimer);
      repeatIntervalTimer = null;
    }
  }

  function startKeyRepeat(key: string, e: PointerEvent) {
    e.preventDefault();
    stopKeyRepeat();
    handleVirtualKey(key);
    repeatDelayTimer = setTimeout(() => {
      repeatIntervalTimer = setInterval(() => handleVirtualKey(key), 55);
    }, 300);
  }

  function pointToTerminalCell(clientX: number, clientY: number) {
    const screenEl = terminalContainer?.querySelector('.xterm-screen') as HTMLElement | null;
    const rect = (screenEl ?? terminalContainer!).getBoundingClientRect();
    const cols = Math.max(1, term?.cols || 80);
    const rows = Math.max(1, term?.rows || 24);
    const col = Math.max(0, Math.min(cols - 1, Math.floor(((clientX - rect.left) / rect.width) * cols)));
    const viewRow = Math.max(0, Math.min(rows - 1, Math.floor(((clientY - rect.top) / rect.height) * rows)));
    const bufRow = (term?.buffer.active.viewportY ?? 0) + viewRow;
    return { col, bufRow, cols };
  }

  function updateHandleCoords() {
    if (!term || !terminalContainer || !selectionMode || !hasSelection) {
      startHandlePos = null;
      endHandlePos = null;
      return;
    }
    const wrapper = terminalContainer.parentElement;
    const screenEl = terminalContainer.querySelector('.xterm-screen') as HTMLElement | null;
    if (!wrapper || !screenEl) return;
    const wRect = wrapper.getBoundingClientRect();
    const sRect = screenEl.getBoundingClientRect();
    const cols = Math.max(1, term.cols || 80);
    const rows = Math.max(1, term.rows || 24);
    const cellW = sRect.width / cols;
    const cellH = sRect.height / rows;
    const vpY = term.buffer.active.viewportY ?? 0;
    const offX = sRect.left - wRect.left;
    const offY = sRect.top - wRect.top;

    startHandlePos = {
      x: Math.max(8, Math.min(wRect.width - 8, offX + selStartCol * cellW)),
      y: Math.max(12, Math.min(wRect.height - 8, offY + (selStartRow - vpY + 1) * cellH))
    };
    endHandlePos = {
      x: Math.max(8, Math.min(wRect.width - 8, offX + (selEndCol + 1) * cellW)),
      y: Math.max(12, Math.min(wRect.height - 8, offY + (selEndRow - vpY + 1) * cellH))
    };
  }

  function selectWordAt(col: number, bufRow: number) {
    if (!term) return;
    const lineStr = term.buffer.active.getLine(bufRow)?.translateToString(false) ?? '';
    const isWord = (ch: string) => /[A-Za-z0-9_.\-/:@~]/.test(ch);
    let c1 = col;
    let c2 = col;
    if (col < lineStr.length && isWord(lineStr[col])) {
      while (c1 > 0 && isWord(lineStr[c1 - 1])) c1--;
      while (c2 + 1 < lineStr.length && isWord(lineStr[c2 + 1])) c2++;
    }
    selStartCol = c1;
    selStartRow = bufRow;
    selEndCol = c2;
    selEndRow = bufRow;
    term.select(c1, bufRow, c2 - c1 + 1);
    hasSelection = true;
    updateHandleCoords();
  }

  function applyOrderedSelection(c1: number, r1: number, c2: number, r2: number, cols: number) {
    const idx1 = r1 * cols + c1;
    const idx2 = r2 * cols + c2;
    const minIdx = Math.min(idx1, idx2);
    const maxIdx = Math.max(idx1, idx2);
    const sRow = Math.floor(minIdx / cols);
    const sCol = minIdx % cols;
    const eRow = Math.floor(maxIdx / cols);
    const eCol = maxIdx % cols;
    selStartCol = sCol;
    selStartRow = sRow;
    selEndCol = eCol;
    selEndRow = eRow;
    term?.select(sCol, sRow, maxIdx - minIdx + 1);
    hasSelection = true;
    updateHandleCoords();
  }

  function exitSelectionMode() {
    term?.clearSelection();
    hasSelection = false;
    showTouchMenu = false;
    selectionMode = false;
    activeHandle = null;
    startHandlePos = null;
    endHandlePos = null;
  }

  function handleOverlayPointerDown(e: PointerEvent) {
    e.preventDefault();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    const cell = pointToTerminalCell(e.clientX, e.clientY);
    activeHandle = 'new';
    selStartCol = cell.col;
    selStartRow = cell.bufRow;
    selEndCol = cell.col;
    selEndRow = cell.bufRow;
    term?.select(cell.col, cell.bufRow, 1);
    hasSelection = true;
    updateHandleCoords();
  }

  function handleHandlePointerDown(which: 'start' | 'end', e: PointerEvent) {
    e.preventDefault();
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    activeHandle = which;
  }

  function handleOverlayPointerMove(e: PointerEvent) {
    if (!activeHandle) return;
    e.preventDefault();
    // Offset slightly upward so finger doesn't cover the target row
    const cell = pointToTerminalCell(e.clientX, e.clientY - (activeHandle === 'new' ? 0 : 18));
    if (activeHandle === 'start') {
      applyOrderedSelection(cell.col, cell.bufRow, selEndCol, selEndRow, cell.cols);
    } else if (activeHandle === 'end') {
      applyOrderedSelection(selStartCol, selStartRow, cell.col, cell.bufRow, cell.cols);
    } else {
      applyOrderedSelection(selStartCol, selStartRow, cell.col, cell.bufRow, cell.cols);
    }
  }

  function handleOverlayPointerUp(e: PointerEvent) {
    if (!activeHandle) return;
    e.preventDefault();
    activeHandle = null;
  }

  async function handleCopySelection() {
    const sel = term?.getSelection() ?? '';
    if (!sel) return;
    try {
      const bridge = (window as any).AndroidClipboard;
      if (bridge?.writeText) {
        bridge.writeText(sel);
      } else {
        await navigator.clipboard.writeText(sel);
      }
      toast.success('Copied');
    } catch {
      toast.error('Clipboard permission denied');
    }
    exitSelectionMode();
  }

  async function handlePasteClipboard() {
    try {
      const bridge = (window as any).AndroidClipboard;
      const text = bridge?.readText ? String(bridge.readText()) : await navigator.clipboard.readText();
      if (!text) {
        toast.info('Clipboard is empty');
        return;
      }
      sendTerminalInput(text);
    } catch {
      toast.error('Clipboard read blocked by system');
    }
    exitSelectionMode();
    term?.focus();
  }

  function changeFontSize(delta: number) {
    const next = Math.min(20, Math.max(9, fontSize + delta));
    fontSize = next;
    if (typeof window !== 'undefined') {
      localStorage.setItem('opentermius_term_fontsize', String(next));
    }
    if (term) {
      term.options.fontSize = next;
      try { fitAddon?.fit(); } catch {}
    }
  }

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
    if (remoteConfig) {
      if (remoteConfig.mode === 'control' && relayWs?.readyState === WebSocket.OPEN) {
        relayWs.send(relayEncoder.encode(data));
      }
      return;
    }
    if (isRelaySharing && relayWs?.readyState === WebSocket.OPEN) {
      const now = Date.now();
      if (now - lastHostTypingSent > 700) {
        lastHostTypingSent = now;
        relayWs.send(JSON.stringify({ type: 'typing' }));
      }
    }
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

  let connSeq = 0;

  async function connectNative() {
    const mySeq = ++connSeq;
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
        if (mySeq !== connSeq) return;
        term?.write(event.payload);
        if (relayWs && relayWs.readyState === WebSocket.OPEN && isRelaySharing) {
          relayWs.send(relayEncoder.encode(event.payload));
        }
      });

      unlistenExit = await listen(`terminal-exit:${activeSessionId}`, () => {
        if (mySeq !== connSeq) return;
        connectionState = 'disconnected';
        term?.writeln('\r\n\x1b[1;31mSession disconnected.\x1b[0m\r\n');
        invoke('terminal_close', { sessionId: activeSessionId }).catch(() => {});
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
      const rawMsg = err instanceof Error ? err.message : typeof err === 'string' ? err : '';
      const prompt = toast.confirmHostKey(rawMsg);
      if (prompt) {
        const trusted = await prompt;
        if (trusted) {
          await handleTrustHostKey();
          return;
        }
        connectionState = 'error';
        errorMessage = 'Connection cancelled: host key was not trusted.';
        term?.writeln('\r\n\x1b[1;33mHost key verification cancelled.\x1b[0m\r\n');
        return;
      }
      connectionState = 'error';
      errorMessage = formatErrorMessage(err, 'Failed to start terminal session');
      term?.writeln(`\r\n\x1b[1;31m● Connection error: ${errorMessage}\x1b[0m\r\n`);
    }
  }

  async function handleTrustHostKey() {
    if (!config?.address) return;
    try {
      const { invoke } = await import('@tauri-apps/api/core');
      await invoke('ssh_trust_host_key', {
        host: config.address,
        port: config.port ?? 22
      });
      toast.success('Updated known host key');
      await handleReconnect();
    } catch (err: unknown) {
      toast.error(err, 'Failed to reset host key');
    }
  }

  onMount(() => {
    if (!terminalContainer) return;

    term = new Terminal({
      cursorBlink: true,
      scrollback: platform.isMobile ? 1500 : 5000,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      fontSize,
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

    if (term.textarea) {
      term.textarea.setAttribute('autocomplete', 'off');
      term.textarea.setAttribute('autocorrect', 'off');
      term.textarea.setAttribute('autocapitalize', 'none');
      term.textarea.setAttribute('spellcheck', 'false');
      term.textarea.setAttribute('data-gramm', 'false');
      term.textarea.setAttribute('enterkeyhint', 'send');
    }

    setTimeout(() => {
      safeFit();
    }, 20);

    term.onData((data) => {
      sendTerminalInput(applyModifiers(data));
    });

    // Let app-level Termius shortcuts bubble to window instead of being swallowed by xterm
    term.attachCustomKeyEventHandler((e: KeyboardEvent) => {
      const k = e.key.toLowerCase();
      if (e.ctrlKey && k === 'tab') return false;
      if (platform.isMac && e.metaKey) {
        if (/^[1-90=\-+,/\[\]]$/.test(k) || ['t', 'k', 'j', 'w', 'd', 'b'].includes(k)) return false;
        if (e.altKey && (e.key === 'ArrowLeft' || e.key === 'ArrowRight')) return false;
        if (e.shiftKey && ['l', 'k', 'd'].includes(k)) return false;
      } else if (!platform.isMac && e.ctrlKey) {
        if (/^[1-90=\-+,/]$/.test(k) || k === 'j') return false;
        if (e.shiftKey && ['t', 'k', 'w', 'd', 'b', 'l', '[', ']'].includes(k)) return false;
        if (e.altKey && k === 'd') return false;
      }
      return true;
    });

    term.onResize(({ cols, rows }) => {
      if (remoteConfig || cols < 10 || rows < 3) return;
      import('@tauri-apps/api/core').then(({ invoke }) => {
        invoke('terminal_resize', { sessionId: activeSessionId, cols, rows }).catch(() => {});
      });
    });

    term.onSelectionChange(() => {
      hasSelection = term?.hasSelection() ?? false;
      if (hasSelection) showTouchMenu = true;
    });

    // Long-press (250ms) activates touch-locked Selection Mode overlay
    let lpTimer: ReturnType<typeof setTimeout> | null = null;
    let startX = 0;
    let startY = 0;

    const onTouchStart = (e: TouchEvent) => {
      if (selectionMode || e.touches.length !== 1) return;
      const t = e.touches[0];
      startX = t.clientX;
      startY = t.clientY;
      if (lpTimer) clearTimeout(lpTimer);
      lpTimer = setTimeout(() => {
        navigator.vibrate?.(25);
        const cell = pointToTerminalCell(startX, startY);
        selectionMode = true;
        showTouchMenu = true;
        selectWordAt(cell.col, cell.bufRow);
      }, 250);
    };

    const onTouchMove = (e: TouchEvent) => {
      if (selectionMode || e.touches.length !== 1) return;
      const t = e.touches[0];
      if (Math.hypot(t.clientX - startX, t.clientY - startY) > 18 && lpTimer) {
        clearTimeout(lpTimer);
        lpTimer = null;
      }
    };

    const onTouchEnd = () => {
      if (lpTimer) {
        clearTimeout(lpTimer);
        lpTimer = null;
      }
    };

    terminalContainer.addEventListener('touchstart', onTouchStart, { passive: true, capture: true });
    terminalContainer.addEventListener('touchmove', onTouchMove, { passive: true, capture: true });
    terminalContainer.addEventListener('touchend', onTouchEnd, { passive: true, capture: true });
    terminalContainer.addEventListener('touchcancel', onTouchEnd, { passive: true, capture: true });

    // Auto-fit on window resize (ignored while hidden so display:none never sends tiny PTY dims)
    resizeObserver = new ResizeObserver(() => {
      safeFit();
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
    const handleStartRelayEvent = (e: Event) => {
      const targetId = (e as CustomEvent<{ sessionId: string }>).detail?.sessionId;
      if (targetId === activeSessionId) {
        startHostRelay();
      }
    };
    const handleTerminalActionEvent = (e: Event) => {
      if (!active || isSplitPane) return;
      const action = (e as CustomEvent<{ action: string }>).detail?.action;
      if (action === 'clear') {
        term?.clear();
      } else if (action === 'zoom-in') {
        fontSize = Math.min(24, fontSize + 1);
        if (term) term.options.fontSize = fontSize;
        localStorage.setItem('opentermius_term_fontsize', String(fontSize));
        safeFit();
      } else if (action === 'zoom-out') {
        fontSize = Math.max(9, fontSize - 1);
        if (term) term.options.fontSize = fontSize;
        localStorage.setItem('opentermius_term_fontsize', String(fontSize));
        safeFit();
      } else if (action === 'zoom-reset') {
        fontSize = platform.isMobile ? 12 : 14;
        if (term) term.options.fontSize = fontSize;
        localStorage.setItem('opentermius_term_fontsize', String(fontSize));
        safeFit();
      }
    };
    window.addEventListener('opentermius:run-snippet', handleSnippetEvent);
    window.addEventListener('opentermius:start-relay', handleStartRelayEvent);
    window.addEventListener('opentermius:terminal-action', handleTerminalActionEvent);

    if (remoteConfig) {
      connectRemoteViewer();
    } else {
      connectNative();
    }

    return () => {
      window.removeEventListener('opentermius:run-snippet', handleSnippetEvent);
      window.removeEventListener('opentermius:start-relay', handleStartRelayEvent);
      window.removeEventListener('opentermius:terminal-action', handleTerminalActionEvent);
      terminalContainer?.removeEventListener('touchstart', onTouchStart, true);
      terminalContainer?.removeEventListener('touchmove', onTouchMove, true);
      terminalContainer?.removeEventListener('touchend', onTouchEnd, true);
      terminalContainer?.removeEventListener('touchcancel', onTouchEnd, true);
    };
  });

  onDestroy(() => {
    stopKeyRepeat();
    stopRelay();
    finalizeSessionLog();
    if (!remoteConfig) {
      import('@tauri-apps/api/core').then(({ invoke }) => {
        invoke('terminal_close', { sessionId: activeSessionId }).catch(() => {});
      });
    }
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
  <div
    class="relative flex {platform.isPhone ? 'h-9 px-2' : 'h-10 px-3 sm:px-4'} shrink-0 items-center justify-between border-b border-[#1a1a24] bg-[#0d0d12]"
    style={isFocusMode ? 'padding-top: env(safe-area-inset-top);' : ''}
  >
    <div class="flex items-center gap-2 min-w-0">
      <div class="flex items-center gap-2 min-w-0">
        <span class="relative flex h-2 w-2 shrink-0">
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
        <span class="text-xs font-semibold text-white tracking-wide truncate">{title}</span>
      </div>
    </div>

    <div class="flex items-center gap-1.5 shrink-0">
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

      {#if onToggleFocus && platform.isPhone}
        <button
          type="button"
          onclick={onToggleFocus}
          class="flex items-center justify-center rounded {btnTouch} text-xs transition {isFocusMode ? 'bg-cyan-500/20 text-cyan-300 border border-cyan-500/30' : 'bg-[#181822] text-neutral-300 hover:text-white'}"
          title={isFocusMode ? 'Show App Header' : 'Focus Terminal'}
          aria-label={isFocusMode ? 'Show App Header' : 'Focus Terminal'}
        >
          {#if isFocusMode}
            <Minimize2 class="h-3.5 w-3.5" />
          {:else}
            <Maximize2 class="h-3.5 w-3.5" />
          {/if}
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

      {#if !platform.isPhone}
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
      {/if}

      {#if relayParticipants.length > 0}
        <div class="flex -space-x-1.5 mr-1">
          {#each relayParticipants as p (p.user_id + p.mode)}
            <span
              title="{p.user_name} ({p.mode === 'host' ? 'Host' : p.mode === 'control' ? 'Control' : 'View'})"
              class="inline-flex h-5 w-5 items-center justify-center rounded-full text-[9px] font-bold text-white ring-2 ring-[#11111a] {getAvatarTone(p.user_id || p.user_name)}"
            >
              {getInitials(p.user_name)}
            </span>
          {/each}
        </div>
      {/if}
      {#if remoteConfig}
        <span class="rounded-full bg-indigo-500/20 border border-indigo-500/30 px-2.5 py-0.5 text-[10px] font-semibold text-indigo-300">
          {remoteConfig.mode === 'control' ? 'Remote Control' : 'View-Only'}
        </span>
      {:else if isRelaySharing}
        <button
          type="button"
          onclick={stopRelay}
          class="flex items-center gap-1 rounded-full bg-emerald-500/20 border border-emerald-500/40 px-2.5 py-0.5 text-[10px] font-semibold text-emerald-300 hover:bg-rose-500/20 hover:text-rose-300 hover:border-rose-500/40 transition"
          title="Stop remote relay"
        >
          Sharing ({relayViewerCount}) ×
        </button>
      {/if}

      {#if platform.isPhone}
        <button
          type="button"
          onclick={() => (isMoreMenuOpen = !isMoreMenuOpen)}
          class="flex items-center justify-center rounded bg-[#181822] {btnTouch} text-xs text-neutral-300 hover:text-white transition"
          title="More terminal actions"
          aria-label="More terminal actions"
        >
          <MoreVertical class="h-3.5 w-3.5" />
        </button>
        {#if isMoreMenuOpen}
          <div class="fixed inset-0 z-30" role="presentation" onclick={() => (isMoreMenuOpen = false)}></div>
          <div class="absolute right-2 top-10 z-40 w-44 rounded-xl border border-[#282838] bg-[#14141f] p-1.5 shadow-2xl space-y-1 text-xs">
            <button
              type="button"
              onclick={() => {
                isMoreMenuOpen = false;
                handleReconnect();
              }}
              class="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-neutral-200 hover:bg-[#1e1e2e]"
            >
              <RefreshCw class="h-3.5 w-3.5 text-emerald-400" />
              <span>Reconnect</span>
            </button>
            <button
              type="button"
              onclick={() => {
                isMoreMenuOpen = false;
                handleClear();
              }}
              class="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-neutral-200 hover:bg-[#1e1e2e]"
            >
              <Trash2 class="h-3.5 w-3.5 text-amber-400" />
              <span>Clear Terminal</span>
            </button>
          </div>
        {/if}
      {/if}

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
      <!-- Termux-style 2x7 Extra Keys Grid (Zero horizontal scroll, hold-to-repeat arrows) -->
      {@const termuxRows = [
        [
          { k: 'ESC', l: 'ESC' },
          { k: '/', l: '/' },
          { k: '-', l: '-' },
          { k: 'HOME', l: 'HOME' },
          { k: 'UP', l: '↑', repeat: true },
          { k: 'END', l: 'END' },
          { k: 'PGUP', l: 'PGUP' }
        ],
        [
          { k: 'TAB', l: 'TAB' },
          { k: 'MOD_CTRL', l: 'CTRL', mod: 'ctrl' },
          { k: 'MOD_ALT', l: 'ALT', mod: 'alt' },
          { k: 'LEFT', l: '←', repeat: true },
          { k: 'DOWN', l: '↓', repeat: true },
          { k: 'RIGHT', l: '→', repeat: true },
          { k: 'PGDN', l: 'PGDN' }
        ]
      ]}
      <div
        class="shrink-0 bg-[#0d0d13] {dockTop ? 'border-b' : 'border-t'} border-[#1f202c] px-1.5 py-1 space-y-1 select-none z-20"
        style={!dockTop ? 'padding-bottom: max(0.25rem, env(safe-area-inset-bottom));' : ''}
      >
        {#each termuxRows as row}
          <div class="grid grid-cols-7 gap-1">
            {#each row as key}
              {@const isModOn = (key.mod === 'ctrl' && ctrlActive) || (key.mod === 'alt' && altActive)}
              <button
                type="button"
                onpointerdown={(e) => {
                  if (key.repeat) {
                    startKeyRepeat(key.k, e);
                  } else {
                    e.preventDefault();
                    handleVirtualKey(key.k);
                  }
                }}
                onpointerup={() => key.repeat && stopKeyRepeat()}
                onpointercancel={() => key.repeat && stopKeyRepeat()}
                onpointerleave={() => key.repeat && stopKeyRepeat()}
                class="h-8 rounded-md border text-[11px] font-mono font-medium transition flex items-center justify-center {isModOn
                  ? 'bg-emerald-500 text-black border-emerald-400 font-bold'
                  : key.repeat
                    ? 'bg-[#1a1a26] border-[#2a2a3c] text-white font-bold active:bg-emerald-500 active:text-black'
                    : 'bg-[#14141e] border-[#222230] text-neutral-300 active:bg-emerald-600 active:text-white'}"
              >
                {key.l}
              </button>
            {/each}
          </div>
        {/each}
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
          <div class="flex flex-wrap items-center justify-center gap-2">
            {#if errorMessage?.includes('HOST_KEY_MISMATCH')}
              <button
                onclick={handleTrustHostKey}
                class="rounded-lg bg-amber-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-amber-500 transition"
              >
                Trust New Key & Reconnect
              </button>
            {/if}
            <button
              onclick={handleReconnect}
              class="rounded-lg bg-rose-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-rose-500 transition"
            >
              Retry Connection
            </button>
          </div>
        </div>
      </div>
    {/if}

    <div bind:this={terminalContainer} class="h-full w-full"></div>

    {#if typingUsers.length > 0}
      <div class="pointer-events-none absolute top-2.5 right-3 z-25 flex items-center gap-1.5 rounded-full border border-emerald-500/40 bg-[#12131f]/90 px-2.5 py-1 text-[11px] font-medium text-emerald-300 shadow-lg backdrop-blur-sm">
        <span class="inline-flex h-4 w-4 items-center justify-center rounded-full text-[8px] font-bold text-white {getAvatarTone(typingUsers[0].user_id)}">
          {getInitials(typingUsers[0].user_name)}
        </span>
        <span>
          {typingUsers.map((u) => u.user_name).join(', ')} is typing...
        </span>
      </div>
    {/if}

    {#if selectionMode}
      <!-- Transparent touch-none overlay: WebView never scrolls while dragging selection -->
      <div
        class="absolute inset-0 z-20 touch-none border-2 border-emerald-500/40 rounded-lg bg-emerald-500/[0.02]"
        role="application"
        aria-label="Terminal text selection"
        onpointerdown={handleOverlayPointerDown}
        onpointermove={handleOverlayPointerMove}
        onpointerup={handleOverlayPointerUp}
        onpointercancel={handleOverlayPointerUp}
      ></div>

      {#if startHandlePos}
        <div
          class="absolute z-30 touch-none -translate-x-1/2 -translate-y-1 flex flex-col items-center cursor-grab active:cursor-grabbing"
          style="left: {startHandlePos.x}px; top: {startHandlePos.y}px;"
          role="slider"
          aria-label="Selection start handle"
          aria-valuenow={selStartCol}
          tabindex="0"
          onpointerdown={(e) => handleHandlePointerDown('start', e)}
          onpointermove={handleOverlayPointerMove}
          onpointerup={handleOverlayPointerUp}
          onpointercancel={handleOverlayPointerUp}
        >
          <div class="h-3 w-0.5 bg-emerald-400"></div>
          <div class="h-5 w-5 rounded-full rounded-tr-none bg-emerald-400 border-2 border-[#0a0a0c] shadow-lg shadow-emerald-500/40 -rotate-45"></div>
          <div class="h-4 w-9 -mt-2"></div>
        </div>
      {/if}

      {#if endHandlePos}
        <div
          class="absolute z-30 touch-none -translate-x-1/2 -translate-y-1 flex flex-col items-center cursor-grab active:cursor-grabbing"
          style="left: {endHandlePos.x}px; top: {endHandlePos.y}px;"
          role="slider"
          aria-label="Selection end handle"
          aria-valuenow={selEndCol}
          tabindex="0"
          onpointerdown={(e) => handleHandlePointerDown('end', e)}
          onpointermove={handleOverlayPointerMove}
          onpointerup={handleOverlayPointerUp}
          onpointercancel={handleOverlayPointerUp}
        >
          <div class="h-3 w-0.5 bg-cyan-400"></div>
          <div class="h-5 w-5 rounded-full rounded-tl-none bg-cyan-400 border-2 border-[#0a0a0c] shadow-lg shadow-cyan-500/40 rotate-45"></div>
          <div class="h-4 w-9 -mt-2"></div>
        </div>
      {/if}
    {/if}

    {#if showTouchMenu}
      <div class="absolute top-3 right-3 z-30 flex items-center gap-1 rounded-full border border-[#2c2c3e] bg-[#14141e]/95 px-2.5 py-1 shadow-2xl backdrop-blur-md animate-in fade-in zoom-in-95 duration-100">
        {#if selectionMode}
          <span class="px-1.5 text-[10px] font-medium text-emerald-400">Drag to select</span>
        {/if}
        {#if hasSelection}
          <button
            type="button"
            onpointerdown={(e) => { e.preventDefault(); void handleCopySelection(); }}
            class="flex items-center gap-1.5 rounded-full bg-emerald-500 px-3 py-1.5 text-xs font-semibold text-black active:scale-95 transition"
          >
            <Copy class="h-3.5 w-3.5" /> Copy
          </button>
        {/if}
        <button
          type="button"
          onpointerdown={(e) => { e.preventDefault(); void handlePasteClipboard(); }}
          class="flex items-center gap-1.5 rounded-full bg-[#222232] px-3 py-1.5 text-xs font-medium text-white active:scale-95 transition"
        >
          <ClipboardPaste class="h-3.5 w-3.5 text-cyan-400" /> Paste
        </button>
        <button
          type="button"
          onpointerdown={(e) => { e.preventDefault(); exitSelectionMode(); }}
          class="flex h-7 w-7 items-center justify-center rounded-full text-neutral-400 hover:text-white"
          aria-label="Dismiss"
        >
          <X class="h-3.5 w-3.5" />
        </button>
      </div>
    {/if}
  </div>

  {#if showAccessoryBar && !dockTop}
    {@render accessoryBarSnippet()}
  {/if}
</div>
