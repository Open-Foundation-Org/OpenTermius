// Svelte 5 Terminal State Management with Runes

export interface TerminalHostConfig {
  address: string;
  port: number;
  username: string;
  authType?: 'password' | 'key';
  password?: string;
  privateKey?: string;
  passphrase?: string;
}

export interface RemoteViewerConfig {
  sessionId: string;
  requestId: string;
  mode: 'view' | 'control';
  ownerName: string;
}

export interface TerminalTab {
  id: string;
  hostId: string;
  label: string;
  vaultId?: string;
  vaultName?: string;
  startedAt: string;
  config?: TerminalHostConfig;
  remoteConfig?: RemoteViewerConfig;
  connected: boolean;
  active: boolean;
}

class TerminalStore {
  // Svelte 5 Runes
  tabs = $state<TerminalTab[]>([]);
  activeTabId = $state<string | null>(null);
  splitTab = $state<TerminalTab | null>(null);
  splitPickerOpen = $state(false);
  splitDirection = $state<'horizontal' | 'vertical'>('horizontal');

  activeTab = $derived(
    this.tabs.find((tab) => tab.id === this.activeTabId) || null
  );

  hasSplitPane = $derived(this.splitPickerOpen || this.splitTab !== null);

  tabCount = $derived(this.tabs.length);

  openTab(
    hostId: string,
    label: string,
    config?: TerminalHostConfig,
    meta?: { vaultId?: string; vaultName?: string; remoteConfig?: RemoteViewerConfig }
  ): string {
    const id = crypto.randomUUID();
    const newTab: TerminalTab = {
      id,
      hostId,
      label,
      vaultId: meta?.vaultId,
      vaultName: meta?.vaultName,
      startedAt: new Date().toISOString(),
      config,
      remoteConfig: meta?.remoteConfig,
      connected: false,
      active: true
    };

    // Deactivate previous active tab
    for (const tab of this.tabs) {
      tab.active = false;
    }

    this.tabs.push(newTab);
    this.activeTabId = id;
    return id;
  }

  openSplitPicker(direction: 'horizontal' | 'vertical' = 'horizontal') {
    this.splitDirection = direction;
    this.splitTab = null;
    this.splitPickerOpen = true;
  }

  connectSplit(
    hostId: string,
    label: string,
    config?: TerminalHostConfig,
    meta?: { vaultId?: string; vaultName?: string }
  ): string {
    const id = crypto.randomUUID();
    this.splitTab = {
      id,
      hostId,
      label,
      vaultId: meta?.vaultId,
      vaultName: meta?.vaultName,
      startedAt: new Date().toISOString(),
      config,
      connected: false,
      active: true
    };
    this.splitPickerOpen = false;
    return id;
  }

  closeSplit() {
    this.splitTab = null;
    this.splitPickerOpen = false;
  }

  closeTab(tabId: string) {
    if (this.splitTab?.id === tabId) {
      this.splitTab = null;
      return;
    }
    const idx = this.tabs.findIndex((t) => t.id === tabId);
    if (idx !== -1) {
      this.tabs.splice(idx, 1);
      if (this.activeTabId === tabId) {
        this.activeTabId = this.tabs.length > 0 ? this.tabs[Math.max(0, idx - 1)].id : null;
        if (this.activeTabId) {
          const nextActive = this.tabs.find((t) => t.id === this.activeTabId);
          if (nextActive) nextActive.active = true;
        }
      }
      if (this.tabs.length === 0) {
        this.closeSplit();
      }
    }
  }

  setConnected(tabId: string, connected: boolean) {
    if (this.splitTab?.id === tabId) {
      this.splitTab.connected = connected;
      return;
    }
    const tab = this.tabs.find((t) => t.id === tabId);
    if (tab) {
      tab.connected = connected;
    }
  }

  selectTab(tabId: string) {
    for (const tab of this.tabs) {
      tab.active = tab.id === tabId;
    }
    this.activeTabId = tabId;
  }
}

export const terminalStore = new TerminalStore();
