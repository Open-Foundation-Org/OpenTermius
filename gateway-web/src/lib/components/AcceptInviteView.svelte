<script lang="ts">
  import { Mail, CheckCircle2 } from '@lucide/svelte';

  interface Props {
    verifyState: 'loading' | 'ready' | 'done' | 'invalid';
    inviteInfo: { email: string; name: string; expires_at: string } | null;
    verifyError: string | null;
    acceptError: string | null;
    isAccepting: boolean;
    onAccept: (password: string, confirmPassword: string) => void;
  }

  let {
    verifyState,
    inviteInfo,
    verifyError,
    acceptError,
    isAccepting,
    onAccept
  }: Props = $props();

  let password = $state('');
  let confirmPassword = $state('');

  function handleSubmit(e: Event) {
    e.preventDefault();
    onAccept(password, confirmPassword);
  }
</script>

<div class="flex min-h-screen w-screen items-center justify-center bg-[#0a0a0c] p-4 text-neutral-100 font-sans">
  <div class="w-full max-w-sm rounded-2xl border border-white/[0.08] bg-[#121218] p-6 shadow-2xl space-y-4 text-xs">
    <div class="text-center space-y-1">
      <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 mx-auto mb-2.5">
        <Mail class="h-5 w-5" />
      </div>
      <h1 class="text-base font-semibold text-white">Activate OpenTermius Account</h1>
      <p class="text-neutral-400">Set your own password to finish joining this Gateway</p>
    </div>

    {#if verifyState === 'loading'}
      <p class="py-6 text-center text-neutral-400">Verifying invite link...</p>
    {:else if verifyState === 'invalid'}
      <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-3 text-rose-300 text-center space-y-2">
        <p>{verifyError || 'This invite link is invalid or has expired.'}</p>
        <p class="text-[11px] text-neutral-400">Ask your Gateway admin to send a new invite.</p>
      </div>
    {:else if verifyState === 'done'}
      <div class="rounded-xl bg-emerald-950/30 border border-emerald-500/30 p-4 text-center space-y-2">
        <CheckCircle2 class="h-7 w-7 text-emerald-400 mx-auto" />
        <p class="text-sm font-semibold text-white">Account Activated!</p>
        <p class="text-neutral-300 leading-relaxed">
          You can now sign in from the <strong>OpenTermius</strong> Desktop or Mobile app using
          <span class="font-mono text-emerald-300">{inviteInfo?.email}</span>.
        </p>
      </div>
    {:else if inviteInfo}
      <div class="rounded-lg border border-white/[0.07] bg-[#0b0b10] px-3 py-2.5 space-y-0.5">
        <p class="font-medium text-white">{inviteInfo.name}</p>
        <p class="font-mono text-[11px] text-emerald-400">{inviteInfo.email}</p>
      </div>

      {#if acceptError}
        <div class="rounded-lg bg-rose-950/40 border border-rose-500/30 p-2.5 text-rose-300 text-[11px]">
          {acceptError}
        </div>
      {/if}

      <form onsubmit={handleSubmit} class="space-y-3">
        <div>
          <label for="acc-pw" class="block font-medium text-neutral-400 mb-1">Create Password</label>
          <input
            id="acc-pw"
            type="password"
            required
            minlength="8"
            autocomplete="new-password"
            bind:value={password}
            placeholder="Minimum 8 characters"
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          />
        </div>
        <div>
          <label for="acc-pw2" class="block font-medium text-neutral-400 mb-1">Confirm Password</label>
          <input
            id="acc-pw2"
            type="password"
            required
            minlength="8"
            autocomplete="new-password"
            bind:value={confirmPassword}
            placeholder="Repeat password"
            class="w-full rounded-lg border border-white/[0.08] bg-[#0b0b10] px-3 py-2 text-white outline-none focus:border-emerald-500/70"
          />
        </div>
        <button
          type="submit"
          disabled={isAccepting}
          class="w-full rounded-lg bg-emerald-600 py-2.5 font-semibold text-white hover:bg-emerald-500 transition disabled:opacity-50 cursor-pointer"
        >
          {isAccepting ? 'Activating...' : 'Set Password & Activate'}
        </button>
      </form>
    {/if}
  </div>
</div>
