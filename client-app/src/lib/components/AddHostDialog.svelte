<script lang="ts">
  import { X, Server, ChevronDown, ChevronRight, Radio, Network, Eye, EyeOff } from '@lucide/svelte';
  import { api } from '../api/client';
  import { decryptVaultPayload } from '../crypto/vault';

  interface Props {
    isOpen: boolean;
    initialHost?: any;
    activeVaultId?: string;
    onClose: () => void;
    onSave: (host: any) => void;
  }

  let { isOpen, initialHost = null, activeVaultId, onClose, onSave }: Props = $props();

  let label = $state('');
  let address = $state('');
  let port = $state(22);
  let username = $state('root');
  let credMode = $state<'manual' | 'identity' | 'keychain'>('manual');
  let authType = $state<'password' | 'key'>('password');
  let password = $state('');
  let privateKey = $state('');
  let passphrase = $state('');
  let showPassword = $state(false);
  let showPassphrase = $state(false);
  let useMosh = $state(false);
  let group = $state('Default');
  let tags = $state('');
  let showAdvanced = $state(false);

  let vaultIdentities = $state<any[]>([]);
  let vaultKeys = $state<any[]>([]);
  let vaultHosts = $state<any[]>([]);
  let selectedIdentityId = $state('');
  let selectedKeyId = $state('');

  // Proxy / Jump Host Configuration
  let proxyMode = $state<'none' | 'jump_host' | 'socks5' | 'http'>('none');
  let proxyJumpHostId = $state('');
  let proxyHost = $state('127.0.0.1');
  let proxyPort = $state(1080);

  async function loadVaultCredentials() {
    if (!activeVaultId) return;
    try {
      const [identities, keys, rawHosts] = await Promise.all([
        api.getVaultItems(activeVaultId, 'identity'),
        api.getVaultItems(activeVaultId, 'key'),
        api.getVaultItems(activeVaultId, 'host')
      ]);

      vaultHosts = rawHosts.filter((h) => !initialHost || h.id !== initialHost.id);

      const secret = api.getVaultSecret(activeVaultId);
      vaultIdentities = await Promise.all(
        identities.map(async (item) => {
          const parsed = await decryptVaultPayload<any>(item.ciphertext, item.nonce, secret);
          return {
            id: item.id,
            label: item.label,
            username: parsed?.username || 'root',
            authType: parsed?.authType || 'password',
            password: parsed?.password || '',
            privateKey: parsed?.privateKey || '',
            passphrase: parsed?.passphrase || ''
          };
        })
      );

      vaultKeys = await Promise.all(
        keys.map(async (item) => {
          const parsed = await decryptVaultPayload<any>(item.ciphertext, item.nonce, secret);
          return {
            id: item.id,
            label: item.label,
            privateKey: parsed?.privateKey || '',
            passphrase: parsed?.passphrase || ''
          };
        })
      );
    } catch {
      // ignore
    }
  }

  function handleSelectIdentity(id: string) {
    selectedIdentityId = id;
    const found = vaultIdentities.find((i) => i.id === id);
    if (found) {
      username = found.username;
      authType = found.authType;
      password = found.authType === 'password' ? found.password : '';
      privateKey = found.authType === 'key' ? found.privateKey : '';
      passphrase = found.authType === 'key' ? found.passphrase : '';
    }
  }

  function handleSelectKey(id: string) {
    selectedKeyId = id;
    const found = vaultKeys.find((k) => k.id === id);
    if (found) {
      authType = 'key';
      privateKey = found.privateKey;
      passphrase = found.passphrase;
    }
  }

  $effect(() => {
    if (isOpen) {
      loadVaultCredentials();
      if (initialHost) {
        label = initialHost.label || '';
        address = initialHost.address || '';
        port = initialHost.port || 22;
        username = initialHost.username || 'root';
        authType = initialHost.authType || 'password';
        password = initialHost.password || '';
        privateKey = initialHost.privateKey || '';
        passphrase = initialHost.passphrase || '';
        useMosh = !!initialHost.useMosh;
        group = initialHost.group || 'Default';
        tags = Array.isArray(initialHost.tags)
          ? initialHost.tags.join(', ')
          : initialHost.tags || '';
        selectedIdentityId = initialHost.identityId || '';
        selectedKeyId = '';
        credMode = initialHost.identityId ? 'identity' : 'manual';
        if (initialHost.proxy) {
          proxyMode = initialHost.proxy.mode || 'none';
          proxyJumpHostId = initialHost.proxy.jumpHostId || '';
          proxyHost = initialHost.proxy.host || '127.0.0.1';
          proxyPort = initialHost.proxy.port || 1080;
        } else {
          proxyMode = 'none';
          proxyJumpHostId = '';
          proxyHost = '127.0.0.1';
          proxyPort = 1080;
        }
        showAdvanced = Boolean(
          initialHost.proxy ||
            initialHost.useMosh ||
            (initialHost.group && initialHost.group !== 'Default') ||
            (Array.isArray(initialHost.tags) && initialHost.tags.length > 0)
        );
      } else {
        label = '';
        address = '';
        port = 22;
        username = 'root';
        authType = 'password';
        password = '';
        privateKey = '';
        passphrase = '';
        useMosh = false;
        group = 'Default';
        tags = '';
        selectedIdentityId = '';
        selectedKeyId = '';
        credMode = 'manual';
        proxyMode = 'none';
        proxyJumpHostId = '';
        proxyHost = '127.0.0.1';
        proxyPort = 1080;
        showAdvanced = false;
      }
    }
  });

  function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    // Always re-apply selected Identity or Keychain right before saving (prevents stale/empty credentials)
    if (credMode === 'identity' && selectedIdentityId) {
      handleSelectIdentity(selectedIdentityId);
    } else if (credMode === 'keychain' && selectedKeyId) {
      handleSelectKey(selectedKeyId);
    }
    const cleanAddr = address.trim() || '127.0.0.1';
    onSave({
      id: initialHost?.id || crypto.randomUUID(),
      label: label.trim() || cleanAddr,
      address: cleanAddr,
      port: port || 22,
      username: username.trim() || 'root',
      authType,
      identityId: credMode === 'identity' ? selectedIdentityId : '',
      password: authType === 'password' ? password : '',
      privateKey: authType === 'key' ? privateKey : '',
      passphrase: authType === 'key' ? passphrase : '',
      useMosh,
      group: group || 'Default',
      tags: tags
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean),
      pingMs: initialHost?.pingMs || Math.floor(Math.random() * 25) + 10,
      proxy:
        proxyMode === 'none'
          ? undefined
          : {
              mode: proxyMode,
              jumpHostId: proxyJumpHostId,
              host: proxyHost,
              port: proxyPort
            }
    });
    onClose();
  }
