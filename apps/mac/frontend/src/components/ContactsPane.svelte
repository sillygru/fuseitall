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
  import { onMount } from 'svelte';
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
  } from '@lucide/svelte';
  import ContentHeader from './ContentHeader.svelte';
  import {
    listContacts,
    searchContacts,
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
  let error = $state('');
  let permissionError = $state(false);
  let contacts = $state<ContactEntry[]>([]);
  let selectedId = $state<string>('');
  let copiedField = $state<string>('');

  let filteredContacts = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return contacts;
    return contacts.filter((c) => {
      if (c.display_name.toLowerCase().includes(q)) return true;
      if (c.phones?.some((p) => p.number.toLowerCase().includes(q))) return true;
      if (c.emails?.some((e) => e.address.toLowerCase().includes(q))) return true;
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
    error = '';
    permissionError = false;
    try {
      const res = await listContacts('', 100, force);
      if (res.error) {
        error = res.error;
        permissionError = res.error_code === 'permission_denied' || res.permission === 'contacts';
      } else {
        contacts = res.contacts;
        if (contacts.length > 0 && !selectedId) {
          selectedId = contacts[0].contact_id;
        }
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e);
      if (error.toLowerCase().includes('permission') || error.toLowerCase().includes('contacts access')) {
        permissionError = true;
      }
    } finally {
      loading = false;
    }
  }

  function getInitials(name: string): string {
    const parts = name.trim().split(/\s+/);
    if (parts.length === 0 || !parts[0]) return '?';
    if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
  }

  function getAvatarColor(name: string): string {
    const colors = [
      'bg-blue-500/20 text-blue-600 dark:text-blue-400',
      'bg-indigo-500/20 text-indigo-600 dark:text-indigo-400',
      'bg-emerald-500/20 text-emerald-600 dark:text-emerald-400',
      'bg-amber-500/20 text-amber-600 dark:text-amber-400',
      'bg-rose-500/20 text-rose-600 dark:text-rose-400',
      'bg-purple-500/20 text-purple-600 dark:text-purple-400',
      'bg-teal-500/20 text-teal-600 dark:text-teal-400',
    ];
    let hash = 0;
    for (let i = 0; i < name.length; i++) {
      hash = (hash << 5) - hash + name.charCodeAt(i);
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

    // Subscribe to contacts:changed Wails event (push-driven, no polling)
    const win = window as unknown as {
      runtime?: { EventsOn?: (evt: string, cb: () => void) => () => void };
    };
    if (typeof win.runtime?.EventsOn === 'function') {
      const unsub = win.runtime.EventsOn('contacts:changed', () => {
        void loadContacts(false);
      });
      return () => {
        if (typeof unsub === 'function') unsub();
      };
    }
  });

  $effect(() => {
    if (paired && contacts.length === 0 && !loading && !error) {
      void loadContacts(false);
    }
  });
</script>

<div class="flex min-h-0 flex-1 flex-col gap-3">
  <div class="flex items-center justify-between">
    <ContentHeader
      title="Contacts"
      subtitle={deviceLabel ? `${contacts.length} contacts · ${deviceLabel}` : `${contacts.length} contacts`}
      icon={Users}
    />
    {#if paired}
      <button
        onclick={() => loadContacts(true)}
        disabled={loading}
        title="Refresh contacts"
        aria-label="Refresh contacts"
        class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-control px-2.5 text-[12px] font-medium text-label hover:bg-hover active:bg-active disabled:opacity-50"
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
        class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-control px-3 text-[12px] font-medium text-label hover:bg-hover"
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
      <button
        onclick={() => loadContacts(true)}
        class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-control px-3 text-[12px] font-medium text-label hover:bg-hover"
      >
        <RefreshCw size={12} aria-hidden="true" />
        <span>Try Again</span>
      </button>
    </div>
  {:else}
    <!-- 2-Pane Split View: Contacts List on Left, Detail on Right -->
    <div class="flex min-h-0 flex-1 overflow-hidden rounded-lg border border-separator bg-control">
      <!-- Left List Pane -->
      <div class="flex w-72 flex-none flex-col border-r border-separator bg-window/50">
        <!-- Search bar -->
        <div class="border-b border-separator p-2">
          <div class="relative">
            <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-tertiary" aria-hidden="true" />
            <input
              bind:value={query}
              type="text"
              placeholder="Search contacts…"
              aria-label="Search contacts"
              class="h-7 w-full rounded-md border border-separator bg-altrow pl-8 pr-2 text-[12px] text-label placeholder:text-tertiary focus:outline-none focus:ring-1 focus:ring-focus"
            />
          </div>
        </div>

        <!-- Scrollable contact rows -->
        <div class="flex-1 overflow-y-auto p-1">
          {#if filteredContacts.length === 0}
            <div class="px-4 py-8 text-center text-[12px] text-tertiary">
              {query ? 'No matching contacts' : 'No contacts on phone'}
            </div>
          {:else}
            {#each filteredContacts as contact (contact.contact_id)}
              {@const isSelected = selectedContact?.contact_id === contact.contact_id}
              <button
                type="button"
                onclick={() => (selectedId = contact.contact_id)}
                class="flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left transition-colors {isSelected
                  ? 'bg-accent text-white font-medium'
                  : 'text-label hover:bg-hover'}"
              >
                <!-- Avatar circle or image -->
                {#if contact.avatar_b64}
                  <img
                    src="data:image/jpeg;base64,{contact.avatar_b64}"
                    alt={contact.display_name}
                    class="h-7 w-7 flex-none rounded-full object-cover"
                  />
                {:else}
                  <div
                    class="flex h-7 w-7 flex-none items-center justify-center rounded-full text-[11px] font-semibold {isSelected
                      ? 'bg-white/20 text-white'
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
                    <div class="truncate text-[11px] {isSelected ? 'text-white/80' : 'text-secondary'}">
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
      <div class="flex flex-1 flex-col overflow-y-auto p-6">
        {#if selectedContact}
          <!-- Contact header card -->
          <div class="flex items-start gap-4 border-b border-separator pb-6">
            {#if selectedContact.avatar_b64}
              <img
                src="data:image/jpeg;base64,{selectedContact.avatar_b64}"
                alt={selectedContact.display_name}
                class="h-16 w-16 flex-none rounded-full object-cover ring-1 ring-separator"
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
              {#if selectedContact.phones?.[0]}
                <div class="mt-3 flex items-center gap-2">
                  <button
                    onclick={() => handleMessage(selectedContact!.phones![0], selectedContact!)}
                    class="inline-flex h-7 items-center gap-1.5 rounded-md bg-accent px-3 text-[12px] font-medium text-white shadow-sm hover:opacity-90 active:opacity-100"
                  >
                    <MessageSquare size={13} aria-hidden="true" />
                    <span>Message</span>
                  </button>
                  <button
                    onclick={() => copyText(selectedContact!.phones![0].number, 'header-phone')}
                    class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-control px-2.5 text-[12px] font-medium text-label hover:bg-hover active:bg-active"
                  >
                    {#if copiedField === 'header-phone'}
                      <Check size={12} class="text-emerald-500" aria-hidden="true" />
                      <span class="text-emerald-600 dark:text-emerald-400">Copied</span>
                    {:else}
                      <Copy size={12} aria-hidden="true" />
                      <span>Copy Number</span>
                    {/if}
                  </button>
                </div>
              {/if}
            </div>
          </div>

          <!-- Contact details: Phone numbers -->
          <div class="mt-6 space-y-4">
            <div>
              <h3 class="text-[11px] font-semibold uppercase tracking-wider text-tertiary">Phone Numbers</h3>
              {#if selectedContact.phones && selectedContact.phones.length > 0}
                <div class="mt-2 divide-y divide-separator rounded-lg border border-separator bg-altrow/50">
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
                <div class="mt-2 divide-y divide-separator rounded-lg border border-separator bg-altrow/50">
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
</div>
