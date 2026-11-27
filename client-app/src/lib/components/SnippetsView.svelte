<script lang="ts">
  import { onMount } from 'svelte';
  import { api, type ApiVaultItem, type ApiVault } from '../api/client';
  import { toast, formatErrorMessage } from '../stores/toast.svelte';
  import { encryptVaultPayload, decryptVaultPayload } from '../crypto/vault';
  import { FileCode2, Play, Plus, RefreshCw, AlertCircle, Pencil, Trash2, Copy, Check } from '@lucide/svelte';

  interface Props {
    activeVaultId?: string;
    onRunSnippet?: (script: string) => void;
  }

  let { activeVaultId, onRunSnippet }: Props = $props();

  let state = $state<'loading' | 'success' | 'empty' | 'error'>('loading');
  let errorMessage = $state<string | null>(null);
  let snippets = $state<Array<{ id: string; title: string; script: string; category: string }>>([]);
  let isCreating = $state(false);
  let editingId = $state<string | null>(null);
  let copiedId = $state<string | null>(null);

  let newTitle = $state('');
  let newScript = $state('');
  let newCategory = $state('General');

  async function loadSnippets() {
    if (!activeVaultId) {
      state = 'empty';
      return;
    }

    state = 'loading';
    errorMessage = null;

    try {
      const items = await api.getVaultItems(activeVaultId, 'snippet');
      if (items.length === 0) {
        snippets = [];
        state = 'empty';
        return;
      }

      // Decrypt payload
      const secret = api.getVaultSecret(activeVaultId);
      snippets = await Promise.all(
        items.map(async (item) => {
          const parsed = await decryptVaultPayload<any>(item.ciphertext, item.nonce, secret);
          return {
            id: item.id,
            title: item.label,
            script: parsed?.script || '',
            category: parsed?.category || 'General'
          };
        })
      );

      state = 'success';
    } catch (err: unknown) {
      state = 'error';
      errorMessage = formatErrorMessage(err, 'Failed to fetch snippets from backend');
    }
  }

  function openNewForm() {
    editingId = null;
    newTitle = '';
    newScript = '';
    newCategory = 'General';
    isCreating = true;
  }

  function openEditForm(snippet: { id: string; title: string; script: string; category: string }) {
    editingId = snippet.id;
    newTitle = snippet.title;
    newScript = snippet.script;
    newCategory = snippet.category || 'General';
    isCreating = true;
  }

  async function handleSaveSnippet(e: SubmitEvent) {
    e.preventDefault();
    if (!activeVaultId || !newTitle.trim() || !newScript.trim()) return;

    try {
      const payload = { script: newScript, category: newCategory.trim() || 'General' };
      const { ciphertext, nonce } = await encryptVaultPayload(payload, api.getVaultSecret(activeVaultId));

      if (editingId) {
        await api.updateVaultItem(activeVaultId, editingId, {
          label: newTitle.trim(),
          ciphertext,
          nonce
        });
        snippets = snippets.map((s) =>
          s.id === editingId
            ? { ...s, title: newTitle.trim(), script: newScript, category: newCategory.trim() || 'General' }
            : s
        );
        toast.success('Snippet updated.');
      } else {
        const created = await api.createVaultItem(activeVaultId, {
          kind: 'snippet',
          label: newTitle.trim(),
          ciphertext,
          nonce
        });

        snippets.unshift({
          id: created.id,
          title: created.label,
          script: newScript,
          category: newCategory.trim() || 'General'
        });
        toast.success('Snippet saved.');
      }

      state = 'success';
      isCreating = false;
      editingId = null;
      newTitle = '';
      newScript = '';
      newCategory = 'General';
    } catch (err: unknown) {
      toast.error(err, 'Failed to save snippet');
    }
  }

  async function handleDeleteSnippet(id: string) {
    if (!activeVaultId) return;
    if (!(await toast.confirm('Delete this snippet?'))) return;
    try {
      await api.deleteVaultItem(activeVaultId, id);
      snippets = snippets.filter((s) => s.id !== id);
      if (snippets.length === 0) state = 'empty';
      toast.success('Snippet deleted.');
    } catch (err: unknown) {
      toast.error(err, 'Failed to delete snippet');
    }
  }

  async function handleCopySnippet(id: string, script: string) {
    try {
      await navigator.clipboard.writeText(script);
      copiedId = id;
      setTimeout(() => {
        if (copiedId === id) copiedId = null;
      }, 1800);
    } catch {
      toast.error('Clipboard permission unavailable');
    }
  }

  $effect(() => {
    if (activeVaultId !== undefined) {
      loadSnippets();
    }
  });

  onMount(() => {
    loadSnippets();
  });
