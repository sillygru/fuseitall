<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Pairing flow: centered QR, large code, three plain steps, one Copy
  Code action. No outer box: hierarchy comes from type + whitespace.
  The white QR well stays (functional quiet zone, not decoration).
  Addresses and the certificate fingerprint hide under Advanced: they
  diagnose, they do not greet.
  How pairing works: the Mac shows a QR + 6-digit code (its address +
  a one-time token). The phone scans it on the same Wi-Fi, verifies,
  and the Mac flips to paired. Nothing to configure.
-->
<script lang="ts">
  import { Check, Copy, ScanLine, ShieldCheck, Wifi } from '@lucide/svelte';

  interface PairStatus {
    Listening: boolean;
    QRHost: string;
    QRPort: number;
    QRCandidates: string[];
    LastRejectKind: string;
    LastRejectUnix: number;
    LastAcceptUnix: number;
  }

  interface Props {
    qrSrc: string;
    code: string;
    copied: boolean;
    hostPort: string;
    fingerprint: string;
    pairStatus?: PairStatus | null;
    onCopyCode: () => void;
  }

  let { qrSrc, code, copied, hostPort, fingerprint, pairStatus = null, onCopyCode }: Props = $props();

  function ago(unix: number): string {
    if (!unix) return '';
    const secs = Math.max(0, Math.floor(Date.now() / 1000 - unix));
    if (secs < 60) return 'just now';
    const mins = Math.floor(secs / 60);
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    return new Date(unix * 1000).toLocaleString();
  }

  // Plain-language listener state. Addresses stay under Advanced; this line
  // only tells the user whether the phone found us yet or was rejected.
  let statusLine = $derived.by(() => {
    if (!pairStatus) return '';
    if (pairStatus.LastRejectKind === 'auth') {
      return `That phone tried an old code ${ago(pairStatus.LastRejectUnix)} — scan the new code shown here.`;
    }
    if (pairStatus.LastRejectKind === 'update') {
      return `Version mismatch ${ago(pairStatus.LastRejectUnix)} — update when prompted above, then scan again.`;
    }
    if (pairStatus.LastRejectKind === 'bad_request') {
      return `Your phone reached this Mac but was rejected ${ago(pairStatus.LastRejectUnix)} — update both apps and retry.`;
    }
    if (pairStatus.LastAcceptUnix) {
      return `Last phone contact ${ago(pairStatus.LastAcceptUnix)} — waiting for it to return.`;
    }
    return 'Waiting for your phone — keep both on the same Wi-Fi and scan above.';
  });
</script>

<section aria-label="Pair your phone" class="section mx-auto w-full max-w-[480px] px-6 py-10">
  <div class="mx-auto max-w-[330px] text-center">
    <p class="text-[11px] font-semibold uppercase tracking-[0.16em] text-tertiary">Get connected</p>
    <h2 class="mt-2 text-[22px] font-semibold tracking-[-0.035em] text-label">Pair your phone</h2>
    <p class="mt-2 text-[13px] leading-relaxed text-secondary">Scan with FuseItAll on your phone to link it to this Mac.</p>
  </div>

  {#if qrSrc}
    <div class="anim-qr mx-auto my-7 w-fit rounded-[14px] bg-white p-4 shadow-[0_12px_34px_rgba(0,0,0,0.12)]">
      <img src={qrSrc} alt="Pairing QR code" class="h-48 w-48 rounded" />
    </div>
  {:else}
    <div class="mx-auto my-5 w-fit rounded-lg bg-altrow p-3 shadow-[0_8px_24px_rgba(0,0,0,0.06)]" role="status" aria-label="Waiting for pairing code">
      <div class="anim-skel h-48 w-48 rounded bg-grid/60"></div>
    </div>
    <p class="text-center text-[13px] text-secondary">Waiting for a secure pairing code…</p>
  {/if}

  {#if code}
    <p data-copy={code} style="--i: 2" class="anim-row mono text-center text-[24px] font-semibold tracking-[0.3em] text-label">{code}</p>
  {/if}

  <button
    type="button"
    onclick={onCopyCode}
    disabled={!code}
    style="--i: 3"
    class="anim-row mx-auto mt-4 flex h-9 items-center gap-2 rounded-[9px] bg-accent px-4 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
  >
    {#key copied}
      <span class="anim-badge items-center gap-2">
        {#if copied}
          <Check size={15} strokeWidth={2} aria-hidden="true" />
          <span>Copied</span>
        {:else}
          <Copy size={15} strokeWidth={2} aria-hidden="true" />
          <span>Copy Code</span>
        {/if}
      </span>
    {/key}
  </button>

  {#if statusLine}
    <p role="status" style="--i: 3" class="anim-row mx-auto mt-4 max-w-[420px] text-center text-[12px] leading-relaxed text-secondary">{statusLine}</p>
  {/if}

  <ol class="mx-auto mt-8 flex max-w-[420px] flex-col gap-3 bg-altrow/45 px-4 py-4 rounded-xl">
    <li style="--i: 4" class="anim-row flex items-center gap-2.5 text-[12px] text-secondary">
      <ScanLine size={15} strokeWidth={2} class="flex-none text-secondary" aria-hidden="true" />
      <span><span class="font-semibold text-label">1.</span> Open FuseItAll on your phone and scan the code</span>
    </li>
    <li style="--i: 5" class="anim-row flex items-center gap-2.5 text-[12px] text-secondary">
      <Wifi size={15} strokeWidth={2} class="flex-none text-secondary" aria-hidden="true" />
      <span><span class="font-semibold text-label">2.</span> Keep both devices on the same Wi-Fi</span>
    </li>
    <li style="--i: 6" class="anim-row flex items-center gap-2.5 text-[12px] text-secondary">
      <ShieldCheck size={15} strokeWidth={2} class="flex-none text-secondary" aria-hidden="true" />
      <span><span class="font-semibold text-label">3.</span> This Mac verifies the phone automatically</span>
    </li>
  </ol>

  <details class="mt-4 rounded-xl bg-altrow/45 px-3 py-2">
    <summary class="cursor-pointer text-[12px] text-tertiary">Advanced</summary>
    <div class="mt-2 py-2">
      <p class="text-[12px] text-secondary">Address</p>
      <p data-copy={hostPort} class="mono mt-0.5 break-all text-[12px] text-label">{hostPort || '…'}</p>
    </div>
    <div class="py-2">
      <p class="text-[12px] text-secondary">Certificate fingerprint</p>
      <p data-copy={fingerprint} class="mono mt-0.5 break-all text-[11px] leading-relaxed text-tertiary">{fingerprint || '…'}</p>
    </div>
  </details>
</section>
