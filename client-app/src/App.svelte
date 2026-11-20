<script lang="ts">
  import { onMount } from 'svelte';
  import Sidebar from './lib/components/Sidebar.svelte';
  import HostCard from './lib/components/HostCard.svelte';
  import TerminalView from './lib/components/TerminalView.svelte';
  import VaultsView from './lib/components/VaultsView.svelte';
  import KeychainView from './lib/components/KeychainView.svelte';
  import IdentitiesView from './lib/components/IdentitiesView.svelte';
  import SnippetsView from './lib/components/SnippetsView.svelte';
  import SessionLogsView from './lib/components/SessionLogsView.svelte';
  import AddHostDialog from './lib/components/AddHostDialog.svelte';
  import MoveHostModal from './lib/components/MoveHostModal.svelte';
  import ImportModal from './lib/components/ImportModal.svelte';
  import TunnelsView from './lib/components/TunnelsView.svelte';
  import SftpView from './lib/components/SftpView.svelte';
  import ToastContainer from './lib/components/ToastContainer.svelte';
  import { terminalStore } from './lib/stores/terminal.svelte';
  import { platform } from './lib/stores/platform.svelte';
  import { toast, formatErrorMessage } from './lib/stores/toast.svelte';
  import {
    api,
    type ApiVault,
    type ApiVaultItem,
    type DevicePresence,
    type RemoteRequest
  } from './lib/api/client';
  import { encryptVaultPayload, decryptVaultPayload } from './lib/crypto/vault';
  import {
    Plus,
    Search,
    Server,
    Shield,
    Sparkles,
    RefreshCw,
    AlertCircle,
    X,
    Lock,
    KeyRound,
    Folder,
    ChevronDown,
    ChevronRight,
    Laptop,
    Radio,
    ArrowRight,
    ArrowLeft,
    Network,
    HardDrive,
    CheckCircle2,
    PanelLeft,
    PanelLeftOpen,
    Download,
    Menu,
    Terminal,
    FolderLock,
    Key,
    UserCheck,
    FileCode2,
    ScrollText,
    Settings,
    LayoutGrid,
    Eye,
    EyeOff
  } from '@lucide/svelte';

  // Svelte 5 Runes State
  let showAuthPassword = $state(false);
  let activeNav = $state('hosts');
  let searchQuery = $state('');
  let isAddHostOpen = $state(false);
  let isImportOpen = $state(false);
  let editingHost = $state<any | null>(null);
  let movingHost = $state<any | null>(null);
  let isMoveModalOpen = $state(false);
  let selectedFolderFilter = $state<string>('all');
  let collapsedFolders = $state<Record<string, boolean>>({});
  let isSidebarCollapsed = $state(false);
  let isMobileMenuOpen = $state(false);
  let isSessionSwitcherOpen = $state(false);
  let isVaultSwitcherOpen = $state(false);
  let isQuickConnectOpen = $state(false);
  let quickConnectQuery = $state('');
  let quickConnectInput = $state<HTMLInputElement | null>(null);
  let isTerminalFocus = $state(false);
  let sftpSelectedHostId = $state('');
  let currentUserId = $state('');
  let gatewayOnline = $state(true);
  let teamPresence = $state<DevicePresence[]>([]);
  let isTeamLiveExpanded = $state(false);
  let incomingRemoteRequests = $state<RemoteRequest[]>([]);
  let pendingOutboundRequestId = $state<string | null>(null);

  function handleOpenSftp(host: any) {
    sftpSelectedHostId = host.id;
    activeNav = 'sftp';
  }

  function openQuickConnect() {
    if (!isAuthenticated) return;
    quickConnectQuery = '';
    isQuickConnectOpen = true;
    setTimeout(() => quickConnectInput?.focus(), 20);
  }

  function handleKeydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'b') {
      e.preventDefault();
      isSidebarCollapsed = !isSidebarCollapsed;
    } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 't') {
      e.preventDefault();
      openQuickConnect();
    } else if (e.key === 'Escape' && isQuickConnectOpen) {
      e.preventDefault();
      isQuickConnectOpen = false;
    }
  }

  // Gateway & Standalone Connection State
  let gatewayApiUrl = $state(api.getApiBase());
  let connectionChoice = $state<'select' | 'gateway' | 'standalone'>(
    api.isStandalone() ? 'standalone' : api.getToken() ? 'gateway' : 'select'
  );

  // Auth State
  let isAuthenticated = $state(api.isStandalone() || Boolean(api.getToken()));
  let authEmail = $state('admin@opentermius.local');
  let authPassword = $state('OpenTermius2026!');
  let authError = $state<string | null>(null);
  let isAuthLoading = $state(false);

  // Backend Data State (4 UI States)
  let loadState = $state<'loading' | 'success' | 'empty' | 'error'>('loading');
  let errorMessage = $state<string | null>(null);
  let activeVault = $state<ApiVault | null>(null);
  let vaults = $state<ApiVault[]>([]);
  let hosts = $state<Array<{
    id: string;
    label: string;
    address: string;
    port: number;
    username: string;
    tags: string[];
    useMosh?: boolean;
    pingMs: number;
    group: string;
  }>>([]);

  let quickConnectHosts = $derived(
    hosts.filter((h) => {
      const q = quickConnectQuery.trim().toLowerCase();
      if (!q) return true;
      return (
        h.label.toLowerCase().includes(q) ||
        h.address.toLowerCase().includes(q) ||
        h.username.toLowerCase().includes(q) ||
        (h.group || '').toLowerCase().includes(q)
      );
    })
  );

  let filteredHosts = $derived(
    hosts.filter(
      (h) =>
        h.label.toLowerCase().includes(searchQuery.toLowerCase()) ||
        h.address.toLowerCase().includes(searchQuery.toLowerCase()) ||
        h.tags.some((t) => t.toLowerCase().includes(searchQuery.toLowerCase()))
    )
  );

  let availableFolders = $derived(
    Array.from(new Set(hosts.map((h) => h.group || 'Default'))).sort()
  );

  let displayedHosts = $derived(
    filteredHosts.filter(
      (h) => selectedFolderFilter === 'all' || (h.group || 'Default') === selectedFolderFilter
    )
  );

  let hostsByFolder = $derived.by(() => {
    const map: Record<string, typeof hosts> = {};
    for (const host of displayedHosts) {
      const g = host.group || 'Default';
      if (!map[g]) map[g] = [];
      map[g].push(host);
    }
    return map;
  });

  async function checkAuthAndLoadData() {
    loadState = 'loading';
    errorMessage = null;

    try {
      if (api.isStandalone()) {
        isAuthenticated = true;
        vaults = await api.getVaults();
        activeVault = vaults[0];
        await loadHosts();
        loadState = 'success';
        return;
      }

      // 1. Check if token exists and is valid
      const token = api.getToken();
      if (!token) {
        isAuthenticated = false;
        connectionChoice = 'select';
        loadState = 'success';
        return;
      }

      const me = await api.getMe();
      currentUserId = me.id;
      isAuthenticated = true;

      // 2. Load Vaults from Backend (ensure user always has a Personal Vault even if invited to a Team Vault first)
      vaults = await api.getVaults();
      if (!vaults.some((v) => v.type === 'personal')) {
        const initialVault = await api.createVault(
          'Personal Cloud Vault',
          'personal',
          'initial-master-vault-key-aes-256'
        );
        vaults = [initialVault, ...vaults];
      }
      if (!activeVault || !vaults.some((v) => v.id === activeVault?.id)) {
        activeVault = vaults[0];
      }

      // 3. Load Hosts from Backend Database
      await loadHosts();
    } catch (err: unknown) {
      if (err instanceof Error && err.message.includes('401')) {
        api.clearToken();
        isAuthenticated = false;
        connectionChoice = 'select';
        loadState = 'success';
      } else {
        loadState = 'error';
        errorMessage = formatErrorMessage(err, 'Cannot connect to OpenTermius backend');
      }
    }
  }

  async function loadHosts() {
    if (!activeVault) return;
    try {
      const items = await api.getVaultItems(activeVault.id, 'host');
      if (items.length === 0) {
        hosts = [];
        loadState = 'empty';
        return;
      }

      hosts = await Promise.all(
        items.map(async (item) => {
          const parsed = await decryptVaultPayload<any>(
            item.ciphertext,
            item.nonce,
            activeVault?.encrypted_key
          );
          return {
            id: item.id,
            label: item.label,
            address: parsed?.address || '127.0.0.1',
            port: parsed?.port || 22,
            username: parsed?.username || 'root',
            authType: parsed?.authType || 'password',
            password: parsed?.password || '',
            privateKey: parsed?.privateKey || '',
            passphrase: parsed?.passphrase || '',
            tags: parsed?.tags || [],
            group: parsed?.group || 'Default',
            useMosh: !!parsed?.useMosh,
            pingMs: Math.floor(Math.random() * 20) + 10
          };
        })
      );

      loadState = 'success';
    } catch (err: unknown) {
      loadState = 'error';
      errorMessage = formatErrorMessage(err, 'Failed to fetch hosts from database');
    }
  }

  async function handleAuthSubmit() {
    isAuthLoading = true;
    authError = null;

    try {
      api.setGateway(gatewayApiUrl.trim());
      await api.login(authEmail, authPassword);
      const migrated = await api.syncStandaloneToGateway();
      if (migrated > 0) {
        toast.success(`Synced ${migrated} local item(s) to your Personal Vault.`);
      }
      isAuthenticated = true;
      await checkAuthAndLoadData();
    } catch (err: unknown) {
      authError = formatErrorMessage(err, 'Gateway connection failed');
    } finally {
      isAuthLoading = false;
    }
  }

  async function handleStartStandalone() {
    isAuthLoading = true;
    authError = null;
    try {
      api.setStandalone(true);
      isAuthenticated = true;
      await checkAuthAndLoadData();
    } catch (err: unknown) {
      authError = err instanceof Error ? err.message : 'Failed to start standalone mode';
    } finally {
      isAuthLoading = false;
    }
  }

  function handleSignOut() {
    api.clearToken();
    isAuthenticated = false;
    connectionChoice = 'select';
  }

  async function handleSaveHost(hostData: any) {
    if (!activeVault) return;

    try {
      // Encrypt host connection payload client-side before sending to backend
      const payload = {
        address: hostData.address,
        port: hostData.port,
        username: hostData.username,
        authType: hostData.authType,
        password: hostData.password,
        privateKey: hostData.privateKey,
        passphrase: hostData.passphrase,
        tags: hostData.tags,
        group: hostData.group,
        useMosh: hostData.useMosh
      };

      const { ciphertext, nonce } = await encryptVaultPayload(payload, activeVault.encrypted_key);
      const isExistingHost = Boolean(editingHost || (hostData.id && hosts.some((h) => h.id === hostData.id)));

      if (isExistingHost) {
        // Update existing host item
        await api.updateVaultItem(activeVault.id, hostData.id, {
          label: hostData.label,
          ciphertext,
          nonce
        });

        const idx = hosts.findIndex((h) => h.id === hostData.id);
        if (idx !== -1) {
          hosts[idx] = { ...hostData };
        }
        editingHost = null;
      } else {
        // Create new host item
        const created = await api.createVaultItem(activeVault.id, {
          kind: 'host',
          label: hostData.label,
          ciphertext,
          nonce
        });

        hosts.unshift({
          ...hostData,
          id: created.id
        });
      }
      loadState = 'success';
    } catch (err: unknown) {
      toast.error(err, 'Failed to save host to database');
    }
  }

  function handleEditHost(host: any) {
    editingHost = host;
    isAddHostOpen = true;
  }

  async function handleDeleteHost(host: any) {
    if (!activeVault) return;
    if (!(await toast.confirm(`Are you sure you want to remove host "${host.label}"?`))) return;

    try {
      await api.deleteVaultItem(activeVault.id, host.id);
      hosts = hosts.filter((h) => h.id !== host.id);
      if (hosts.length === 0) {
        loadState = 'empty';
      }
    } catch (err: unknown) {
      toast.error(err, 'Failed to delete host');
    }
  }

  function handleOpenMoveModal(host: any) {
    movingHost = host;
    isMoveModalOpen = true;
    api.getVaults().then((loaded) => {
      if (loaded.length > 0) vaults = loaded;
    }).catch(() => {});
  }

  async function handleMoveToFolder(newFolder: string) {
    const targetHost = movingHost;
    if (!targetHost) return;
    const updated = { ...targetHost, group: newFolder };
    await handleSaveHost(updated);
    toast.success(`Moved "${targetHost.label}" to folder "${newFolder}".`);
  }

  async function handleMoveToVault(targetVaultId: string) {
    const targetHost = movingHost;
    const sourceVault = activeVault;
    if (!targetHost || !sourceVault) return;
    try {
      await api.moveVaultItem(sourceVault.id, targetHost.id, targetVaultId);
      const targetVault = vaults.find((v) => v.id === targetVaultId);
      if (targetVault && targetVault.encrypted_key !== sourceVault.encrypted_key) {
        const payload = {
          address: targetHost.address,
          port: targetHost.port,
          username: targetHost.username,
          authType: targetHost.authType,
          password: targetHost.password,
          privateKey: targetHost.privateKey,
          passphrase: targetHost.passphrase,
          tags: targetHost.tags,
          group: targetHost.group,
          useMosh: targetHost.useMosh
        };
        const { ciphertext, nonce } = await encryptVaultPayload(payload, targetVault.encrypted_key);
        await api.updateVaultItem(targetVaultId, targetHost.id, {
          label: targetHost.label,
          ciphertext,
          nonce
        });
      }
      hosts = hosts.filter((h) => h.id !== targetHost.id);
      if (hosts.length === 0) {
        loadState = 'empty';
      }
      const targetVaultName = targetVault?.name || 'target vault';
      toast.success(`Transferred "${targetHost.label}" to ${targetVaultName}.`);
    } catch (err: unknown) {
      toast.error(err, 'Failed to move host to vault');
    }
  }

  function toggleFolder(folder: string) {
    collapsedFolders[folder] = !collapsedFolders[folder];
  }

  function handleConnect(host: any) {
    terminalStore.openTab(
      host.id,
      `${host.username}@${host.label}`,
      {
        address: host.address,
        port: host.port,
        username: host.username,
        authType: host.authType,
        password: host.authType === 'key' ? '' : host.password,
        privateKey: host.authType === 'key' ? host.privateKey : '',
        passphrase: host.authType === 'key' ? host.passphrase : ''
      },
      { vaultId: activeVault?.id, vaultName: activeVault?.name }
    );
    activeNav = 'terminal';
    void runPresenceHeartbeat();
  }

  function handleNewLocalTab() {
    const count = terminalStore.tabCount + 1;
    terminalStore.openTab(`local-${Date.now()}`, `Local Shell #${count}`, {
      address: '',
      port: 22,
      username: ''
    });
    activeNav = 'terminal';
  }

  function handleSplitTerminal(direction: 'horizontal' | 'vertical') {
    if (!terminalStore.activeTab) return;
    const curr = terminalStore.activeTab;
    terminalStore.splitCurrent(
      curr.hostId,
      `${curr.label} (Split)`,
      curr.config,
      direction
    );
  }

  async function handleImportHosts(importedList: any[]) {
    if (!activeVault) return;
    for (const h of importedList) {
      const payload = {
        address: h.address,
        port: h.port || 22,
        username: h.username || 'root',
        group: h.group || 'Imported',
        tags: h.tags || ['imported'],
        authType: h.authType || 'password',
        password: h.password || '',
        privateKey: h.privateKey || '',
        passphrase: h.passphrase || ''
      };
      const { ciphertext, nonce } = await encryptVaultPayload(payload, activeVault.encrypted_key);
      await api.createVaultItem(activeVault.id, {
        kind: 'host',
        label: h.label,
        ciphertext,
        nonce
      });
    }
    await loadHosts();
  }

  async function handleSelectVault(vault: any) {
    activeVault = vault;
    await loadHosts();
  }

  async function runPresenceHeartbeat() {
    if (!isAuthenticated || api.isStandalone()) {
      gatewayOnline = false;
      teamPresence = [];
      return;
    }
    const activeSessions = terminalStore.tabs
      .filter((t) => !t.remoteConfig)
      .map((t) => ({
        session_id: t.id,
        vault_id: t.vaultId || activeVault?.id || '',
        vault_name: t.vaultName || activeVault?.name || 'Vault',
        host_label: t.label,
        started_at: t.startedAt
      }));

    const deviceName = platform.isAndroid ? 'Android Device' : platform.isMac ? 'macOS Desktop' : 'Desktop Client';
    const res = await api.sendHeartbeat(deviceName, platform.layoutMode, activeSessions);
    gatewayOnline = res.online;
    incomingRemoteRequests = res.pending_requests;

    if (res.online && activeVault?.type === 'team') {
      try {
        teamPresence = await api.getPresence(activeVault.id);
      } catch {
        teamPresence = [];
      }
    } else {
      teamPresence = [];
    }
  }

  function getAvatarInitials(name: string): string {
    const parts = (name || '?').trim().split(/\s+/);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return (parts[0]?.slice(0, 2) || '?').toUpperCase();
  }

  function getAvatarTone(seed: string): string {
    const tones = [
      'bg-emerald-600 text-white',
      'bg-indigo-600 text-white',
      'bg-cyan-600 text-white',
      'bg-amber-600 text-white',
      'bg-rose-600 text-white',
      'bg-violet-600 text-white'
    ];
    let hash = 0;
    for (let i = 0; i < seed.length; i++) {
      hash = (hash * 31 + seed.charCodeAt(i)) | 0;
    }
    return tones[Math.abs(hash) % tones.length];
  }

  function handleRejoinRemote(
    sessionId: string,
    vaultId: string,
    hostLabel: string,
    requestId: string,
    mode: 'view' | 'control',
    ownerName: string
  ) {
    terminalStore.openTab(
      `remote-${sessionId}`,
      `${hostLabel} (${mode})`,
      undefined,
      {
        vaultId,
        remoteConfig: {
          sessionId,
          requestId,
          mode,
          ownerName
        }
      }
    );
    activeNav = 'terminal';
    toast.success(`Rejoined ${hostLabel} (${mode})`);
  }

  let remotePollTimer: ReturnType<typeof setInterval> | null = null;

  async function handleRequestRemote(sessionId: string, hostLabel: string, ownerName: string, mode: 'view' | 'control') {
    try {
      const req = await api.requestRemoteSession(sessionId, mode);
      pendingOutboundRequestId = req.id;
      toast.info(`Requested ${mode} access to ${hostLabel}. Waiting for ${ownerName}...`);

      if (remotePollTimer) {
        clearInterval(remotePollTimer);
        remotePollTimer = null;
      }

      let attempts = 0;
      const poll = setInterval(async () => {
        attempts++;
        if (attempts > 40 || pendingOutboundRequestId !== req.id) {
          clearInterval(poll);
          if (remotePollTimer === poll) remotePollTimer = null;
          if (pendingOutboundRequestId === req.id) pendingOutboundRequestId = null;
          return;
        }
        try {
          const latest = await api.getRemoteRequest(req.id);
          if (latest.status === 'approved') {
            clearInterval(poll);
            if (remotePollTimer === poll) remotePollTimer = null;
            pendingOutboundRequestId = null;
            terminalStore.openTab(
              `remote-${latest.session_id}`,
              `${latest.host_label} (${latest.mode})`,
              undefined,
              {
                vaultId: latest.vault_id,
                remoteConfig: {
                  sessionId: latest.session_id,
                  requestId: latest.id,
                  mode: latest.mode,
                  ownerName
                }
              }
            );
            activeNav = 'terminal';
            toast.success(`Connected to ${latest.host_label} (${latest.mode})`);
          } else if (latest.status === 'rejected') {
            clearInterval(poll);
            if (remotePollTimer === poll) remotePollTimer = null;
            pendingOutboundRequestId = null;
            toast.error('Remote access request declined');
          }
        } catch {
          // ignore transient poll error
        }
      }, 1500);
      remotePollTimer = poll;
    } catch (err: unknown) {
      toast.error(err, 'Failed to request remote access');
    }
  }

  async function handleRespondRemote(req: RemoteRequest, action: 'approve' | 'reject', mode: 'view' | 'control' = req.mode) {
    try {
      await api.respondRemoteRequest(req.id, action, mode);
      incomingRemoteRequests = incomingRemoteRequests.filter((r) => r.id !== req.id);
      if (action === 'approve') {
        window.dispatchEvent(new CustomEvent('opentermius:start-relay', { detail: { sessionId: req.session_id } }));
        toast.success(`Granted ${mode} access to ${req.requester_name}`);
      } else {
        toast.info(`Declined request from ${req.requester_name}`);
      }
    } catch (err: unknown) {
      toast.error(err, 'Failed to respond to request');
    }
  }

  async function silentBackgroundSync() {
    if (!isAuthenticated || api.isStandalone() || isAddHostOpen || isMoveModalOpen) return;
    try {
      let latestVaults = await api.getVaults();
      if (gatewayOnline && !latestVaults.some((v) => v.type === 'personal')) {
        const personal = await api.createVault(
          'Personal Cloud Vault',
          'personal',
          'initial-master-vault-key-aes-256'
        );
        latestVaults = [personal, ...latestVaults];
      }
      if (latestVaults.length > 0) {
        vaults = latestVaults;
        if (!activeVault || !latestVaults.some((v) => v.id === activeVault?.id)) {
          activeVault = latestVaults[0];
        }
      }
      await loadHosts();
    } catch {
      // Ignore background polling errors silently
    }
  }

  let hideMobileBottomNav = $derived(
    platform.isMobile && (platform.isKeyboardOpen || platform.isInputFocused) && activeNav === 'terminal'
  );

  $effect(() => {
    const count = terminalStore.tabCount;
    (window as any).AndroidClipboard?.setKeepAliveSessions?.(count);
  });

  const APP_VERSION = '0.5.0';
  type DesktopUpdate = Awaited<ReturnType<typeof import('@tauri-apps/plugin-updater').check>>;
  let availableUpdate = $state<DesktopUpdate | null>(null);
  let releaseFallback = $state<{ version: string; url: string } | null>(null);
  let isCheckingUpdate = $state(false);
  let isUpdating = $state(false);
  let updateDismissed = $state(false);

  function isNewerSemver(remote: string, current: string): boolean {
    const r = remote.replace(/^v/i, '').split('.').map((n) => parseInt(n, 10) || 0);
    const c = current.replace(/^v/i, '').split('.').map((n) => parseInt(n, 10) || 0);
    for (let i = 0; i < 3; i++) {
      if ((r[i] || 0) > (c[i] || 0)) return true;
      if ((r[i] || 0) < (c[i] || 0)) return false;
    }
    return false;
  }

  async function checkDesktopUpdates(manual = false) {
    if (isCheckingUpdate) return;
    isCheckingUpdate = true;
    try {
      if (!platform.isAndroid && (window as any).__TAURI_INTERNALS__) {
        try {
          const { check } = await import('@tauri-apps/plugin-updater');
          const update = await check();
          if (update) {
            availableUpdate = update;
            updateDismissed = false;
            if (manual) toast.info(`Update v${update.version} is ready to install.`);
            return;
          }
        } catch {
          // Fallback to GitHub Releases API check below
        }
      }

      const res = await fetch('https://api.github.com/repos/wisnu-w/OpenTermius/releases/latest');
      if (res.ok) {
        const data = await res.json();
        const remoteVer = String(data.tag_name || '').replace(/^v/i, '');
        if (remoteVer && isNewerSemver(remoteVer, APP_VERSION)) {
          const assets: Array<{ name: string; browser_download_url: string }> = Array.isArray(data.assets) ? data.assets : [];
          let targetAsset = data.html_url || 'https://github.com/wisnu-w/OpenTermius/releases/latest';
          if (platform.isAndroid) {
            targetAsset = assets.find((a) => a.name.endsWith('.apk'))?.browser_download_url || targetAsset;
          } else if (platform.isMac) {
            targetAsset = assets.find((a) => a.name.endsWith('.dmg'))?.browser_download_url || targetAsset;
          } else {
            targetAsset = assets.find((a) => a.name.endsWith('.exe'))?.browser_download_url || targetAsset;
          }
          releaseFallback = { version: remoteVer, url: targetAsset };
          updateDismissed = false;
          if (manual) toast.info(`Update v${remoteVer} is available!`);
          return;
        }
      }

      if (manual) {
        toast.success(`You are on the latest version (v${APP_VERSION}).`);
      }
    } catch {
      if (manual) {
        toast.error('Could not check for updates. Check your internet connection.');
      }
    } finally {
      isCheckingUpdate = false;
    }
  }

  async function installDesktopUpdate() {
    if (!availableUpdate || isUpdating) return;
    isUpdating = true;
    try {
      await availableUpdate.downloadAndInstall();
      const { relaunch } = await import('@tauri-apps/plugin-process');
      await relaunch();
    } catch (e) {
      toast.error(`Update failed: ${formatErrorMessage(e)}`);
      isUpdating = false;
    }
  }

  onMount(() => {
    checkAuthAndLoadData();
    void checkDesktopUpdates();
    const unsubSync = api.onSyncComplete((count) => {
      toast.success(`Synced ${count} offline change(s) to Gateway.`);
      void loadHosts();
    });
    const syncTimer = setInterval(silentBackgroundSync, 15000);
    const heartbeatTimer = setInterval(runPresenceHeartbeat, 8000);
    const onFocus = () => {
      void silentBackgroundSync();
      void runPresenceHeartbeat();
    };
    window.addEventListener('focus', onFocus);
    void runPresenceHeartbeat();

    let lastBackMs = 0;
    const onAndroidBack = () => {
      if (isAddHostOpen) { isAddHostOpen = false; editingHost = null; return; }
      if (isMoveModalOpen) { isMoveModalOpen = false; movingHost = null; return; }
      if (isImportOpen) { isImportOpen = false; return; }
      if (isSessionSwitcherOpen) { isSessionSwitcherOpen = false; return; }
      if (isVaultSwitcherOpen) { isVaultSwitcherOpen = false; return; }
      if (isQuickConnectOpen) { isQuickConnectOpen = false; return; }

      const subPages = ['vaults', 'keychain', 'identities', 'snippets', 'logs', 'settings'];
      if (subPages.includes(activeNav)) {
        activeNav = 'more';
        return;
      }
      if (activeNav !== 'hosts') {
        activeNav = 'hosts';
        return;
      }

      const now = Date.now();
      if (now - lastBackMs < 2000) {
        (window as any).AndroidClipboard?.moveToBackground?.();
      } else {
        lastBackMs = now;
        toast.info('Swipe back again to minimize');
      }
    };
    window.addEventListener('opentermius:android-back', onAndroidBack);

    return () => {
      unsubSync();
      clearInterval(syncTimer);
      clearInterval(heartbeatTimer);
      if (remotePollTimer) clearInterval(remotePollTimer);
      window.removeEventListener('focus', onFocus);
      window.removeEventListener('opentermius:android-back', onAndroidBack);
    };
  });
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="flex h-full w-screen overflow-hidden bg-[#0a0a0c] text-neutral-100 font-sans select-none">
  <!-- Left Navigation Sidebar (Desktop & Tablet only; Mobile uses native Bottom Nav + More screen) -->
  {#if !platform.isMobile}
    <Sidebar
      {activeNav}
      onNavChange={(nav) => (activeNav = nav)}
      {activeVault}
      {vaults}
      onSelectVault={handleSelectVault}
      isCollapsed={isSidebarCollapsed}
      onToggleCollapse={() => (isSidebarCollapsed = !isSidebarCollapsed)}
      isMobileOpen={isMobileMenuOpen}
      onCloseMobile={() => (isMobileMenuOpen = false)}
      {gatewayOnline}
      onSwitchToGateway={() => {
        isAuthenticated = false;
        connectionChoice = 'gateway';
      }}
    />
  {/if}

  <!-- Main Viewport Area -->
  <main class="flex flex-1 flex-col min-w-0 min-h-0 overflow-hidden">
    {#if (availableUpdate || releaseFallback) && !updateDismissed}
      {@const targetVersion = availableUpdate?.version || releaseFallback?.version}
      <div class="flex items-center justify-between gap-3 border-b border-emerald-500/30 bg-emerald-500/10 px-4 py-2 text-xs text-emerald-100 shrink-0">
        <div class="flex items-center gap-2 min-w-0">
          <Sparkles class="h-3.5 w-3.5 text-emerald-400 shrink-0" />
          <span class="truncate">
            Update <strong class="font-mono text-emerald-300">v{targetVersion}</strong> is ready to install.
          </span>
        </div>
        <div class="flex items-center gap-2 shrink-0">
          {#if availableUpdate}
            <button
              type="button"
              disabled={isUpdating}
              onclick={installDesktopUpdate}
              class="rounded-md bg-emerald-500 px-2.5 py-1 font-medium text-neutral-950 hover:bg-emerald-400 disabled:opacity-50"
            >
              {isUpdating ? 'Updating...' : 'Update & Restart'}
            </button>
          {:else if releaseFallback}
            <a
              href={releaseFallback.url}
              target="_blank"
              rel="noopener noreferrer"
              class="rounded-md bg-emerald-500 px-2.5 py-1 font-medium text-neutral-950 hover:bg-emerald-400"
            >
              Download v{releaseFallback.version}
            </a>
          {/if}
          <button
            type="button"
            disabled={isUpdating}
            onclick={() => (updateDismissed = true)}
            class="rounded-md px-2 py-1 text-neutral-400 hover:text-neutral-200"
          >
            Later
          </button>
        </div>
      </div>
    {/if}

    <!-- Top Header & Tabs Bar -->
    {#if platform.isMobile}
      {@const navTitles: Record<string, string> = {
        hosts: 'Hosts',
        sftp: 'SFTP',
        tunnels: 'Tunnels',
        more: 'More',
        vaults: 'Vaults',
        keychain: 'Keychain',
        identities: 'Identities',
        snippets: 'Snippets',
        logs: 'Session Logs',
        settings: 'Settings'
      }}
      {@const isSubPage = ['vaults', 'keychain', 'identities', 'snippets', 'logs', 'settings'].includes(activeNav)}
      {@const activeTab = terminalStore.tabs.find((t) => t.active)}
      {#if !(activeNav === 'terminal' && isTerminalFocus)}
      <header
        class="flex min-h-14 items-center justify-between border-b border-[#1f1f2a] bg-[#0e0e13] px-3.5 shrink-0 select-none"
        style="padding-top: env(safe-area-inset-top);"
      >
        <div class="flex items-center gap-2 min-w-0 flex-1">
          {#if isSubPage}
            <button
              type="button"
              onclick={() => (activeNav = 'more')}
              class="flex h-10 w-10 -ml-1.5 items-center justify-center rounded-xl text-neutral-300 active:bg-[#1a1a24] transition shrink-0"
              aria-label="Back to More"
            >
              <ArrowLeft class="h-5 w-5" />
            </button>
          {/if}

          {#if activeNav === 'terminal'}
            <button
              type="button"
              onpointerdown={(e) => {
                e.preventDefault();
                (document.activeElement as HTMLElement | null)?.blur?.();
                activeNav = 'hosts';
              }}
              class="flex h-10 w-10 -ml-1.5 items-center justify-center rounded-xl text-neutral-300 active:bg-[#1a1a24] transition shrink-0"
              aria-label="Back to Hosts"
              title="Back to Hosts"
            >
              <ArrowLeft class="h-5 w-5" />
            </button>
            <button
              type="button"
              onclick={() => (isSessionSwitcherOpen = true)}
              class="flex items-center gap-2 max-w-[200px] rounded-full bg-[#161620] border border-[#282838] px-3.5 py-1.5 text-xs font-semibold text-white active:scale-95 transition"
            >
              <span class="h-2 w-2 rounded-full bg-emerald-400 shrink-0"></span>
              <span class="truncate">{activeTab ? activeTab.label : 'Terminal'}</span>
              <ChevronDown class="h-3.5 w-3.5 text-neutral-400 shrink-0" />
            </button>
          {:else}
            <h1 class="text-lg font-semibold text-white tracking-tight truncate">
              {navTitles[activeNav] ?? 'OpenTermius'}
            </h1>
            {#if activeNav === 'hosts' && activeVault}
              <button
                type="button"
                onclick={() => (isVaultSwitcherOpen = true)}
                class="ml-1 flex items-center gap-1 rounded-full bg-emerald-500/10 border border-emerald-500/25 px-2.5 py-1 text-[11px] font-medium text-emerald-400 active:scale-95 transition max-w-[140px]"
              >
                <span class="truncate">{activeVault.name}</span>
                <ChevronDown class="h-3 w-3 shrink-0" />
              </button>
            {/if}
          {/if}
        </div>

        <div class="flex items-center gap-1.5 shrink-0">
          <button
            type="button"
            onclick={openQuickConnect}
            class="flex h-10 w-10 items-center justify-center rounded-xl bg-[#161620] border border-[#252535] text-emerald-400 active:scale-95 transition"
            title="New Connection Tab (⌘T)"
            aria-label="New Connection Tab"
          >
            <Plus class="h-4.5 w-4.5" />
          </button>
        </div>
      </header>
      {/if}
    {:else}
      <header
        data-tauri-drag-region
        class="flex h-11 items-center justify-between border-b border-[#1c1c25] bg-[#111116] px-2.5 sm:px-3 shrink-0 {platform.isMac && platform.isNativeTauri && isSidebarCollapsed ? 'pl-20' : ''}"
      >
        <!-- Left: Mobile Menu / Desktop Toggle + Tabs Bar -->
        <div data-tauri-drag-region class="flex items-center gap-1 overflow-x-auto no-scrollbar flex-1 mr-2 sm:mr-4">
          <!-- Tablet Drawer Toggle Button -->
          <button
            type="button"
            onclick={() => (isMobileMenuOpen = !isMobileMenuOpen)}
            class="flex lg:hidden items-center justify-center rounded-lg p-1.5 text-neutral-400 hover:text-white hover:bg-[#1a1a24] transition mr-0.5 shrink-0"
            aria-label="Toggle navigation menu"
          >
            <Menu class="h-4 w-4 text-emerald-400" />
          </button>

          <!-- Desktop Sidebar Toggle (Hide / Unhide) -->
          <button
            type="button"
            onclick={() => (isSidebarCollapsed = !isSidebarCollapsed)}
            class="hidden lg:flex items-center justify-center rounded-lg p-1.5 text-neutral-400 hover:text-white hover:bg-[#1a1a24] transition mr-1 shrink-0"
            title={isSidebarCollapsed ? "Show Sidebar (⌘B)" : "Hide Sidebar (⌘B)"}
          >
            {#if isSidebarCollapsed}
              <PanelLeftOpen class="h-4 w-4 text-emerald-400" />
            {:else}
              <PanelLeft class="h-4 w-4" />
            {/if}
          </button>

          <button
            onclick={() => (activeNav = 'hosts')}
            class="flex items-center gap-1.5 sm:gap-2 rounded-lg px-2.5 sm:px-3 py-1.5 text-xs font-semibold transition shrink-0 {activeNav === 'hosts' ? 'bg-[#1a1a24] text-white border border-[#2a2a38]' : 'text-neutral-400 hover:text-white'}"
          >
            <Server class="h-3.5 w-3.5" />
            <span class="hidden sm:inline">Hosts Dashboard</span>
            <span class="sm:hidden">Hosts</span>
          </button>

          {#each terminalStore.tabs as tab (tab.id)}
            <div
              class="flex items-center gap-2 rounded-lg px-2.5 sm:px-3 py-1.5 text-xs transition cursor-pointer border shrink-0 {tab.active && activeNav === 'terminal' ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/30 font-medium' : 'bg-[#14141a] text-neutral-400 border-[#20202a] hover:text-neutral-200'}"
              onclick={() => {
                terminalStore.selectTab(tab.id);
                activeNav = 'terminal';
              }}
              role="button"
              tabindex="0"
              onkeydown={(e) => e.key === 'Enter' && terminalStore.selectTab(tab.id)}
            >
              <span class="h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
              <span class="max-w-[100px] sm:max-w-[140px] truncate">{tab.label}</span>
              <button
                class="text-neutral-500 hover:text-rose-400 p-0.5 rounded"
                onclick={(e) => {
                  e.stopPropagation();
                  terminalStore.closeTab(tab.id);
                  if (terminalStore.tabCount === 0) {
                    activeNav = 'hosts';
                  }
                }}
                aria-label="Close session"
              >
                <X class="h-3 w-3" />
              </button>
            </div>
          {/each}

          <button
            onclick={openQuickConnect}
            class="flex items-center justify-center h-7 w-7 rounded-lg border border-[#20202a] bg-[#14141a] text-neutral-400 hover:text-white hover:border-[#30303e] transition shrink-0"
            title="New Connection Tab (⌘T / Ctrl+T)"
            aria-label="New Connection Tab"
          >
            <Plus class="h-3.5 w-3.5" />
          </button>
        </div>
      </header>
    {/if}

    <!-- Dynamic Screen Content -->
    <div class="flex-1 min-h-0 overflow-hidden bg-[#0a0a0c]">
      {#if !isAuthenticated}
        <!-- Top macOS drag region on auth/mode screen -->
        <div data-tauri-drag-region class="fixed top-0 left-0 right-0 h-8 z-40"></div>
        <!-- Initial Setup / Connection Mode Screen -->
        <div class="flex h-full items-center justify-center p-6 overflow-y-auto">
          {#if connectionChoice === 'select'}
            <!-- Mode Selection Cards: Connect to Gateway vs Standalone -->
            <div class="w-full max-w-lg rounded-2xl border border-[#252535] bg-[#12121a] p-8 shadow-2xl space-y-6 animate-in fade-in zoom-in-95 duration-150">
              <div class="text-center space-y-2">
                <div class="flex h-12 w-12 items-center justify-center rounded-2xl bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 mx-auto mb-2 shadow-inner">
                  <Server class="h-6 w-6" />
                </div>
                <h2 class="text-lg font-bold text-white tracking-tight">OpenTermius</h2>
                <p class="text-xs text-neutral-400">
                  Select workspace mode
                </p>
              </div>

              {#if authError}
                <div class="rounded-xl bg-rose-950/40 border border-rose-500/30 p-3 text-rose-300 text-xs flex items-center gap-2">
                  <AlertCircle class="h-4 w-4 shrink-0 text-rose-400" />
                  <span>{authError}</span>
                </div>
              {/if}

              <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                <!-- Connect to Gateway Card -->
                <button
                  type="button"
                  onclick={() => (connectionChoice = 'gateway')}
                  class="group relative rounded-xl border border-[#252538] bg-[#151522] hover:border-emerald-500/60 hover:bg-[#181829] p-5 text-left transition duration-150 flex flex-col justify-between space-y-4 shadow-sm"
                >
                  <div class="space-y-3">
                    <div class="flex items-center justify-between">
                      <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 group-hover:scale-105 transition">
                        <Radio class="h-5 w-5" />
                      </div>
                      <span class="rounded bg-emerald-500/15 px-2 py-0.5 text-[10px] font-semibold text-emerald-400">
                        Sync
                      </span>
                    </div>
                    <div>
                      <h3 class="text-sm font-bold text-white group-hover:text-emerald-300 transition">
                        Gateway
                      </h3>
                      <p class="text-[11px] text-neutral-400 mt-1 leading-relaxed">
                        Sync vaults with team or self-hosted server.
                      </p>
                    </div>
                  </div>
                  <div class="flex items-center gap-1.5 text-xs text-emerald-400 font-medium pt-2 border-t border-[#222233]">
                    <span>Connect</span>
                    <ArrowRight class="h-3.5 w-3.5 group-hover:translate-x-1 transition" />
                  </div>
                </button>

                <!-- Standalone Mode Card -->
                <button
                  type="button"
                  onclick={handleStartStandalone}
                  disabled={isAuthLoading}
                  class="group relative rounded-xl border border-[#252538] bg-[#151522] hover:border-indigo-500/60 hover:bg-[#181829] p-5 text-left transition duration-150 flex flex-col justify-between space-y-4 shadow-sm"
                >
                  <div class="space-y-3">
                    <div class="flex items-center justify-between">
                      <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-indigo-500/10 text-indigo-400 border border-indigo-500/20 group-hover:scale-105 transition">
                        <Laptop class="h-5 w-5" />
                      </div>
                      <span class="rounded bg-indigo-500/15 px-2 py-0.5 text-[10px] font-semibold text-indigo-400">
                        Local
                      </span>
                    </div>
                    <div>
                      <h3 class="text-sm font-bold text-white group-hover:text-indigo-300 transition">
                        Standalone
                      </h3>
                      <p class="text-[11px] text-neutral-400 mt-1 leading-relaxed">
                        Store vaults locally on this device.
                      </p>
                    </div>
                  </div>
                  <div class="flex items-center gap-1.5 text-xs text-indigo-400 font-medium pt-2 border-t border-[#222233]">
                    <span>{isAuthLoading ? 'Opening...' : 'Continue'}</span>
                    <ArrowRight class="h-3.5 w-3.5 group-hover:translate-x-1 transition" />
                  </div>
                </button>
              </div>

              <p class="text-[11px] text-center text-neutral-500">
                You can switch between Standalone and Gateway anytime in Settings.
              </p>
            </div>
          {:else if connectionChoice === 'gateway'}
            <!-- Gateway Configuration Form -->
            <div class="w-full max-w-md rounded-2xl border border-[#252535] bg-[#12121a] p-6 shadow-2xl space-y-5 animate-in fade-in zoom-in-95 duration-150 text-xs">
              <div class="flex items-center justify-between border-b border-[#222230] pb-3">
                <div class="flex items-center gap-3">
                  <div class="flex h-9 w-9 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                    <Radio class="h-4.5 w-4.5" />
                  </div>
                  <div>
                    <h2 class="text-sm font-bold text-white">Connect to Sync Gateway</h2>
                    <p class="text-[11px] text-neutral-400">Self-Hosted Gateway</p>
                  </div>
                </div>
                <button
                  type="button"
                  onclick={() => (connectionChoice = 'select')}
                  class="flex items-center gap-1 text-[11px] text-neutral-400 hover:text-white transition"
                >
                  <ArrowLeft class="h-3 w-3" /> Back
                </button>
              </div>

              {#if authError}
                <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
                  {authError}
                </div>
              {/if}

              <div class="space-y-3.5">
                <div>
                  <label for="gw-api-url" class="block font-medium text-neutral-300 mb-1">
                    Gateway API URL
                  </label>
                  <input
                    id="gw-api-url"
                    type="text"
                    bind:value={gatewayApiUrl}
                    placeholder="http://192.168.1.50:8080/api/v1"
                    class="w-full rounded-lg border border-[#262635] bg-[#0c0c12] px-3 py-2 text-white font-mono text-[11px] outline-none focus:border-emerald-500"
                  />
                </div>

                <div class="pt-2 border-t border-[#1e1e2a] grid grid-cols-1 gap-3">
                  <div>
                    <label for="auth-email" class="block font-medium text-neutral-300 mb-1">Account Email</label>
                    <input
                      id="auth-email"
                      type="email"
                      bind:value={authEmail}
                      placeholder="admin@opentermius.local"
                      class="w-full rounded-lg border border-[#262635] bg-[#0c0c12] px-3 py-1.5 text-white outline-none focus:border-emerald-500"
                    />
                  </div>
                  <div>
                    <label for="auth-pw" class="block font-medium text-neutral-300 mb-1">Master Password</label>
                    <div class="relative">
                      <input
                        id="auth-pw"
                        type={showAuthPassword ? 'text' : 'password'}
                        bind:value={authPassword}
                        placeholder="••••••••••••"
                        class="w-full rounded-lg border border-[#262635] bg-[#0c0c12] pl-3 pr-9 py-1.5 text-white outline-none focus:border-emerald-500"
                      />
                      <button
                        type="button"
                        onclick={() => (showAuthPassword = !showAuthPassword)}
                        class="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1 text-neutral-400 hover:text-white transition cursor-pointer"
                        aria-label={showAuthPassword ? 'Hide password' : 'Show password'}
                        title={showAuthPassword ? 'Hide password' : 'Show password'}
                      >
                        {#if showAuthPassword}
                          <EyeOff class="h-3.5 w-3.5" />
                        {:else}
                          <Eye class="h-3.5 w-3.5" />
                        {/if}
                      </button>
                    </div>
                  </div>
                </div>

                <p class="text-[11px] text-neutral-400">
                  Need an account? Register through your Gateway Console.
                </p>

                <div class="pt-3 flex items-center justify-between gap-2 border-t border-[#1e1e2a]">
                  <button
                    type="button"
                    onclick={() => (connectionChoice = 'select')}
                    class="px-3 py-2 rounded-lg border border-[#252535] text-neutral-400 hover:text-white transition"
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    onclick={handleAuthSubmit}
                    disabled={isAuthLoading}
                    class="rounded-lg bg-emerald-600 px-4 py-2 font-semibold text-white hover:bg-emerald-500 transition shadow disabled:opacity-50"
                  >
                    {isAuthLoading ? 'Connecting...' : 'Connect Gateway'}
                  </button>
                </div>
              </div>
            </div>
          {/if}
        </div>
      {:else}
        <!-- Hosts Inventory Screen (Persistent in DOM) -->
        <div class="h-full w-full flex-col px-3.5 pt-3.5 pb-24 sm:px-6 sm:py-5 overflow-y-auto space-y-4 {activeNav === 'hosts' ? 'flex' : 'hidden'}">
          <!-- Clean Search & Action Toolbar -->
          <div class="space-y-3">
            <div class="flex items-center gap-2 sm:gap-2.5">
              <div class="relative flex-1 max-w-md">
                <Search class="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-zinc-500" />
                <input
                  type="text"
                  bind:value={searchQuery}
                  autocapitalize="none"
                  autocorrect="off"
                  spellcheck={false}
                  placeholder="Search hosts, IP, or tags..."
                  class="w-full rounded-xl border border-white/[0.08] bg-[#121218] pl-9 pr-9 py-2.5 sm:py-2 text-sm sm:text-xs text-white placeholder-zinc-500 outline-none focus:border-emerald-500/60 transition"
                />
                {#if searchQuery}
                  <button
                    type="button"
                    onclick={() => (searchQuery = '')}
                    class="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-lg p-1 text-zinc-400 hover:text-white"
                    aria-label="Clear search"
                  >
                    <X class="h-3.5 w-3.5" />
                  </button>
                {/if}
              </div>

              <div class="flex items-center gap-1.5 sm:gap-2 shrink-0 ml-auto">
                <span class="hidden md:inline text-[11px] font-mono text-zinc-500 mr-1">
                  {filteredHosts.length} {filteredHosts.length === 1 ? 'host' : 'hosts'}
                </span>
                <button
                  type="button"
                  onclick={loadHosts}
                  class="flex h-10 w-10 sm:h-8 sm:w-8 items-center justify-center rounded-xl sm:rounded-lg border border-white/[0.07] bg-[#14141b] text-zinc-400 hover:text-white hover:border-white/[0.14] active:scale-95 transition cursor-pointer"
                  title="Refresh hosts"
                  aria-label="Refresh hosts"
                >
                  <RefreshCw class="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                </button>
                {#if !platform.isMobile}
                  <button
                    type="button"
                    onclick={() => {
                      editingHost = null;
                      isAddHostOpen = true;
                    }}
                    class="flex items-center gap-1.5 rounded-lg bg-emerald-600 px-3 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500 active:scale-95 transition cursor-pointer"
                  >
                    <Plus class="h-3.5 w-3.5" />
                    <span>New Host</span>
                  </button>
                {/if}
              </div>
            </div>

            <!-- Folder Filter Pills (only shown when >1 folder exists) -->
            {#if availableFolders.length > 1}
              <div class="flex items-center gap-1.5 overflow-x-auto no-scrollbar pb-0.5 text-xs select-none">
                <button
                  type="button"
                  onclick={() => (selectedFolderFilter = 'all')}
                  class="rounded-full px-3 py-1 transition flex items-center gap-1.5 shrink-0 cursor-pointer {selectedFolderFilter === 'all' ? 'bg-emerald-500/15 border border-emerald-500/35 text-emerald-300 font-medium' : 'bg-[#131319] border border-white/[0.06] text-zinc-400 hover:text-zinc-200'}"
                >
                  <span>All</span>
                  <span class="text-[10px] font-mono opacity-75">{hosts.length}</span>
                </button>
                {#each availableFolders as folder}
                  {@const count = hosts.filter((h) => (h.group || 'Default') === folder).length}
                  <button
                    type="button"
                    onclick={() => (selectedFolderFilter = folder)}
                    class="rounded-full px-3 py-1 transition flex items-center gap-1.5 shrink-0 cursor-pointer {selectedFolderFilter === folder ? 'bg-emerald-500/15 border border-emerald-500/35 text-emerald-300 font-medium' : 'bg-[#131319] border border-white/[0.06] text-zinc-400 hover:text-zinc-200'}"
                  >
                    <Folder class="h-3 w-3 opacity-75" />
                    <span>{folder}</span>
                    <span class="text-[10px] font-mono opacity-75">{count}</span>
                  </button>
                {/each}
              </div>
            {/if}

            <!-- Live Team Sessions (Collapsible, collapsed by default) -->
            {#if activeVault?.type === 'team' && gatewayOnline}
              {@const activeTeamPeers = teamPresence.filter((p) => p.online && p.active_sessions.length > 0 && p.device_id !== api.getDeviceId())}
              {@const totalLiveSessions = activeTeamPeers.reduce((n, p) => n + p.active_sessions.length, 0)}
              {#if activeTeamPeers.length > 0}
                <div class="rounded-xl border border-white/[0.08] bg-[#131317] overflow-hidden">
                  <button
                    type="button"
                    onclick={() => (isTeamLiveExpanded = !isTeamLiveExpanded)}
                    class="flex w-full items-center justify-between px-3 py-2 text-left hover:bg-white/[0.03] transition cursor-pointer"
                  >
                    <span class="flex items-center gap-2 text-[11px] font-mono font-semibold text-emerald-400 uppercase tracking-wider">
                      <span class="h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse"></span>
                      Team Live SSH ({totalLiveSessions})
                    </span>
                    <span class="flex items-center gap-1 text-[11px] text-neutral-400">
                      <span>{isTeamLiveExpanded ? 'Hide' : 'Expand'}</span>
                      {#if isTeamLiveExpanded}
                        <ChevronDown class="h-3.5 w-3.5" />
                      {:else}
                        <ChevronRight class="h-3.5 w-3.5" />
                      {/if}
                    </span>
                  </button>
                  {#if isTeamLiveExpanded}
                    <div class="border-t border-white/[0.06] p-2.5 space-y-1.5">
                      {#each activeTeamPeers as peer (peer.user_id + peer.device_id)}
                        {#each peer.active_sessions as sess (sess.session_id)}
                          <div class="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-[#0e0e11] border border-white/[0.06] px-3 py-2 text-xs">
                            <div class="flex items-center gap-2.5 min-w-0">
                              <div class="flex -space-x-1.5 shrink-0">
                                {#if sess.viewers && sess.viewers.length > 0}
                                  {#each sess.viewers as v (v.user_id + v.mode)}
                                    <span
                                      title="{v.user_name} ({v.mode === 'host' ? 'Host' : v.mode === 'control' ? 'Control' : 'View'})"
                                      class="inline-flex h-6 w-6 items-center justify-center rounded-full text-[10px] font-bold ring-2 ring-[#0e0e11] {getAvatarTone(v.user_id || v.user_name)}"
                                    >
                                      {getAvatarInitials(v.user_name)}
                                    </span>
                                  {/each}
                                {:else}
                                  <span
                                    title="{peer.user_name} (Host)"
                                    class="inline-flex h-6 w-6 items-center justify-center rounded-full text-[10px] font-bold ring-2 ring-[#0e0e11] {getAvatarTone(peer.user_id || peer.user_name)}"
                                  >
                                    {getAvatarInitials(peer.user_name)}
                                  </span>
                                {/if}
                              </div>
                              <div class="min-w-0">
                                <p class="font-semibold text-zinc-100 truncate">{sess.host_label}</p>
                                <p class="text-[11px] text-zinc-400 font-mono truncate">
                                  {peer.user_name} · {peer.platform}
                                  {#if sess.viewers && sess.viewers.length > 1}
                                    · {sess.viewers.length} connected
                                  {/if}
                                </p>
                              </div>
                            </div>
                            <div class="w-full sm:w-auto grid grid-cols-2 sm:flex items-center gap-1.5 shrink-0">
                              {#if sess.approved_request_id}
                                <button
                                  type="button"
                                  onclick={() =>
                                    handleRejoinRemote(
                                      sess.session_id,
                                      sess.vault_id,
                                      sess.host_label,
                                      sess.approved_request_id!,
                                      sess.approved_mode || 'view',
                                      peer.user_name
                                    )}
                                  class="col-span-2 w-full sm:w-auto rounded-lg bg-emerald-600 px-3 py-2 sm:py-1.5 text-[11px] font-semibold text-white hover:bg-emerald-500 active:scale-[0.99] transition"
                                >
                                  Rejoin ({sess.approved_mode === 'control' ? 'Control' : 'View'})
                                </button>
                              {:else}
                                <button
                                  type="button"
                                  disabled={Boolean(pendingOutboundRequestId)}
                                  onclick={() => handleRequestRemote(sess.session_id, sess.host_label, peer.user_name, 'view')}
                                  class="rounded-lg bg-white/[0.05] border border-white/[0.08] px-2.5 py-2 sm:py-1.5 text-[11px] font-medium text-zinc-200 hover:bg-white/[0.09] disabled:opacity-50"
                                >
                                  Request View
                                </button>
                                <button
                                  type="button"
                                  disabled={Boolean(pendingOutboundRequestId)}
                                  onclick={() => handleRequestRemote(sess.session_id, sess.host_label, peer.user_name, 'control')}
                                  class="rounded-lg bg-emerald-600 px-2.5 py-2 sm:py-1.5 text-[11px] font-semibold text-white hover:bg-emerald-500 disabled:opacity-50"
                                >
                                  Request Control
                                </button>
                              {/if}
                            </div>
                          </div>
                        {/each}
                      {/each}
                    </div>
                  {/if}
                </div>
              {/if}
            {/if}
          </div>

          <!-- 4 UI States for Hosts -->
          {#if loadState === 'loading'}
            <div class="flex h-48 items-center justify-center text-xs text-neutral-400 gap-2">
              <RefreshCw class="h-4 w-4 animate-spin text-emerald-400" />
              <span>Loading hosts...</span>
            </div>
          {:else if loadState === 'error'}
            <div class="rounded-xl border border-rose-500/30 bg-rose-950/20 p-5 flex items-center justify-between">
              <div class="flex items-center gap-3">
                <AlertCircle class="h-5 w-5 text-rose-400 shrink-0" />
                <div>
                  <p class="text-xs font-semibold text-rose-300">Backend Communication Error</p>
                  <p class="text-[11px] text-rose-400/80">{errorMessage}</p>
                </div>
              </div>
              <button
                onclick={checkAuthAndLoadData}
                class="rounded-lg bg-rose-500/20 px-3 py-1.5 text-xs font-semibold text-rose-200 hover:bg-rose-500/30"
              >
                Retry Connection
              </button>
            </div>
          {:else if loadState === 'empty' || filteredHosts.length === 0}
            <div class="flex flex-col items-center justify-center p-12 text-center rounded-xl border border-[#22222a] bg-[#14141a]">
              <Server class="h-10 w-10 text-neutral-600 mb-3" />
              <p class="text-sm font-semibold text-white">No hosts</p>
              <p class="text-xs text-neutral-400 mt-1">
                Add an SSH host to start connecting.
              </p>
              <button
                onclick={() => {
                  editingHost = null;
                  isAddHostOpen = true;
                }}
                class="mt-4 rounded-lg bg-emerald-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500"
              >
                Add Host
              </button>
            </div>
          {:else}
            <!-- Clean Flat Host Grid (single Default folder) or Borderless Collapsible Folder Sections -->
            {@const folderEntries = Object.entries(hostsByFolder)}
            {#if folderEntries.length === 1 && folderEntries[0][0] === 'Default'}
              <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 gap-2.5 sm:gap-3">
                {#each folderEntries[0][1] as host (host.id)}
                  <HostCard
                    {host}
                    onConnect={handleConnect}
                    onEdit={handleEditHost}
                    onDelete={handleDeleteHost}
                    onMove={handleOpenMoveModal}
                    onOpenSftp={handleOpenSftp}
                  />
                {/each}
              </div>
            {:else}
              <div class="space-y-5">
                {#each folderEntries as [folderName, folderHosts] (folderName)}
                  <section class="space-y-2.5">
                    <!-- Clean Borderless Section Header -->
                    <div class="flex items-center justify-between border-b border-white/[0.06] pb-2">
                      <button
                        type="button"
                        onclick={() => toggleFolder(folderName)}
                        class="flex items-center gap-2 text-left text-zinc-200 hover:text-white transition cursor-pointer"
                      >
                        {#if collapsedFolders[folderName]}
                          <ChevronRight class="h-3.5 w-3.5 text-zinc-500" />
                        {:else}
                          <ChevronDown class="h-3.5 w-3.5 text-emerald-400" />
                        {/if}
                        <Folder class="h-3.5 w-3.5 text-zinc-400" />
                        <span class="text-xs font-semibold tracking-wide text-zinc-100">{folderName}</span>
                        <span class="text-[11px] font-mono text-zinc-500">{folderHosts.length}</span>
                      </button>

                      <button
                        type="button"
                        onclick={() => {
                          editingHost = { group: folderName };
                          isAddHostOpen = true;
                        }}
                        class="flex items-center gap-1 rounded-md px-2 py-1 text-[11px] text-zinc-400 hover:text-white hover:bg-white/[0.05] transition cursor-pointer"
                        title="Add host to this folder"
                      >
                        <Plus class="h-3 w-3" />
                        <span class="hidden sm:inline">Add</span>
                      </button>
                    </div>

                    {#if !collapsedFolders[folderName]}
                      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 gap-2.5 sm:gap-3 animate-in fade-in duration-100">
                        {#each folderHosts as host (host.id)}
                          <HostCard
                            {host}
                            onConnect={handleConnect}
                            onEdit={handleEditHost}
                            onDelete={handleDeleteHost}
                            onMove={handleOpenMoveModal}
                            onOpenSftp={handleOpenSftp}
                          />
                        {/each}
                      </div>
                    {/if}
                  </section>
                {/each}
              </div>
            {/if}
          {/if}
        </div>

        <!-- Live Terminal Screen (Persistent in DOM with Split Support) -->
        <div class="h-full w-full {activeNav === 'terminal' ? 'block' : 'hidden'}">
          {#if terminalStore.tabCount > 0}
            <div class="relative h-full w-full flex {terminalStore.splitTab ? (terminalStore.splitDirection === 'horizontal' ? 'flex-col md:flex-row' : 'flex-col') : ''}">
              <!-- Primary Terminal Pane -->
              <div class="relative flex-1 min-w-0 min-h-0 {terminalStore.splitTab ? (terminalStore.splitDirection === 'horizontal' ? 'border-b md:border-b-0 md:border-r border-[#1f1f2e]' : 'border-b border-[#1f1f2e]') : ''}">
                {#each terminalStore.tabs as tab (tab.id)}
                  <div
                    class="absolute inset-0 h-full w-full"
                    style={tab.id === terminalStore.activeTabId ? 'display: block;' : 'display: none;'}
                  >
                    <TerminalView
                      sessionId={tab.id}
                      title={tab.label}
                      hostId={tab.hostId}
                      config={tab.config}
                      remoteConfig={tab.remoteConfig}
                      active={activeNav === 'terminal' && tab.id === terminalStore.activeTabId}
                      isFocusMode={platform.isPhone && isTerminalFocus}
                      onToggleFocus={platform.isPhone ? () => (isTerminalFocus = !isTerminalFocus) : undefined}
                      onSplitRight={() => handleSplitTerminal('horizontal')}
                      onSplitDown={() => handleSplitTerminal('vertical')}
                    />
                  </div>
                {/each}
              </div>

              <!-- Secondary Split Pane (if active) -->
              {#if terminalStore.splitTab}
                <div class="relative flex-1 min-w-0 min-h-0">
                  <TerminalView
                    sessionId={terminalStore.splitTab.id}
                    title={terminalStore.splitTab.label}
                    hostId={terminalStore.splitTab.hostId}
                    config={terminalStore.splitTab.config}
                    active={activeNav === 'terminal'}
                    isSplitPane={true}
                    onClosePane={() => terminalStore.closeSplit()}
                  />
                </div>
              {/if}
            </div>
          {:else}
            <div class="flex h-full flex-col items-center justify-center text-center p-8 space-y-4">
              <div class="flex h-16 w-16 items-center justify-center rounded-2xl bg-[#14141d] border border-[#22222a] text-neutral-400">
                <Server class="h-8 w-8 text-neutral-500" />
              </div>
              <div>
                <h3 class="text-base font-semibold text-white">No Active Session</h3>
                <p class="text-xs text-neutral-400 mt-1">
                  Select a host to connect.
                </p>
              </div>
              <button
                onclick={() => (activeNav = 'hosts')}
                class="rounded-lg bg-emerald-600 px-4 py-2 text-xs font-semibold text-white shadow hover:bg-emerald-500 transition"
              >
                View Hosts
              </button>
            </div>
          {/if}
        </div>

        <!-- Other Views -->
        {#if activeNav === 'sftp'}
          <SftpView {hosts} initialHostId={sftpSelectedHostId} />
        {:else if activeNav === 'tunnels'}
          <TunnelsView activeVaultId={activeVault?.id} {hosts} />
        {:else if activeNav === 'keychain'}
        <KeychainView activeVaultId={activeVault?.id} />
      {:else if activeNav === 'identities'}
        <IdentitiesView activeVaultId={activeVault?.id} />
      {:else if activeNav === 'vaults'}
        <VaultsView
          {activeVault}
          onSelectVault={handleSelectVault}
          onVaultsUpdated={(updated) => (vaults = updated)}
        />
      {:else if activeNav === 'snippets'}
        <SnippetsView
          activeVaultId={activeVault?.id}
          onRunSnippet={(script) => {
            if (terminalStore.tabCount === 0) {
              handleNewLocalTab();
            } else {
              activeNav = 'terminal';
            }
            setTimeout(() => {
              window.dispatchEvent(new CustomEvent('opentermius:run-snippet', { detail: { script } }));
            }, 120);
          }}
        />
      {:else if activeNav === 'logs'}
        <SessionLogsView />
      {:else if activeNav === 'more'}
        {@const moreItems = [
          { id: 'keychain', label: 'Keychain', desc: 'SSH keys & certificates', icon: Key },
          { id: 'identities', label: 'Identities', desc: 'Saved usernames & profiles', icon: UserCheck },
          { id: 'snippets', label: 'Snippets', desc: 'Reusable shell commands', icon: FileCode2 },
          { id: 'vaults', label: 'Vaults', desc: 'Personal & team vaults', icon: FolderLock },
          { id: 'logs', label: 'Session Logs', desc: 'Terminal history & audit trail', icon: ScrollText },
          { id: 'settings', label: 'Settings', desc: 'Gateway URL & workspace mode', icon: Settings }
        ]}
        <div class="h-full overflow-y-auto p-4 space-y-5">
          {#if vaults.length > 0}
            <div class="rounded-2xl border border-[#22222c] bg-[#121218] p-3.5 space-y-2.5">
              <div class="flex items-center justify-between">
                <span class="text-[11px] font-semibold uppercase tracking-wider text-neutral-400">Active Vault</span>
                <span class="text-[11px] text-emerald-400 font-medium">{api.isStandalone() ? 'Standalone' : 'Gateway'}</span>
              </div>
              <div class="flex gap-2 overflow-x-auto no-scrollbar pb-0.5">
                {#each vaults as v (v.id)}
                  <button
                    type="button"
                    onclick={() => handleSelectVault(v)}
                    class="flex items-center gap-2 rounded-xl border px-3 py-2 text-xs font-medium shrink-0 transition active:scale-95 {activeVault?.id === v.id ? 'border-emerald-500/40 bg-emerald-500/15 text-emerald-300' : 'border-[#252532] bg-[#181822] text-neutral-300'}"
                  >
                    <FolderLock class="h-3.5 w-3.5 shrink-0" />
                    <span>{v.name}</span>
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          <div class="rounded-2xl border border-[#22222c] bg-[#121218] divide-y divide-[#1c1c26] overflow-hidden">
            {#each moreItems as item (item.id)}
              <button
                type="button"
                onclick={() => (activeNav = item.id)}
                class="flex w-full items-center justify-between px-4 py-3.5 text-left active:bg-[#1a1a24] transition"
              >
                <div class="flex items-center gap-3.5 min-w-0">
                  <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-[#1a1a24] text-emerald-400 shrink-0">
                    <item.icon class="h-5 w-5" />
                  </div>
                  <div class="min-w-0">
                    <div class="text-sm font-medium text-white">{item.label}</div>
                    <div class="text-xs text-neutral-400 truncate">{item.desc}</div>
                  </div>
                </div>
                <ChevronRight class="h-4 w-4 text-neutral-500 shrink-0" />
              </button>
            {/each}
          </div>
        </div>
      {:else if activeNav === 'settings'}
        <div class="h-full overflow-y-auto p-6 space-y-6 max-w-2xl">
          <h2 class="text-lg font-bold text-white">Settings & Preferences</h2>
          <div class="rounded-xl border border-[#22222a] bg-[#14141a] p-5 space-y-4 text-xs">
            <!-- Mode Status -->
            <div class="flex items-center justify-between p-3 rounded-lg bg-[#0e0e14] border border-[#20202d]">
              <div>
                <p class="font-semibold text-white">Current Mode</p>
                <p class="text-[11px] text-neutral-400 mt-0.5">
                  {api.isStandalone() ? 'Offline local storage' : 'Connected to Gateway'}
                </p>
              </div>
              {#if api.isStandalone()}
                <button
                  type="button"
                  onclick={() => {
                    api.clearToken();
                    isAuthenticated = false;
                    connectionChoice = 'gateway';
                  }}
                  class="px-3 py-1.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-medium text-xs transition"
                >
                  Connect to Gateway
                </button>
              {:else}
                <span class="rounded bg-emerald-500/15 border border-emerald-500/30 px-2 py-1 text-[10px] font-semibold text-emerald-400">
                  Gateway Active
                </span>
              {/if}
            </div>

            <div>
              <label for="cfg-backend-url" class="block font-medium text-neutral-300 mb-1">Self-Hosted Gateway URL</label>
              <input
                id="cfg-backend-url"
                type="url"
                autocapitalize="none"
                autocorrect="off"
                spellcheck={false}
                bind:value={gatewayApiUrl}
                onchange={() => api.setGateway(gatewayApiUrl)}
                class="w-full rounded-lg border border-[#252535] bg-[#0d0d12] px-3 py-2 font-mono text-neutral-200 outline-none focus:border-emerald-500"
              />
            </div>

            <!-- Import Hosts (moved from top bar) -->
            <div class="flex items-center justify-between p-3 rounded-lg bg-[#0e0e14] border border-[#20202d]">
              <div>
                <p class="font-semibold text-white">Import Hosts</p>
                <p class="text-[11px] text-neutral-400 mt-0.5">
                  Import hosts from OpenSSH config (~/.ssh/config), CSV, or JSON.
                </p>
              </div>
              <button
                type="button"
                onclick={() => (isImportOpen = true)}
                class="flex items-center gap-1.5 rounded-lg border border-[#282838] bg-[#161620] px-3 py-1.5 text-xs font-medium text-neutral-200 hover:text-white hover:border-[#38384d] transition cursor-pointer shrink-0"
              >
                <Download class="h-3.5 w-3.5 text-emerald-400" />
                <span>Import Hosts</span>
              </button>
            </div>
            <div class="pt-2 border-t border-[#20202c] flex items-center justify-between gap-2">
              <div class="flex items-center gap-2.5">
                <span class="text-neutral-400 font-mono text-[11px]">OpenTermius v{APP_VERSION}</span>
                <button
                  type="button"
                  disabled={isCheckingUpdate}
                  onclick={() => checkDesktopUpdates(true)}
                  class="inline-flex items-center gap-1 rounded-md border border-[#282838] bg-[#161620] px-2 py-1 text-[11px] font-medium text-emerald-400 hover:bg-[#1e1e2c] disabled:opacity-50 transition"
                >
                  <RefreshCw class="h-3 w-3 {isCheckingUpdate ? 'animate-spin' : ''}" />
                  <span>{isCheckingUpdate ? 'Checking...' : 'Check for Updates'}</span>
                </button>
              </div>
              <button
                type="button"
                onclick={handleSignOut}
                class="text-rose-400 hover:underline text-xs"
              >
                {api.isStandalone() ? 'Exit Standalone' : 'Disconnect'}
              </button>
            </div>
          </div>
        </div>
        {/if}
      {/if}
    </div>

    <!-- Mobile Bottom Navigation Bar (Flex child at bottom of main — never overlaps terminal/special keys) -->
    {#if isAuthenticated && platform.isMobile && !hideMobileBottomNav}
      {@const isMoreActive = ['more', 'vaults', 'keychain', 'identities', 'snippets', 'logs', 'settings'].includes(activeNav)}
      <nav
        class="shrink-0 min-h-16 bg-[#0e0e14]/95 backdrop-blur-md border-t border-[#1f1f2a] flex items-center justify-around z-30 px-1 select-none"
        style="padding-bottom: env(safe-area-inset-bottom);"
      >
        <button
          type="button"
          onclick={() => (activeNav = 'hosts')}
          class="flex flex-col items-center justify-center flex-1 py-1 {activeNav === 'hosts' ? 'text-emerald-400 font-medium' : 'text-neutral-400'}"
        >
          <div class="px-4 py-1 rounded-full transition {activeNav === 'hosts' ? 'bg-emerald-500/20' : ''}">
            <Server class="h-4.5 w-4.5" />
          </div>
          <span class="text-[11px] mt-0.5">Hosts</span>
        </button>

        <button
          type="button"
          onclick={() => (activeNav = 'terminal')}
          class="relative flex flex-col items-center justify-center flex-1 py-1 {activeNav === 'terminal' ? 'text-emerald-400 font-medium' : 'text-neutral-400'}"
        >
          <div class="relative px-4 py-1 rounded-full transition {activeNav === 'terminal' ? 'bg-emerald-500/20' : ''}">
            <Terminal class="h-4.5 w-4.5" />
            {#if terminalStore.tabCount > 0}
              <span class="absolute -top-0.5 right-1.5 h-4 min-w-[16px] px-1 rounded-full bg-emerald-500 text-black text-[9px] font-bold flex items-center justify-center">
                {terminalStore.tabCount}
              </span>
            {/if}
          </div>
          <span class="text-[11px] mt-0.5">Terminal</span>
        </button>

        <button
          type="button"
          onclick={() => (activeNav = 'sftp')}
          class="flex flex-col items-center justify-center flex-1 py-1 {activeNav === 'sftp' ? 'text-emerald-400 font-medium' : 'text-neutral-400'}"
        >
          <div class="px-4 py-1 rounded-full transition {activeNav === 'sftp' ? 'bg-emerald-500/20' : ''}">
            <Folder class="h-4.5 w-4.5" />
          </div>
          <span class="text-[11px] mt-0.5">SFTP</span>
        </button>

        <button
          type="button"
          onclick={() => (activeNav = 'tunnels')}
          class="flex flex-col items-center justify-center flex-1 py-1 {activeNav === 'tunnels' ? 'text-emerald-400 font-medium' : 'text-neutral-400'}"
        >
          <div class="px-4 py-1 rounded-full transition {activeNav === 'tunnels' ? 'bg-emerald-500/20' : ''}">
            <Network class="h-4.5 w-4.5" />
          </div>
          <span class="text-[11px] mt-0.5">Tunnels</span>
        </button>

        <button
          type="button"
          onclick={() => (activeNav = 'more')}
          class="flex flex-col items-center justify-center flex-1 py-1 {isMoreActive ? 'text-emerald-400 font-medium' : 'text-neutral-400'}"
        >
          <div class="px-4 py-1 rounded-full transition {isMoreActive ? 'bg-emerald-500/20' : ''}">
            <LayoutGrid class="h-4.5 w-4.5" />
          </div>
          <span class="text-[11px] mt-0.5">More</span>
        </button>
      </nav>
    {/if}
  </main>

  <!-- Mobile Floating Action Button (FAB) on Hosts screen -->
  {#if isAuthenticated && platform.isMobile && activeNav === 'hosts'}
    <button
      type="button"
      onclick={() => {
        editingHost = null;
        isAddHostOpen = true;
      }}
      class="fixed bottom-[calc(5rem+var(--sab))] right-4 z-30 flex h-14 w-14 items-center justify-center rounded-2xl bg-emerald-500 text-black shadow-lg shadow-emerald-500/25 active:scale-95 transition"
      aria-label="Add new host"
    >
      <Plus class="h-6 w-6 stroke-[2.5]" />
    </button>
  {/if}

  <!-- Mobile Vault Switcher Bottom Sheet -->
  {#if platform.isMobile && isVaultSwitcherOpen}
    <div
      class="fixed inset-0 z-50 flex flex-col justify-end bg-black/70 backdrop-blur-xs"
      onclick={() => (isVaultSwitcherOpen = false)}
      role="presentation"
    >
      <div
        class="rounded-t-3xl border-t border-[#262635] bg-[#12121a] px-4 pt-3 pb-[calc(1.5rem+var(--sab))] space-y-3 shadow-2xl"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => { if (e.key === 'Escape') isVaultSwitcherOpen = false; }}
        role="dialog"
        tabindex="-1"
        aria-label="Switch Vault"
      >
        <div class="mx-auto h-1 w-10 rounded-full bg-neutral-700"></div>
        <div class="flex items-center justify-between pt-1">
          <h2 class="text-sm font-semibold text-white">Switch Vault</h2>
          <button
            type="button"
            onclick={() => (isVaultSwitcherOpen = false)}
            class="rounded-lg p-1.5 text-neutral-400 active:bg-[#1e1e2c]"
            aria-label="Close vault switcher"
          >
            <X class="h-4 w-4" />
          </button>
        </div>

        <div class="max-h-64 overflow-y-auto space-y-2">
          {#each vaults as v (v.id)}
            <button
              type="button"
              onclick={() => {
                handleSelectVault(v);
                isVaultSwitcherOpen = false;
              }}
              class="flex w-full items-center justify-between rounded-2xl border px-3.5 py-3 text-left transition {activeVault?.id === v.id ? 'border-emerald-500/40 bg-emerald-500/10 text-white' : 'border-[#222230] bg-[#171722] text-neutral-300'}"
            >
              <div class="flex items-center gap-3 min-w-0">
                <FolderLock class="h-4 w-4 {activeVault?.id === v.id ? 'text-emerald-400' : 'text-neutral-400'} shrink-0" />
                <div class="min-w-0">
                  <div class="text-sm font-medium truncate">{v.name}</div>
                  <div class="text-[11px] text-neutral-400 capitalize">{v.type} vault</div>
                </div>
              </div>
              {#if activeVault?.id === v.id}
                <CheckCircle2 class="h-4 w-4 text-emerald-400 shrink-0" />
              {/if}
            </button>
          {/each}
        </div>
      </div>
    </div>
  {/if}

  <!-- Mobile Terminal Session Switcher Bottom Sheet -->
  {#if platform.isMobile && isSessionSwitcherOpen}
    <div
      class="fixed inset-0 z-50 flex flex-col justify-end bg-black/70 backdrop-blur-xs"
      onclick={() => (isSessionSwitcherOpen = false)}
      role="presentation"
    >
      <div
        class="rounded-t-3xl border-t border-[#262635] bg-[#12121a] px-4 pt-3 pb-[calc(1.5rem+var(--sab))] space-y-3 shadow-2xl"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => {
          if (e.key === 'Escape') isSessionSwitcherOpen = false;
        }}
        role="dialog"
        tabindex="-1"
        aria-label="Active Terminal Sessions"
      >
        <div class="mx-auto h-1 w-10 rounded-full bg-neutral-700"></div>
        <div class="flex items-center justify-between pt-1">
          <h2 class="text-sm font-semibold text-white">Active Sessions ({terminalStore.tabCount})</h2>
          <button
            type="button"
            onclick={() => (isSessionSwitcherOpen = false)}
            class="rounded-lg p-1.5 text-neutral-400 active:bg-[#1e1e2c]"
            aria-label="Close session switcher"
          >
            <X class="h-4 w-4" />
          </button>
        </div>

        <div class="max-h-64 overflow-y-auto space-y-2">
          {#each terminalStore.tabs as tab (tab.id)}
            <div
              class="flex items-center justify-between rounded-2xl border px-3.5 py-3 transition {tab.active ? 'border-emerald-500/40 bg-emerald-500/10 text-white' : 'border-[#222230] bg-[#171722] text-neutral-300'}"
              onclick={() => {
                terminalStore.selectTab(tab.id);
                activeNav = 'terminal';
                isSessionSwitcherOpen = false;
              }}
              role="button"
              tabindex="0"
              onkeydown={(e) => {
                if (e.key === 'Enter') {
                  terminalStore.selectTab(tab.id);
                  activeNav = 'terminal';
                  isSessionSwitcherOpen = false;
                }
              }}
            >
              <div class="flex items-center gap-3 min-w-0">
                <span class="h-2.5 w-2.5 rounded-full {tab.active ? 'bg-emerald-400' : 'bg-neutral-500'} shrink-0"></span>
                <span class="text-sm font-medium truncate">{tab.label}</span>
              </div>
              <button
                type="button"
                onclick={(e) => {
                  e.stopPropagation();
                  terminalStore.closeTab(tab.id);
                  if (terminalStore.tabCount === 0) {
                    isSessionSwitcherOpen = false;
                    activeNav = 'hosts';
                  }
                }}
                class="flex h-8 w-8 items-center justify-center rounded-xl text-neutral-400 hover:text-rose-400 active:bg-rose-500/15 shrink-0"
                aria-label="Close session"
              >
                <X class="h-4 w-4" />
              </button>
            </div>
          {/each}
        </div>

        <button
          type="button"
          onclick={() => {
            handleNewLocalTab();
            isSessionSwitcherOpen = false;
          }}
          class="flex w-full items-center justify-center gap-2 rounded-2xl bg-emerald-500 py-3 text-sm font-semibold text-black active:scale-[0.98] transition"
        >
          <Plus class="h-4 w-4" />
          New Local Shell
        </button>
      </div>
    </div>
  {/if}

  <!-- Modals -->
  {#if isQuickConnectOpen}
    <div
      class="fixed inset-0 z-50 flex items-start justify-center bg-black/70 backdrop-blur-xs p-4 pt-12 sm:pt-20"
      onclick={() => (isQuickConnectOpen = false)}
      role="presentation"
    >
      <div
        class="w-full max-w-md rounded-2xl border border-[#28283a] bg-[#13131c] shadow-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-100"
        onclick={(e) => e.stopPropagation()}
        onkeydown={(e) => { if (e.key === 'Escape') isQuickConnectOpen = false; }}
        role="dialog"
        aria-modal="true"
        tabindex="-1"
        aria-label="Quick Connect New Tab"
      >
        <div class="flex items-center gap-2.5 border-b border-[#222232] px-3.5 py-3">
          <Search class="h-4 w-4 text-emerald-400 shrink-0" />
          <input
            bind:this={quickConnectInput}
            type="text"
            bind:value={quickConnectQuery}
            autocapitalize="none"
            autocorrect="off"
            spellcheck={false}
            placeholder="Choose a remote host to connect..."
            onkeydown={(e) => {
              if (e.key === 'Enter' && quickConnectHosts.length > 0) {
                e.preventDefault();
                const target = quickConnectHosts[0];
                isQuickConnectOpen = false;
                handleConnect(target);
              }
            }}
            class="flex-1 bg-transparent text-sm sm:text-xs text-white placeholder-neutral-500 outline-none"
          />
          <span class="hidden sm:inline rounded border border-[#282838] bg-[#1a1a26] px-1.5 py-0.5 text-[10px] font-mono text-neutral-400">
            {platform.isMac ? '⌘T' : 'Ctrl+T'}
          </span>
          <button
            type="button"
            onclick={() => (isQuickConnectOpen = false)}
            class="rounded-lg p-1 text-neutral-400 hover:text-white"
            aria-label="Close"
          >
            <X class="h-4 w-4" />
          </button>
        </div>

        <div class="max-h-72 overflow-y-auto p-2 space-y-1">
          {#if quickConnectHosts.length === 0}
            <div class="px-3 py-6 text-center text-xs text-neutral-500">
              {hosts.length === 0 ? 'No remote hosts saved yet.' : 'No matching remote hosts.'}
            </div>
          {:else}
            {#each quickConnectHosts as host (host.id)}
              <button
                type="button"
                onclick={() => {
                  isQuickConnectOpen = false;
                  handleConnect(host);
                }}
                class="flex w-full items-center justify-between gap-3 rounded-xl px-3 py-2.5 text-left hover:bg-[#1c1c2a] active:bg-[#222234] transition cursor-pointer group"
              >
                <div class="flex items-center gap-3 min-w-0">
                  <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                    <Server class="h-4 w-4" />
                  </div>
                  <div class="min-w-0">
                    <div class="text-xs font-semibold text-white truncate">{host.label}</div>
                    <div class="text-[11px] font-mono text-neutral-400 truncate">
                      {host.username}@{host.address}:{host.port || 22}
                    </div>
                  </div>
                </div>
                <span class="rounded-md bg-[#1a1a26] px-2 py-0.5 text-[10px] text-neutral-400 group-hover:text-emerald-300 shrink-0">
                  Connect
                </span>
              </button>
            {/each}
          {/if}
        </div>

        <div class="border-t border-[#222232] bg-[#101018] p-2 flex items-center justify-between gap-2">
          <button
            type="button"
            onclick={() => {
              isQuickConnectOpen = false;
              handleNewLocalTab();
            }}
            class="flex items-center gap-2 rounded-xl px-3 py-2 text-xs font-medium text-neutral-300 hover:bg-[#1a1a26] hover:text-white transition cursor-pointer"
          >
            <Terminal class="h-3.5 w-3.5 text-cyan-400" />
            <span>Local Shell</span>
          </button>
          <button
            type="button"
            onclick={() => {
              isQuickConnectOpen = false;
              editingHost = null;
              isAddHostOpen = true;
            }}
            class="flex items-center gap-1.5 rounded-xl px-3 py-2 text-xs font-medium text-emerald-400 hover:bg-emerald-500/10 transition cursor-pointer"
          >
            <Plus class="h-3.5 w-3.5" />
            <span>New Host</span>
          </button>
        </div>
      </div>
    </div>
  {/if}

  <AddHostDialog
    isOpen={isAddHostOpen}
    initialHost={editingHost}
    activeVaultId={activeVault?.id}
    onClose={() => {
      isAddHostOpen = false;
      editingHost = null;
    }}
    onSave={handleSaveHost}
  />

  <ImportModal
    isOpen={isImportOpen}
    onClose={() => (isImportOpen = false)}
    onImport={handleImportHosts}
  />

  <MoveHostModal
    isOpen={isMoveModalOpen}
    host={movingHost}
    currentVault={activeVault}
    {vaults}
    {availableFolders}
    onClose={() => {
      isMoveModalOpen = false;
      movingHost = null;
    }}
    onMoveToFolder={handleMoveToFolder}
    onMoveToVault={handleMoveToVault}
  />

  <ToastContainer />

  {#if incomingRemoteRequests.length > 0}
    {@const req = incomingRemoteRequests[0]}
    <div class="fixed top-4 right-4 left-4 sm:left-auto sm:w-96 z-[80] rounded-2xl border border-emerald-500/40 bg-[#14141f]/95 p-4 shadow-2xl backdrop-blur-md space-y-3 animate-in fade-in slide-in-from-top-2 duration-150">
      <div class="flex items-start justify-between gap-2">
        <div>
          <p class="text-xs font-bold text-emerald-400 uppercase tracking-wider">Remote Access Request</p>
          <p class="text-sm font-semibold text-white mt-0.5">{req.requester_name} <span class="text-xs font-normal text-neutral-400">({req.requester_email})</span></p>
          <p class="text-xs text-neutral-300 mt-0.5">
            Wants <span class="font-semibold text-cyan-300">{req.mode === 'control' ? 'Control (Read-Write)' : 'View-Only'}</span> on <span class="font-mono text-white">{req.host_label}</span>
          </p>
        </div>
      </div>
      <div class="grid grid-cols-3 gap-2 text-xs font-semibold">
        <button
          type="button"
          onclick={() => handleRespondRemote(req, 'reject')}
          class="rounded-xl bg-[#222232] py-2 text-neutral-300 hover:text-white"
        >
          Decline
        </button>
        <button
          type="button"
          onclick={() => handleRespondRemote(req, 'approve', 'view')}
          class="rounded-xl bg-indigo-600 py-2 text-white hover:bg-indigo-500"
        >
          View Only
        </button>
        <button
          type="button"
          onclick={() => handleRespondRemote(req, 'approve', 'control')}
          class="rounded-xl bg-emerald-600 py-2 text-white hover:bg-emerald-500"
        >
          Allow Control
        </button>
      </div>
    </div>
  {/if}
</div>
