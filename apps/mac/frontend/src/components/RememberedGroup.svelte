<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Remembered phone card: reconnect plus a quiet forget action with an
  inline two-step confirm (destructive actions confirm in place, never
  in a modal here). Plain language only: no addresses, just when the
  phone was last seen.
-->
<script lang="ts">
  import { History } from '@lucide/svelte';

  interface Props {
    seenLabel: string;
    reconnecting: boolean;
    reconnectResult: string;
    forgetting: boolean;
    forgetResult: string;
    onReconnect: () => void;
    onForget: () => void;
  }

  let { seenLabel, reconnecting, reconnectResult, forgetting, forgetResult, onReconnect, onForget }: Props =
    $props();

  let confirming = $state(false);
</script>

<section aria-label="Last connected phone" class="section border-t border-separator px-1 py-3">
  <div class="flex items-center gap-2.5">
    <History size={17} strokeWidth={2} class="flex-none text-secondary" aria-hidden="true" />
    <div class="min-w-0 flex-1">
      <h2 class="text-[13px] font-semibold text-label">Last Connected</h2>
      <p class="mt-0.5 text-[12px] text-secondary">Seen {seenLabel} · reconnects on its own</p>
    </div>
  </div>
  <div class="mt-3 flex items-center gap-2">
    <button
      type="button"
      onclick={onReconnect}
      disabled={reconnecting}
      class="inline-flex h-8 items-center gap-2 rounded-lg bg-window px-3.5 text-[13px] font-medium text-label transition active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
    >
      {#if reconnecting}
        <span class="spinner" aria-hidden="true"></span>
        <span>Reconnecting…</span>
      {:else}
        <span>Reconnect</span>
      {/if}
    </button>
    {#if !confirming}
      <button
        type="button"
        onclick={() => (confirming = true)}
        class="inline-flex h-8 items-center rounded-lg px-2.5 text-[13px] text-secondary transition hover:text-bad active:translate-y-[1px]"
      >
        Forget
      </button>
    {:else}
      <button
        type="button"
        onclick={() => {
          confirming = false;
          onForget();
        }}
        disabled={forgetting}
        class="inline-flex h-8 items-center gap-2 rounded-lg bg-destructive px-3.5 text-[13px] font-medium text-destructive-text transition hover:brightness-95 active:translate-y-[1px] disabled:opacity-50"
      >
        {forgetting ? 'Forgetting…' : 'Forget This Phone'}
      </button>
      <button
        type="button"
        onclick={() => (confirming = false)}
        class="inline-flex h-8 items-center rounded-lg px-2.5 text-[13px] text-secondary transition hover:text-label"
      >
        Keep
      </button>
    {/if}
  </div>
  {#if reconnectResult}
    <p class="mt-2 text-[12px] text-secondary" aria-live="polite">{reconnectResult}</p>
  {/if}
  {#if forgetResult}
    <p class="mt-2 text-[12px] text-secondary" aria-live="polite">{forgetResult}</p>
  {/if}
</section>