</script>

<div class="h-full overflow-y-auto p-4 sm:p-6 space-y-4 sm:space-y-6">
  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-[#22222a] pb-4">
    <div class="min-w-0">
      <h2 class="text-base font-bold text-white tracking-tight">Snippets</h2>
    </div>

    <div class="flex items-center gap-2 shrink-0">
      <button
        type="button"
        onclick={loadSnippets}
        class="flex flex-1 sm:flex-none items-center justify-center gap-1.5 rounded-lg border border-[#22222a] bg-[#14141d] px-3 py-2 sm:py-1.5 text-xs text-neutral-300 hover:text-white transition"
      >
        <RefreshCw class="h-3.5 w-3.5" /> Refresh
      </button>

      <button
        type="button"
        onclick={() => (isCreating ? (isCreating = false) : openNewForm())}
        class="flex flex-1 sm:flex-none items-center justify-center gap-1.5 rounded-lg bg-emerald-600 px-3.5 py-2 sm:py-1.5 text-xs font-semibold text-white shadow hover:bg-emerald-500 transition cursor-pointer"
      >
        <Plus class="h-4 w-4" /> New Snippet
      </button>
    </div>
  </div>

  {#if isCreating}
    <form onsubmit={handleSaveSnippet} class="rounded-xl border border-emerald-500/30 bg-[#14141d] p-5 space-y-3 animate-in fade-in text-xs">
      <div>
        <label for="snip-title" class="block font-medium text-neutral-300 mb-1">Snippet Title</label>
        <input
          id="snip-title"
          type="text"
          bind:value={newTitle}
          placeholder="e.g. Inspect Docker Container Health"
          class="w-full rounded-lg border border-[#262635] bg-[#0c0c12] px-3 py-1.5 text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
          required
        />
      </div>
      <div>
        <label for="snip-cat" class="block font-medium text-neutral-300 mb-1">Category</label>
        <input
          id="snip-cat"
          type="text"
          bind:value={newCategory}
          placeholder="e.g. Diagnostics / DevOps"
          class="w-full rounded-lg border border-[#262635] bg-[#0c0c12] px-3 py-1.5 text-white placeholder-neutral-500 outline-none focus:border-emerald-500"
        />
      </div>
      <div>
        <label for="snip-code" class="block font-medium text-neutral-300 mb-1">Shell Script / Command</label>
        <textarea
          id="snip-code"
          bind:value={newScript}
          rows="3"
          placeholder="uptime && free -m && df -h"
          class="w-full rounded-lg border border-[#262635] bg-[#0c0c12] p-3 font-mono text-emerald-400 placeholder-neutral-600 outline-none focus:border-emerald-500"
          required
        ></textarea>
      </div>
      <div class="flex justify-end gap-2 pt-2">
        <button
          type="button"
          onclick={() => (isCreating = false)}
          class="rounded-lg bg-[#20202c] px-3 py-1.5 text-neutral-400 hover:text-white"
        >
          Cancel
        </button>
        <button
          type="submit"
          class="rounded-lg bg-emerald-600 px-4 py-1.5 font-semibold text-white hover:bg-emerald-500"
        >
          Save to Vault
        </button>
      </div>
    </form>
  {/if}

  <!-- 4 UI States -->
  {#if state === 'loading'}
    <div class="flex h-48 items-center justify-center text-xs text-neutral-400 gap-2">
      <RefreshCw class="h-4 w-4 animate-spin text-emerald-400" />
      <span>Loading snippets from backend database...</span>
    </div>
  {:else if state === 'error'}
    <div class="rounded-xl border border-rose-500/30 bg-rose-950/20 p-5 flex items-center justify-between">
      <div class="flex items-center gap-3">
        <AlertCircle class="h-5 w-5 text-rose-400 shrink-0" />
        <div>
          <p class="text-xs font-semibold text-rose-300">Backend Communication Error</p>
          <p class="text-[11px] text-rose-400/80">{errorMessage}</p>
        </div>
      </div>
      <button
        onclick={loadSnippets}
        class="rounded-lg bg-rose-500/20 px-3 py-1.5 text-xs font-semibold text-rose-200 hover:bg-rose-500/30"
      >
        Retry
      </button>
    </div>
  {:else if state === 'empty'}
    <div class="rounded-xl border border-[#22222a] bg-[#14141a] p-12 text-center space-y-3">
      <FileCode2 class="h-10 w-10 text-neutral-600 mx-auto" />
      <h3 class="text-sm font-semibold text-white">No Snippets in Vault</h3>
      <p class="text-xs text-neutral-400 max-w-sm mx-auto">
        Create shell snippets to automate server updates, container checks, and repetitive commands.
      </p>
      <button
        onclick={() => (isCreating = true)}
        class="rounded-lg bg-emerald-600 px-4 py-1.5 text-xs font-semibold text-white hover:bg-emerald-500"
      >
        + Create Your First Snippet
      </button>
    </div>
  {:else}
    <div class="space-y-3">
      {#each snippets as snippet (snippet.id)}
        <div class="rounded-xl border border-[#22222a] bg-[#14141a] p-4 hover:border-emerald-500/30 transition">
          <div class="flex items-center justify-between gap-2 mb-2">
            <div class="flex items-center gap-2 min-w-0">
              <h3 class="text-sm font-semibold text-white truncate">{snippet.title}</h3>
              <span class="rounded bg-[#1f1f2b] px-2 py-0.5 text-[10px] text-neutral-300 font-medium shrink-0">
                {snippet.category}
              </span>
            </div>

            <div class="flex items-center gap-1.5 shrink-0">
              <button
                type="button"
                onclick={() => handleCopySnippet(snippet.id, snippet.script)}
                class="rounded-lg p-1.5 text-neutral-400 hover:text-white hover:bg-white/[0.06] transition cursor-pointer"
                title="Copy script"
                aria-label="Copy script"
              >
                {#if copiedId === snippet.id}
                  <Check class="h-3.5 w-3.5 text-emerald-400" />
                {:else}
                  <Copy class="h-3.5 w-3.5" />
                {/if}
              </button>
              <button
                type="button"
                onclick={() => openEditForm(snippet)}
                class="rounded-lg p-1.5 text-neutral-400 hover:text-emerald-400 hover:bg-white/[0.06] transition cursor-pointer"
                title="Edit snippet"
                aria-label="Edit snippet"
              >
                <Pencil class="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onclick={() => handleDeleteSnippet(snippet.id)}
                class="rounded-lg p-1.5 text-neutral-400 hover:text-rose-400 hover:bg-white/[0.06] transition cursor-pointer"
                title="Delete snippet"
                aria-label="Delete snippet"
              >
                <Trash2 class="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onclick={() => onRunSnippet?.(snippet.script)}
                class="flex items-center gap-1.5 rounded-lg bg-emerald-500/15 border border-emerald-500/30 px-3 py-1.5 text-xs font-medium text-emerald-400 hover:bg-emerald-500/25 active:scale-95 transition shrink-0 cursor-pointer"
              >
                <Play class="h-3 w-3" /> Run
              </button>
            </div>
          </div>

          <pre class="rounded-lg bg-[#0d0d12] border border-[#1a1a24] p-3 text-xs font-mono text-neutral-300 overflow-x-auto"><code>{snippet.script}</code></pre>
        </div>
      {/each}
    </div>
  {/if}
</div>
