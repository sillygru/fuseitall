<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Contacts pane: macOS Contacts-style split view. Left: searchable contact list
  with monograms/avatars. Right: contact detail card with quick actions
  (Message, Copy) and phone/email lists.
-->
<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import {
    Users,
    Search,
    RefreshCw,
    MessageSquare,
    Phone,
    Mail,
    Copy,
    Check,
    Star,
    AlertCircle,
    Trash2,
  } from '@lucide/svelte';
  import { Events } from '@wailsio/runtime';
  import ContentHeader from './ContentHeader.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import {
    listAllContacts,
    getContactAvatar,
    deleteContact,
    type ContactEntry,
    type ContactPhone,
  } from '../contacts_messages_api';

  interface Props {
    paired: boolean;
    deviceLabel: string;
    onMessageContact?: (address: string, displayName?: string) => void;
  }

  let { paired, deviceLabel, onMessageContact }: Props = $props();

  let query = $state('');
  let loading = $state(false);
  let loadProgress = $state('');
  let error = $state('');
  let errorCode = $state('');
  let permissionError = $state(false);
  let contacts = $state<ContactEntry[]>([]);
  let avatars = $state<Record<string, string>>({});
  let avatarVersions = $state<Record<string, string>>({});
  let avatarPending = new Set<string>();
  let selectedId = $state<string>('');
  let copiedField = $state<string>('');
  let prevPaired = $state(paired);
  let contactToDelete = $state<ContactEntry | null>(null);
  let deleteBusy = $state(false);
  let deleteError = $state('');

  function promptDeleteContact(contact: ContactEntry) {
    contactToDelete = contact;
    deleteError = '';
  }

  async function confirmDeleteContact() {
    if (!contactToDelete) return;
    deleteBusy = true;
    deleteError = '';
    try {
      const res = await deleteContact(contactToDelete.contact_id, contactToDelete.lookup_key);
      if (!res.ok) {
        deleteError = res.error || 'Failed to delete contact';
        error = deleteError;
        return;
      }
      const deletedId = contactToDelete.contact_id;
      contacts = contacts.filter((c) => c.contact_id !== deletedId);
      delete avatars[deletedId];
      delete avatarVersions[deletedId];
      if (selectedId === deletedId) {
        selectedId = contacts[0]?.contact_id ?? '';
      }
      contactToDelete = null;
    } catch (e: unknown) {
      deleteError = e instanceof Error ? e.message : String(e);
      error = deleteError;
    } finally {
      deleteBusy = false;
    }
  }

  function cancelDeleteContact() {
    if (deleteBusy) return;
    contactToDelete = null;
    deleteError = '';
  }

  let filteredContacts = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return contacts;
    return contacts.filter((c) => {
      const name = (c.display_name ?? '').toLowerCase();
      if (name.includes(q)) return true;
      if ((c.nickname ?? '').toLowerCase().includes(q)) return true;
      if ((c.organization?.company ?? '').toLowerCase().includes(q)) return true;
      if ((c.organization?.title ?? '').toLowerCase().includes(q)) return true;
      if ((c.note ?? '').toLowerCase().includes(q)) return true;
      if ((c.website ?? '').toLowerCase().includes(q)) return true;
      if (c.phones?.some((p) => (p.number ?? '').toLowerCase().includes(q))) return true;
      if (c.emails?.some((e) => (e.address ?? '').toLowerCase().includes(q))) return true;
      return false;
    });
  });

  let selectedContact = $derived.by(() => {
    if (!selectedId) return filteredContacts[0] ?? null;
    return contacts.find((c) => c.contact_id === selectedId) ?? filteredContacts[0] ?? null;
  });

  async function loadContacts(force = false) {
    if (!paired) return;
    loading = true;
    loadProgress = '';
    error = '';
    errorCode = '';
    permissionError = false;
    try {
      const res = await listAllContacts(100, '', (loaded, total) => {
        loadProgress = total > 0 ? `${loaded} of ${total}` : `${loaded}…`;
      }, force);
      if (res.error && res.contacts.length === 0) {
        error = res.error;
        errorCode = res.error_code ?? '';
        permissionError = res.error_code === 'permission_denied' || res.permission === 'contacts';
        // cursor_invalid means the directory generation reset: the
        // backend already fails closed, so retry once from the start.
        if (errorCode === 'cursor_invalid') {
          await loadContacts(true);
          return;
        }
      } else {
        if (res.error) {
          // Partial page error with rows: keep rows, surface banner.
          error = res.error;
          errorCode = res.error_code ?? '';
        }
        contacts = dedupeContacts(res.contacts);
        for (const c of contacts) {
          if (c.avatar_b64) {
            avatars[c.contact_id] = c.avatar_b64;
            if (c.photo_version) avatarVersions[c.contact_id] = c.photo_version;
          } else if (c.photo_version) {
            avatarVersions[c.contact_id] = c.photo_version;
          }
        }
        if (contacts.length > 0 && !selectedId) {
          selectedId = contacts[0].contact_id;
        }
        // Lazily fill visible-list thumbnails (on-demand path was
        // previously never called, so no photo ever displayed).
        void fillMissingAvatars(contacts.slice(0, 60));
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e);
      if (error.toLowerCase().includes('permission') || error.toLowerCase().includes('contacts access')) {
        permissionError = true;
      }
    } finally {
      loading = false;
      loadProgress = '';
    }
  }

  // fillMissingAvatars fetches thumbnails for contacts known to have a
  // photo (photo_version present) but no bytes yet. Bounded + deduped so
  // a 1000-contact directory never fans out 1000 requests at once.
  async function fillMissingAvatars(rows: ContactEntry[]) {
    const missing = rows.filter(
      (c) => !avatars[c.contact_id] && !avatarPending.has(c.contact_id),
    );
    // Without a version hint we cannot tell "no photo" from "not yet
    // fetched": probe only the first few to avoid hammering the phone.
    const probed = missing.filter((c) => c.photo_version).concat(missing.slice(0, 10));
    const batch = [...new Map(probed.map((c) => [c.contact_id, c])).values()].slice(0, 30);
    await Promise.all(
      batch.map(async (c) => {
        avatarPending.add(c.contact_id);
        try {
          const res = await getContactAvatar(c.contact_id, { expectedVersion: c.photo_version });
          if (res.avatar_b64) {
            avatars[c.contact_id] = res.avatar_b64;
            if (res.photo_version) avatarVersions[c.contact_id] = res.photo_version;
            const idx = contacts.findIndex((x) => x.contact_id === c.contact_id);
            if (idx >= 0) {
              contacts[idx] = { ...contacts[idx], avatar_b64: res.avatar_b64, photo_version: res.photo_version };
            }
          }
        } catch {
          // Fail-soft: monogram fallback stays.
        } finally {
          avatarPending.delete(c.contact_id);
        }
      }),
    );
  }

  function avatarFor(c: ContactEntry): string | undefined {
    return c.avatar_b64 || avatars[c.contact_id];
  }

  // dedupeContacts drops repeated contact_ids keeping first-seen order.
  // Overlapping pages must never produce duplicate keyed-each keys
  // (Svelte throws each_key_duplicate).
  function dedupeContacts(rows: ContactEntry[]): ContactEntry[] {
    const seen = new Set<string>();
    return rows.filter((c) => {
      if (seen.has(c.contact_id)) return false;
      seen.add(c.contact_id);
      return true;
    });
  }  // ensureHeaderPhoto fetches the high-res display photo for the selected
  // contact header when only a thumbnail (or nothing) is cached.
  async function ensureHeaderPhoto(c: ContactEntry) {
    if (!c || avatarPending.has(c.contact_id + '#full')) return;
    if (c.avatar_b64 && c.avatar_b64.length > 20000) return; // already detailed
    avatarPending.add(c.contact_id + '#full');
    try {
      const res = await getContactAvatar(c.contact_id, { highRes: true });
      if (res.avatar_b64) {
        avatars[c.contact_id] = res.avatar_b64;
        const idx = contacts.findIndex((x) => x.contact_id === c.contact_id);
        if (idx >= 0) {
          contacts[idx] = { ...contacts[idx], avatar_b64: res.avatar_b64, photo_version: res.photo_version };
        }
      }
    } catch {
      // Keep thumbnail/monogram.
    } finally {
      avatarPending.delete(c.contact_id + '#full');
    }
  }

  function getInitials(name: string | undefined | null): string {
    const parts = (name ?? '').trim().split(/\s+/).filter(Boolean);
    if (parts.length === 0 || !parts[0]) return '?';
    if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
  }

  function getAvatarColor(name: string | undefined | null): string {
    const n = name ?? '';
    // Contact identity stays monochrome so the interface does not become
    // a rainbow of competing accents. Stars and status colors retain meaning.
    const colors = ['bg-altrow text-secondary'];
    let hash = 0;
    for (let i = 0; i < n.length; i++) {
      hash = (hash << 5) - hash + n.charCodeAt(i);
      hash |= 0;
    }
    return colors[Math.abs(hash) % colors.length];
  }

  async function copyText(text: string, fieldId: string) {
    try {
      await navigator.clipboard.writeText(text);
      copiedField = fieldId;
      setTimeout(() => {
        if (copiedField === fieldId) copiedField = '';
      }, 1500);
    } catch {
      // Ignore clipboard write failure
    }
  }

  function handleMessage(phone: ContactPhone, contact: ContactEntry) {
    if (onMessageContact) {
      onMessageContact(phone.number, contact.display_name);
    }
  }

  onMount(() => {
    if (paired) {
      void loadContacts(false);
    }

    const handleRequestDelete = (e: Event) => {
      const custom = e as CustomEvent<{ contactId: string; contactName?: string; lookupKey?: string }>;
      if (!custom.detail?.contactId) return;
      const target = contacts.find((c) => c.contact_id === custom.detail.contactId);
      if (target) {
        promptDeleteContact(target);
      } else {
        promptDeleteContact({
          contact_id: custom.detail.contactId,
          display_name: custom.detail.contactName || 'Contact',
          lookup_key: custom.detail.lookupKey,
        });
      }
    };
    window.addEventListener('request-delete-contact', handleRequestDelete);

    // Subscribe to contacts:changed Wails event (push-driven, no polling)
    let offChanged: (() => void) | null = null;
    try {
      offChanged = Events.On('contacts:changed', () => {
        void loadContacts(false);
      });
    } catch {}
    return () => {
      window.removeEventListener('request-delete-contact', handleRequestDelete);
      try { offChanged?.(); } catch {}
    };
  });

  // Fetch the high-res header photo whenever selection changes.
  $effect(() => {
    const c = selectedContact;
    if (c) {
      untrack(() => void ensureHeaderPhoto(c));
    }
  });

  // Only reload when transition from offline to paired happens, untracked to prevent cyclical effects.
  $effect(() => {
    const isPaired = paired;
    if (isPaired && !prevPaired) {
      untrack(() => void loadContacts(false));
    }
    prevPaired = isPaired;
  });
