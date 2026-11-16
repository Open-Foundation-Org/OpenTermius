<script lang="ts">
  import { onMount } from 'svelte';

  interface Props {
    onDone: () => void;
  }

  let { onDone }: Props = $props();
  let fading = $state(false);
  let step = $state(0);

  const lines = [
    'init russh-core & sftp...',
    'unlocking aes-256-gcm keystore...',
    'ready.'
  ];

  function dismiss() {
    if (fading) return;
    fading = true;
    setTimeout(onDone, 280);
  }

  onMount(() => {
    const t1 = setTimeout(() => (step = 1), 260);
    const t2 = setTimeout(() => (step = 2), 620);
    const t3 = setTimeout(dismiss, 1150);
    return () => {
      clearTimeout(t1);
      clearTimeout(t2);
      clearTimeout(t3);
    };
  });
</script>

<div
  class="fixed inset-0 z-[100] flex flex-col items-center justify-center bg-[#07080d] px-6 select-none transition-all duration-300 {fading ? 'opacity-0 scale-105 pointer-events-none' : 'opacity-100 scale-100'}"
  onclick={dismiss}
  role="presentation"
>
  <!-- Ambient Emerald/Cyan Glow -->
  <div class="pointer-events-none absolute h-64 w-64 rounded-full bg-emerald-500/15 blur-3xl animate-pulse"></div>

  <!-- Animated App Icon -->
  <div class="relative flex flex-col items-center">
    <div class="relative flex h-24 w-24 items-center justify-center animate-in zoom-in-75 duration-300">
      <div class="absolute inset-0 rounded-3xl bg-emerald-500/20 blur-xl animate-pulse"></div>
      <img
        src="/icon.svg"
        alt="OpenTermius"
        class="relative h-24 w-24 rounded-3xl shadow-2xl shadow-emerald-500/20"
      />
    </div>

    <h1 class="mt-5 text-xl font-bold tracking-tight text-white">
      Open<span class="text-emerald-400">Termius</span>
    </h1>

    <!-- Mini Terminal Boot Sequence -->
    <div class="mt-4 w-64 rounded-xl border border-[#1d202d] bg-[#0c0e15] px-3.5 py-2.5 font-mono text-[11px] space-y-1 shadow-lg">
      {#each lines.slice(0, step + 1) as line, i}
        <div class="flex items-center gap-2 text-neutral-300 animate-in fade-in duration-150">
          <span class={i === 2 ? 'text-cyan-400 font-bold' : 'text-emerald-400'}>
            {i === 2 ? '✓' : '>'}
          </span>
          <span class={i === 2 ? 'text-emerald-300 font-semibold' : ''}>{line}</span>
        </div>
      {/each}
    </div>

    <!-- Smooth Progress Bar -->
    <div class="mt-4 h-1 w-40 overflow-hidden rounded-full bg-[#181a24]">
      <div
        class="h-full rounded-full bg-gradient-to-r from-emerald-500 to-cyan-400 transition-all duration-500 ease-out"
        style="width: {step === 0 ? '32%' : step === 1 ? '72%' : '100%'}"
      ></div>
    </div>
  </div>
</div>
