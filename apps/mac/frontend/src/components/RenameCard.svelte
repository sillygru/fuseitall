<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Rename card: shows the effective phone name with an inline editor for the
  Mac-local alias. Saving an empty name clears the alias and falls back to
  the phone's advertised name. Plain text controls only (HIG Buttons:
  verb-led labels, no icon dependency).
-->
<script lang="ts">
  interface Props {
    displayName: string;
    advertisedName: string;
    saving: boolean;
    message: string;
    onSave: (name: string) => void;
  }

  let { displayName, advertisedName, saving, message, onSave }: Props = $props();

  let editing = $state(false);
  let draft = $state('');

  function startEdit(): void {
    draft = displayName === advertisedName ? '' : displayName;
    editing = true;
  }

  function cancel(): void {
    editing = false;
    draft = '';
  }

  function save(): void {
    onSave(draft);
    editing = false;
  }
</script>

<section aria-label="Phone name" class="section border-t border-separator px-1 py-3">
  {#if editing}
    <label for="rename-input" class="block text-[13px] font-medium text-label">Phone name</label>
    <p class="mt-0.5 text-[12px] text-secondary">
      {#if advertisedName}
        Clear to use the phone's name ({advertisedName}).
      {:else}
        Clear to use the phone's name.
      {/if}
    </p>
    <div class="mt-2 flex items-center gap-2">
      <input
        id="rename-input"
        type="text"
        maxlength={64}
        placeholder={advertisedName || 'Phone'}
        bind:value={draft}
        disabled={saving}
        onkeydown={(e) => {
          if (e.key === 'Enter') save();
          else if (e.key === 'Escape') cancel();
        }}
        class="h-8 min-w-0 flex-1 rounded-md bg-control px-2 text-[13px] text-label"
      />
      <button
        type="button"
        onclick={save}
        disabled={saving}
        class="h-8 flex-none rounded-md bg-accent px-3 text-[13px] font-medium text-accent-text transition hover:brightness-95 active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
      >
        {saving ? 'Saving…' : 'Save'}
      </button>
      <button
        type="button"
        onclick={cancel}
        disabled={saving}
        class="h-8 flex-none rounded-md bg-control px-3 text-[13px] text-label transition hover:brightness-95 active:translate-y-[1px] disabled:cursor-not-allowed disabled:opacity-50"
      >
        Cancel
      </button>
    </div>
  {:else}
    <div class="flex items-center gap-3">
      <div class="min-w-0 flex-1">
        <p class="text-[13px] text-secondary">Name</p>
        <p class="truncate text-[13px] font-medium text-label">{displayName}</p>
      </div>
      <button
        type="button"
        onclick={startEdit}
        class="h-7 flex-none rounded-md bg-control px-3 text-[13px] text-label transition hover:brightness-95 active:translate-y-[1px]"
      >
        Rename
      </button>
    </div>
  {/if}
  {#if message}
    <p class="mt-2 text-[12px] text-tertiary" aria-live="polite">{message}</p>
  {/if}
</section>