</script>

<div class="native-pane flex min-h-0 flex-1 flex-col">
  <div class="native-toolbar flex min-h-[52px] flex-none items-center justify-between px-5 py-2">
    <ContentHeader
      title="Contacts"
      subtitle={deviceLabel
        ? `${contacts.length} contacts${loadProgress ? ` · ${loadProgress}` : ''} · ${deviceLabel}`
        : `${contacts.length} contacts${loadProgress ? ` · ${loadProgress}` : ''}`}
      icon={Users}
    />
    {#if paired}
      <button
        onclick={() => loadContacts(true)}
        disabled={loading}
        title="Refresh contacts"
        aria-label="Refresh contacts"
        class="inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-[12px] font-medium text-secondary transition hover:bg-altrow hover:text-label active:bg-active disabled:opacity-50"
      >
        <RefreshCw size={12} class={loading ? 'animate-spin' : ''} aria-hidden="true" />
        <span>{loading ? 'Refreshing…' : 'Refresh'}</span>
      </button>
    {/if}
  </div>

  {#if !paired}
    <div class="flex flex-col items-center px-6 py-16 text-center">
      <Users size={24} class="text-tertiary" aria-hidden="true" />
      <p class="mt-3 text-[13px] font-medium text-label">Phone offline</p>
      <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">
        Reconnect the phone to view and search contacts.
      </p>
    </div>
  {:else if permissionError}
    <div class="flex flex-col items-center px-6 py-16 text-center">
      <AlertCircle size={24} class="text-amber-500" aria-hidden="true" />
      <p class="mt-3 text-[13px] font-medium text-label">Contacts permission needed</p>
      <p class="mt-1 max-w-[42ch] text-[12px] leading-relaxed text-secondary">
        Grant Contacts permission on your Android phone under FuseItAll settings or tap "Grant" on the phone app.
      </p>
      <button
        onclick={() => loadContacts(true)}
        class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md bg-altrow px-3 text-[12px] font-medium text-label transition hover:bg-hover active:bg-active"
      >
        <RefreshCw size={12} aria-hidden="true" />
        <span>Check Again</span>
      </button>
    </div>
  {:else if error && contacts.length === 0}
    <div class="flex flex-col items-center px-6 py-16 text-center">
      <AlertCircle size={24} class="text-rose-500" aria-hidden="true" />
      <p class="mt-3 text-[13px] font-medium text-label">Could not load contacts</p>
      <p class="mt-1 max-w-[42ch] text-[12px] leading-relaxed text-secondary">{error}</p>
      {#if errorCode === 'cursor_invalid'}
        <p class="mt-1 max-w-[42ch] text-[11px] leading-relaxed text-tertiary">Directory changed — retrying from the start usually fixes this.</p>
      {/if}
      <button
        onclick={() => loadContacts(true)}
        class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md bg-altrow px-3 text-[12px] font-medium text-label transition hover:bg-hover active:bg-active"
      >
        <RefreshCw size={12} aria-hidden="true" />
        <span>Try Again</span>
      </button>
    </div>
  {:else}
    {#if error && contacts.length > 0}
      <div class="flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[12px] text-label" role="status">
        <AlertCircle size={13} class="flex-none text-amber-500" aria-hidden="true" />
        <span class="min-w-0 flex-1 truncate">Showing {contacts.length} cached contacts — {error}</span>
        <button onclick={() => loadContacts(true)} class="flex-none font-medium underline">Retry</button>
      </div>
    {/if}
    <!-- 2-Pane Split View: Contacts List on Left, Detail on Right -->
    <div class="native-split flex min-h-0 flex-1 overflow-hidden bg-control">
      <!-- Left List Pane -->
      <div class="native-list flex w-72 flex-none flex-col bg-altrow/45 px-2 pb-2">
        <!-- Search bar -->
        <div class="px-1 pb-2 pt-2">
          <div class="relative">
            <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-tertiary" aria-hidden="true" />
            <input
              bind:value={query}
              type="text"
              placeholder="Search contacts…"
              aria-label="Search contacts"
              class="h-7 w-full rounded-md bg-window/70 pl-8 pr-2 text-[12px] text-label placeholder:text-tertiary transition focus:bg-window"
            />
          </div>
        </div>

        <!-- Scrollable contact rows -->
        <div class="flex-1 overflow-y-auto py-1">
          {#if filteredContacts.length === 0}
            <div class="px-4 py-8 text-center text-[12px] text-tertiary">
              {query ? 'No matching contacts' : 'No contacts on phone'}
            </div>
          {:else}
            {#each filteredContacts as contact (contact.contact_id)}
              {@const isSelected = selectedContact?.contact_id === contact.contact_id}
              <button
                type="button"
                data-menu="contact"
                data-contact-id={contact.contact_id}
                data-contact-lookup-key={contact.lookup_key || ''}
                data-contact-name={contact.display_name || ''}
                data-contact-number={contact.phones?.[0]?.number || ''}
                onclick={() => (selectedId = contact.contact_id)}
                class="flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left transition-colors {isSelected
                  ? 'bg-accent text-accent-text font-medium'
                  : 'text-label hover:bg-hover'}"
              >
                <!-- Avatar circle or image -->
                {#if avatarFor(contact)}
                  <img
                    src="data:image/jpeg;base64,{avatarFor(contact)}"
                    alt={contact.display_name}
                    class="h-7 w-7 flex-none rounded-full object-cover"
                    loading="lazy"
                  />
                {:else}
                  <div
                    class="flex h-7 w-7 flex-none items-center justify-center rounded-full text-[11px] font-semibold {isSelected
                      ? 'bg-accent-text/15 text-accent-text'
                      : getAvatarColor(contact.display_name)}"
                  >
                    {getInitials(contact.display_name)}
                  </div>
                {/if}

                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-1">
                    <span class="truncate text-[13px]">{contact.display_name || 'Unnamed'}</span>
                    {#if contact.starred}
                      <Star size={11} class="flex-none fill-amber-400 text-amber-400" aria-hidden="true" />
                    {/if}
                  </div>
                  {#if contact.phones?.[0]?.number}
                    <div class="truncate text-[11px] {isSelected ? 'text-accent-text/75' : 'text-secondary'}">
                      {contact.phones[0].number}
                    </div>
                  {/if}
                </div>
              </button>
            {/each}
          {/if}
        </div>
      </div>

      <!-- Right Detail Pane -->
      <div class="flex flex-1 flex-col overflow-y-auto bg-window px-8 py-6">
        {#if selectedContact}
          <!-- Contact header card -->
          <div class="flex items-start gap-4 pb-6">
            {#if avatarFor(selectedContact)}
              <img
                src="data:image/jpeg;base64,{avatarFor(selectedContact)}"
                alt={selectedContact.display_name}
                class="h-16 w-16 flex-none rounded-full object-cover shadow-[0_6px_18px_rgba(0,0,0,0.12)]"
              />
            {:else}
              <div
                class="flex h-16 w-16 flex-none items-center justify-center rounded-full text-[20px] font-semibold {getAvatarColor(
                  selectedContact.display_name,
                )}"
              >
                {getInitials(selectedContact.display_name)}
              </div>
            {/if}

            <div class="min-w-0 flex-1 pt-1">
              <div class="flex items-center gap-2">
                <h2 class="truncate text-[18px] font-semibold text-label">
                  {selectedContact.display_name || 'Unnamed'}
                </h2>
                {#if selectedContact.starred}
                  <Star size={14} class="fill-amber-400 text-amber-400" aria-label="Starred contact" />
                {/if}
              </div>

              <!-- Quick action bar -->
              <div class="mt-3 flex items-center gap-2">
                {#if selectedContact.phones?.[0]}
                  <button
                    onclick={() => handleMessage(selectedContact!.phones![0], selectedContact!)}
                    class="inline-flex h-7 items-center gap-1.5 rounded-md bg-accent px-3 text-[12px] font-medium text-accent-text shadow-sm hover:opacity-90 active:opacity-100"
                  >
                    <MessageSquare size={13} aria-hidden="true" />
                    <span>Message</span>
                  </button>
                  <button
                    onclick={() => copyText(selectedContact!.phones![0].number, 'header-phone')}
                    class="inline-flex h-7 items-center gap-1.5 rounded-md bg-altrow px-2.5 text-[12px] font-medium text-label transition hover:bg-hover active:bg-active"
                  >
                    {#if copiedField === 'header-phone'}
                      <Check size={12} class="text-emerald-500" aria-hidden="true" />
                      <span class="text-emerald-600 dark:text-emerald-400">Copied</span>
                    {:else}
                      <Copy size={12} aria-hidden="true" />
                      <span>Copy Number</span>
                    {/if}
                  </button>
                {/if}
                <button
                  type="button"
                  onclick={() => promptDeleteContact(selectedContact!)}
                  title="Delete contact"
                  aria-label="Delete contact"
                  class="inline-flex h-7 items-center gap-1.5 rounded-md bg-altrow px-2.5 text-[12px] font-medium text-destructive transition hover:bg-destructive/10 active:bg-destructive/20"
                >
                  <Trash2 size={12} aria-hidden="true" />
                  <span>Delete…</span>
                </button>
              </div>
            </div>
          </div>

          <!-- Contact details: Phone numbers -->
          <div class="mt-6 space-y-4">
            <div>
              <h3 class="text-[11px] font-semibold uppercase tracking-wider text-tertiary">Phone Numbers</h3>
              {#if selectedContact.phones && selectedContact.phones.length > 0}
                <div class="mt-2 divide-y divide-separator rounded-lg bg-altrow/50">
                  {#each selectedContact.phones as phone, idx (phone.number + idx)}
                    <div class="flex items-center justify-between px-3 py-2.5">
                      <div class="flex items-center gap-3">
                        <Phone size={14} class="text-tertiary" aria-hidden="true" />
                        <div>
                          <div class="text-[13px] font-medium text-label">{phone.number}</div>
                          <div class="text-[11px] text-tertiary capitalize">{phone.label || phone.type || 'phone'}</div>
                        </div>
                      </div>
                      <div class="flex items-center gap-1.5">
                        <button
                          onclick={() => handleMessage(phone, selectedContact!)}
                          title="Send SMS"
                          class="rounded p-1.5 text-secondary hover:bg-hover hover:text-label"
                        >
                          <MessageSquare size={14} aria-hidden="true" />
                        </button>
                        <button
                          onclick={() => copyText(phone.number, `phone-${idx}`)}
                          title="Copy phone number"
                          class="rounded p-1.5 text-secondary hover:bg-hover hover:text-label"
                        >
                          {#if copiedField === `phone-${idx}`}
                            <Check size={14} class="text-emerald-500" aria-hidden="true" />
                          {:else}
                            <Copy size={14} aria-hidden="true" />
                          {/if}
                        </button>
                      </div>
                    </div>
                  {/each}
                </div>
              {:else}
                <p class="mt-2 text-[12px] text-secondary">No phone numbers listed</p>
              {/if}
            </div>

            <!-- Email addresses -->
            <div>
              <h3 class="text-[11px] font-semibold uppercase tracking-wider text-tertiary">Email Addresses</h3>
              {#if selectedContact.emails && selectedContact.emails.length > 0}
                <div class="mt-2 divide-y divide-separator rounded-lg bg-altrow/50">
                  {#each selectedContact.emails as email, idx (email.address + idx)}
                    <div class="flex items-center justify-between px-3 py-2.5">
                      <div class="flex items-center gap-3">
                        <Mail size={14} class="text-tertiary" aria-hidden="true" />
                        <div>
                          <div class="text-[13px] font-medium text-label">{email.address}</div>
                          <div class="text-[11px] text-tertiary capitalize">{email.label || email.type || 'email'}</div>
                        </div>
                      </div>
                      <button
                        onclick={() => copyText(email.address, `email-${idx}`)}
                        title="Copy email address"
                        class="rounded p-1.5 text-secondary hover:bg-hover hover:text-label"
                      >
                        {#if copiedField === `email-${idx}`}
                          <Check size={14} class="text-emerald-500" aria-hidden="true" />
                        {:else}
                          <Copy size={14} aria-hidden="true" />
                        {/if}
                      </button>
                    </div>
                  {/each}
                </div>
              {:else}
                <p class="mt-2 text-[12px] text-secondary">No email addresses listed</p>
              {/if}
            </div>

            <!-- Extended details: org, birthday, address, note, website -->
            {#if selectedContact.nickname || selectedContact.organization?.company || selectedContact.organization?.title || selectedContact.birthday_ms || selectedContact.anniversary_ms || selectedContact.postal?.formatted || selectedContact.note || selectedContact.website}
              <div>
                <h3 class="text-[11px] font-semibold uppercase tracking-wider text-tertiary">Details</h3>
                <div class="mt-2 space-y-2 rounded-lg bg-altrow/50 px-3 py-2.5">
                  {#if selectedContact.nickname}
                    <div class="text-[13px] text-label"><span class="text-tertiary">Nickname · </span>{selectedContact.nickname}</div>
                  {/if}
                  {#if selectedContact.organization?.company || selectedContact.organization?.title}
                    <div class="text-[13px] text-label">
                      <span class="text-tertiary">Work · </span>{[selectedContact.organization?.title, selectedContact.organization?.company]
                        .filter(Boolean)
                        .join(' at ')}
                      {#if selectedContact.organization?.department}
                        <span class="text-secondary"> ({selectedContact.organization.department})</span>
                      {/if}
                    </div>
                  {/if}
                  {#if selectedContact.birthday_ms}
                    <div class="text-[13px] text-label">
                      <span class="text-tertiary">Birthday · </span>{new Date(selectedContact.birthday_ms).toLocaleDateString([], {
                        month: 'long',
                        day: 'numeric',
                        year: new Date(selectedContact.birthday_ms).getFullYear() > 1971 ? 'numeric' : undefined,
                      })}
                    </div>
                  {/if}
                  {#if selectedContact.anniversary_ms}
                    <div class="text-[13px] text-label">
                      <span class="text-tertiary">Anniversary · </span>{new Date(
                        selectedContact.anniversary_ms,
                      ).toLocaleDateString([], { month: 'long', day: 'numeric', year: 'numeric' })}
                    </div>
                  {/if}
                  {#if selectedContact.postal?.formatted}
                    <div class="flex items-start justify-between gap-2">
                      <div class="text-[13px] text-label">
                        <span class="text-tertiary">Address · </span>{selectedContact.postal.formatted}
                      </div>
                      <button
                        onclick={() => copyText(selectedContact!.postal!.formatted!, 'postal')}
                        title="Copy address"
                        class="rounded p-1.5 text-secondary hover:bg-hover hover:text-label"
                      >
                        {#if copiedField === 'postal'}
                          <Check size={14} class="text-emerald-500" aria-hidden="true" />
                        {:else}
                          <Copy size={14} aria-hidden="true" />
                        {/if}
                      </button>
                    </div>
                  {/if}
                  {#if selectedContact.website}
                    <div class="flex items-start justify-between gap-2">
                      <div class="truncate text-[13px] text-label">
                        <span class="text-tertiary">Web · </span>{selectedContact.website}
                      </div>
                      <button
                        onclick={() => copyText(selectedContact!.website!, 'website')}
                        title="Copy website"
                        class="rounded p-1.5 text-secondary hover:bg-hover hover:text-label"
                      >
                        {#if copiedField === 'website'}
                          <Check size={14} class="text-emerald-500" aria-hidden="true" />
                        {:else}
                          <Copy size={14} aria-hidden="true" />
                        {/if}
                      </button>
                    </div>
                  {/if}
                  {#if selectedContact.note}
                    <div class="text-[12px] leading-relaxed text-secondary">{selectedContact.note}</div>
                  {/if}
                </div>
              </div>
            {/if}
          </div>
        {:else}
          <div class="flex flex-1 flex-col items-center justify-center text-center">
            <Users size={32} class="text-tertiary" aria-hidden="true" />
            <p class="mt-3 text-[13px] font-medium text-label">No contact selected</p>
            <p class="mt-1 text-[12px] text-secondary">Choose a contact from the list on the left.</p>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  <ConfirmDialog
    open={!!contactToDelete}
    title="Delete {contactToDelete?.display_name || 'Contact'}?"
    body="This contact will be permanently deleted from your phone. This action cannot be undone."
    confirmLabel="Delete"
    cancelLabel="Keep"
    destructive={true}
    busy={deleteBusy}
    onConfirm={confirmDeleteContact}
    onCancel={cancelDeleteContact}
  />
</div>