</script>

{#if isOpen}
  <div class="fixed inset-0 z-50 flex items-stretch sm:items-center justify-center bg-[#0e0e14] sm:bg-black/75 sm:backdrop-blur-sm p-0 sm:p-4 overflow-y-auto">
    <div class="w-full h-full sm:h-auto sm:max-w-lg sm:max-h-[85dvh] flex flex-col overflow-y-auto rounded-none sm:rounded-2xl sm:border sm:border-[#262635] bg-[#14141d] p-5 pb-[calc(1.25rem+var(--sab))] sm:p-6 shadow-2xl space-y-5 animate-in fade-in duration-150 sm:my-auto">
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-3">
          <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
            <Server class="h-5 w-5" />
          </div>
          <h3 class="text-lg sm:text-base font-bold text-white">
            {initialHost?.id ? 'Edit Host' : 'New Host'}
          </h3>
        </div>
        <button type="button" onclick={onClose} class="text-neutral-400 hover:text-white p-2 rounded-xl hover:bg-[#20202c]">
          <X class="h-5 w-5 sm:h-4 sm:w-4" />
        </button>
      </div>

      <form onsubmit={handleSubmit} class="flex-1 flex flex-col justify-between space-y-4 text-sm sm:text-xs">
        <div class="space-y-4">
        <div>
          <label for="host-label" class="block text-neutral-300 font-medium mb-1.5">Label <span class="text-neutral-500 font-normal">(optional)</span></label>
          <input
            id="host-label"
            type="text"
            bind:value={label}
            placeholder="e.g. Prod Server (defaults to IP)"
            class="w-full rounded-xl border border-[#252535] bg-[#0e0e14] px-3.5 py-3 sm:py-2.5 text-base sm:text-xs text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
          />
        </div>

        <div class="grid grid-cols-4 gap-2.5">
          <div class="col-span-3">
            <label for="host-addr" class="block text-neutral-300 font-medium mb-1.5">Host / IP</label>
            <input
              id="host-addr"
              type="text"
              bind:value={address}
              placeholder="192.168.1.10 or server.com"
              autocapitalize="none"
              autocorrect="off"
              spellcheck={false}
              class="w-full rounded-xl border border-[#252535] bg-[#0e0e14] px-3.5 py-3 sm:py-2.5 font-mono text-base sm:text-xs text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
              required
            />
          </div>
          <div>
            <label for="host-port" class="block text-neutral-300 font-medium mb-1.5">Port</label>
            <input
              id="host-port"
              type="number"
              inputmode="numeric"
              bind:value={port}
              class="w-full rounded-xl border border-[#252535] bg-[#0e0e14] px-3 py-3 sm:py-2.5 font-mono text-base sm:text-xs text-white outline-none focus:border-emerald-500"
              required
            />
          </div>
        </div>

        <div>
          <label for="host-user" class="block text-neutral-300 font-medium mb-1.5">Username</label>
          <input
            id="host-user"
            type="text"
            bind:value={username}
            placeholder="root / ubuntu"
            autocapitalize="none"
            autocorrect="off"
            spellcheck={false}
            class="w-full rounded-xl border border-[#252535] bg-[#0e0e14] px-3.5 py-3 sm:py-2.5 font-mono text-base sm:text-xs text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
            required
          />
        </div>

        <!-- Compact Auth Section -->
        <div class="rounded-xl border border-[#252535] bg-[#0f0f18] p-3 space-y-2.5">
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-1 bg-[#1a1a24] p-0.5 rounded-lg border border-[#2a2a3a]">
              <button
                type="button"
                onclick={() => { credMode = 'manual'; authType = 'password'; }}
                class="px-2.5 py-1 rounded-md text-[11px] font-medium transition {credMode === 'manual' && authType === 'password' ? 'bg-emerald-600 text-white' : 'text-neutral-400 hover:text-white'}"
              >
                Password
              </button>
              <button
                type="button"
                onclick={() => { credMode = 'manual'; authType = 'key'; }}
                class="px-2.5 py-1 rounded-md text-[11px] font-medium transition {credMode === 'manual' && authType === 'key' ? 'bg-emerald-600 text-white' : 'text-neutral-400 hover:text-white'}"
              >
                SSH Key
              </button>
              {#if vaultIdentities.length > 0}
                <button
                  type="button"
                  onclick={() => {
                    credMode = 'identity';
                    if (!selectedIdentityId && vaultIdentities.length > 0) {
                      handleSelectIdentity(vaultIdentities[0].id);
                    } else if (selectedIdentityId) {
                      handleSelectIdentity(selectedIdentityId);
                    }
                  }}
                  class="px-2.5 py-1 rounded-md text-[11px] font-medium transition {credMode === 'identity' ? 'bg-indigo-600 text-white' : 'text-neutral-400 hover:text-white'}"
                >
                  Identity
                </button>
              {/if}
              {#if vaultKeys.length > 0}
                <button
                  type="button"
                  onclick={() => {
                    credMode = 'keychain';
                    if (!selectedKeyId && vaultKeys.length > 0) {
                      handleSelectKey(vaultKeys[0].id);
                    } else if (selectedKeyId) {
                      handleSelectKey(selectedKeyId);
                    }
                  }}
                  class="px-2.5 py-1 rounded-md text-[11px] font-medium transition {credMode === 'keychain' ? 'bg-emerald-600 text-white' : 'text-neutral-400 hover:text-white'}"
                >
                  Keychain
                </button>
              {/if}
            </div>
          </div>

          {#if credMode === 'identity'}
            <select
              id="id-select"
              bind:value={selectedIdentityId}
              onchange={(e) => handleSelectIdentity(e.currentTarget.value)}
              class="w-full rounded-xl border border-[#28283c] bg-[#0a0a10] px-3 py-2.5 text-sm sm:text-xs text-white outline-none focus:border-indigo-500"
            >
              <option value="" disabled>-- Choose Identity --</option>
              {#each vaultIdentities as id}
                <option value={id.id}>{id.label} ({id.username})</option>
              {/each}
            </select>
          {:else if credMode === 'keychain'}
            <select
              id="keychain-select"
              bind:value={selectedKeyId}
              onchange={(e) => handleSelectKey(e.currentTarget.value)}
              class="w-full rounded-xl border border-[#28283c] bg-[#0a0a10] px-3 py-2.5 text-sm sm:text-xs text-white outline-none focus:border-emerald-500"
            >
              <option value="" disabled>-- Choose Key --</option>
              {#each vaultKeys as k}
                <option value={k.id}>{k.label}</option>
              {/each}
            </select>
          {:else if authType === 'password'}
            <div class="relative">
              <input
                id="host-pass"
                type={showPassword ? 'text' : 'password'}
                bind:value={password}
                placeholder="SSH password (optional)"
                class="w-full rounded-xl border border-[#28283c] bg-[#0a0a10] pl-3 pr-10 py-2.5 text-sm sm:text-xs text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
              />
              <button
                type="button"
                onclick={() => (showPassword = !showPassword)}
                class="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-md p-1 text-neutral-400 hover:text-white transition cursor-pointer"
                aria-label={showPassword ? 'Hide password' : 'Show password'}
                title={showPassword ? 'Hide password' : 'Show password'}
              >
                {#if showPassword}
                  <EyeOff class="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                {:else}
                  <Eye class="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                {/if}
              </button>
            </div>
          {:else}
            <div class="space-y-2">
              <textarea
                id="host-privkey"
                rows={3}
                bind:value={privateKey}
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                class="w-full rounded-xl border border-[#28283c] bg-[#0a0a10] px-3 py-2 font-mono text-[11px] text-white placeholder-neutral-600 outline-none focus:border-emerald-500"
              ></textarea>
              <div class="relative">
                <input
                  id="host-passphrase"
                  type={showPassphrase ? 'text' : 'password'}
                  bind:value={passphrase}
                  placeholder="Passphrase (optional)"
                  class="w-full rounded-xl border border-[#28283c] bg-[#0a0a10] pl-3 pr-10 py-2 text-sm sm:text-xs text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
                />
                <button
                  type="button"
                  onclick={() => (showPassphrase = !showPassphrase)}
                  class="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-md p-1 text-neutral-400 hover:text-white transition cursor-pointer"
                  aria-label={showPassphrase ? 'Hide passphrase' : 'Show passphrase'}
                  title={showPassphrase ? 'Hide passphrase' : 'Show passphrase'}
                >
                  {#if showPassphrase}
                    <EyeOff class="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                  {:else}
                    <Eye class="h-4 w-4 sm:h-3.5 sm:w-3.5" />
                  {/if}
                </button>
              </div>
            </div>
          {/if}
        </div>

        <!-- Collapsible Advanced Options -->
        <button
          type="button"
          onclick={() => (showAdvanced = !showAdvanced)}
          class="flex w-full items-center justify-between rounded-xl border border-[#22222e] bg-[#0e0e16] px-3 py-2.5 text-xs font-medium text-neutral-300 hover:text-white transition"
        >
          <span>More Options (Folder, Tags, Proxy, Mosh)</span>
          {#if showAdvanced}
            <ChevronDown class="h-4 w-4 text-emerald-400" />
          {:else}
            <ChevronRight class="h-4 w-4 text-neutral-500" />
          {/if}
        </button>

        {#if showAdvanced}
          <div class="space-y-3 pt-1 animate-in fade-in duration-100">
            <div class="grid grid-cols-2 gap-2.5">
              <div>
                <label for="host-group" class="block text-neutral-400 text-[11px] mb-1">Folder</label>
                <input
                  id="host-group"
                  type="text"
                  bind:value={group}
                  placeholder="Default"
                  class="w-full rounded-xl border border-[#252535] bg-[#0e0e14] px-3 py-2 text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
                />
              </div>
              <div>
                <label for="host-tags" class="block text-neutral-400 text-[11px] mb-1">Tags</label>
                <input
                  id="host-tags"
                  type="text"
                  bind:value={tags}
                  placeholder="prod, web"
                  class="w-full rounded-xl border border-[#252535] bg-[#0e0e14] px-3 py-2 text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
                />
              </div>
            </div>

            <!-- Proxy & Jump Host Section -->
            <div class="rounded-xl border border-[#22222e] bg-[#0e0e16] p-3 space-y-3">
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <Network class="h-4 w-4 text-emerald-400" />
                  <span class="font-medium text-white text-xs">Proxy / Jump Host</span>
                </div>
                <select
                  bind:value={proxyMode}
                  class="rounded-lg border border-[#282838] bg-[#14141d] px-2 py-1 text-[11px] text-neutral-200 outline-none focus:border-emerald-500"
                >
                  <option value="none">Direct</option>
                  <option value="jump_host">Jump Host</option>
                  <option value="socks5">SOCKS5</option>
                  <option value="http">HTTP Proxy</option>
                </select>
              </div>

              {#if proxyMode === 'jump_host'}
                <div class="pt-2 border-t border-[#1e1e28] text-xs">
                  {#if vaultHosts.length === 0}
                    <p class="text-[11px] text-amber-400/90">No other hosts in vault to act as bastion.</p>
                  {:else}
                    <select
                      id="proxy-bastion-select"
                      bind:value={proxyJumpHostId}
                      class="w-full rounded-lg border border-[#282838] bg-[#0a0a10] px-3 py-2 text-xs text-white outline-none focus:border-emerald-500"
                    >
                      <option value="">-- Choose Jump Host --</option>
                      {#each vaultHosts as vh}
                        <option value={vh.id}>{vh.label}</option>
                      {/each}
                    </select>
                  {/if}
                </div>
              {:else if proxyMode === 'socks5' || proxyMode === 'http'}
                <div class="pt-2 border-t border-[#1e1e28] grid grid-cols-3 gap-2 text-xs">
                  <input
                    id="proxy-target-host"
                    type="text"
                    bind:value={proxyHost}
                    placeholder="127.0.0.1"
                    class="col-span-2 w-full rounded-lg border border-[#282838] bg-[#0a0a10] px-2.5 py-2 text-xs text-white outline-none focus:border-emerald-500"
                  />
                  <input
                    id="proxy-target-port"
                    type="number"
                    bind:value={proxyPort}
                    placeholder="1080"
                    class="w-full rounded-lg border border-[#282838] bg-[#0a0a10] px-2.5 py-2 text-xs text-white outline-none focus:border-emerald-500 font-mono"
                  />
                </div>
              {/if}
            </div>

            <!-- Mosh option -->
            <div class="rounded-xl border border-[#22222e] bg-[#0e0e16] p-3 flex items-center justify-between">
              <div class="flex items-center gap-2">
                <Radio class="h-4 w-4 text-indigo-400" />
                <span class="font-medium text-white">Enable Mosh Protocol</span>
              </div>
              <input
                type="checkbox"
                bind:checked={useMosh}
                class="h-4 w-4 accent-emerald-500 rounded cursor-pointer"
              />
            </div>
          </div>
        {/if}

        </div>

        <div class="grid grid-cols-2 sm:flex sm:justify-end gap-2.5 pt-3 border-t border-[#20202c]">
          <button
            type="button"
            onclick={onClose}
            class="rounded-xl sm:rounded-lg bg-[#20202c] px-4 py-3 sm:py-2 text-sm sm:text-xs font-semibold text-neutral-300 hover:bg-[#282838] transition"
          >
            Cancel
          </button>
          <button
            type="submit"
            class="rounded-xl sm:rounded-lg bg-emerald-600 px-5 py-3 sm:py-2 text-sm sm:text-xs font-semibold text-white shadow hover:bg-emerald-500 transition active:scale-95"
          >
            {initialHost?.id ? 'Update Host' : 'Save Host'}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}
