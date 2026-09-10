<!--
  Copyright (C) 2026 FuseItAll contributors.

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published
  by the Free Software Foundation, version 3 of the License. See LICENSE
  for details.

  Messages pane: macOS Messages-style 2-pane split view.
  Left: conversation threads with unread indicators and search.
  Right: message transcript with speech bubbles and compose bar to send SMS through phone.
-->
<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import {
    MessageSquare,
    Search,
    RefreshCw,
    Send,
    SquarePen,
    AlertCircle,
    User,
    ArrowLeft,
  } from '@lucide/svelte';
  import ContentHeader from './ContentHeader.svelte';
  import {
    listAllSMSThreads,
    listSMSMessages,
    sendSMS,
    getContactAvatar,
    type SMSThread,
    type SMSMessage,
  } from '../contacts_messages_api';

  interface Props {
    paired: boolean;
    deviceLabel: string;
    initialRecipient?: string;
    initialDisplayName?: string;
  }

  let { paired, deviceLabel, initialRecipient, initialDisplayName }: Props = $props();

  let query = $state('');
  let loadingThreads = $state(false);
  let loadingMessages = $state(false);
  let sending = $state(false);
  let error = $state('');
  let permissionError = $state(false);
  let threads = $state<SMSThread[]>([]);
  let threadAvatars = $state<Record<string, string>>({});
  let threadAvatarPending = new Set<string>();
  let selectedThreadId = $state<number | null>(null);
  let currentMessages = $state<SMSMessage[]>([]);
  let composeText = $state('');
  let isComposingNew = $state(false);
  let newRecipient = $state('');
  let messagesContainer = $state<HTMLDivElement | null>(null);
  let prevPaired = $state(paired);

  let filteredThreads = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return threads;
    return threads.filter((t) => {
      const name = (t.contact_name ?? '').toLowerCase();
      const addr = (t.address ?? '').toLowerCase();
      const snip = (t.snippet ?? '').toLowerCase();
      if (name.includes(q)) return true;
      if (addr.includes(q)) return true;
      if (snip.includes(q)) return true;
      return false;
    });
  });

  let activeThread = $derived.by(() => {
    if (selectedThreadId === null) return null;
    return threads.find((t) => t.thread_id === selectedThreadId) ?? null;
  });

  async function loadThreads(force = false) {
    if (!paired) return;
    loadingThreads = true;
    error = '';
    permissionError = false;
    try {
      const res = await listAllSMSThreads(50, (n) => {
        // progress is implicit via list length; no extra state needed
        void n;
      });
      if (res.error && res.threads.length === 0) {
        error = res.error;
        permissionError = res.error_code === 'permission_denied' || res.permission === 'sms';
        if (res.error_code === 'cursor_invalid') {
          await loadThreads(true);
          return;
        }
      } else {
        if (res.error) error = res.error;
        threads = dedupeThreads(res.threads ?? []);
        if (!selectedThreadId && threads.length > 0 && !isComposingNew) {
          selectThread(threads[0].thread_id);
        }
        void fillThreadAvatars(threads.slice(0, 40));
      }
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e);
      if (error.toLowerCase().includes('permission') || error.toLowerCase().includes('sms')) {
        permissionError = true;
      }
    } finally {
      loadingThreads = false;
    }
  }

  function threadAvatarKey(t: SMSThread): string {
    return t.contact_id || t.address;
  }

  // dedupeThreads drops repeated thread_ids keeping first-seen order.
  // Overlapping pages or a push landing mid-paging must never produce
  // duplicate keyed-each keys (Svelte throws each_key_duplicate).
  function dedupeThreads(rows: SMSThread[]): SMSThread[] {
    const seen = new Set<number>();
    return rows.filter((t) => {
      if (seen.has(t.thread_id)) return false;
      seen.add(t.thread_id);
      return true;
    });
  }

  // fillThreadAvatars fetches contact photos on demand via contact_id +
  // photo_version (chosen strategy: small wire, reuse contacts LRU).
  // Fail-soft to the generic User icon when unknown or denied.
  async function fillThreadAvatars(rows: SMSThread[]) {
    const batch = rows
      .filter((t) => t.contact_id && !threadAvatars[threadAvatarKey(t)] && !threadAvatarPending.has(threadAvatarKey(t)))
      .slice(0, 30);
    await Promise.all(
      batch.map(async (t) => {
        const key = threadAvatarKey(t);
        threadAvatarPending.add(key);
        try {
          const res = await getContactAvatar(t.contact_id!, { expectedVersion: t.photo_version });
          if (res.avatar_b64) threadAvatars[key] = res.avatar_b64;
        } catch {
          // Keep generic icon.
        } finally {
          threadAvatarPending.delete(key);
        }
      }),
    );
  }

  async function selectThread(threadId: number, force = false) {
    selectedThreadId = threadId;
    isComposingNew = false;
    loadingMessages = true;
    try {
      const res = await listSMSMessages(threadId, '', 100, force);
      currentMessages = (res?.messages ?? []).slice().sort((a, b) => (a.date ?? 0) - (b.date ?? 0));
      await tick();
      scrollToBottom();
    } catch (e: unknown) {
      error = e instanceof Error ? e.message : String(e);
    } finally {
      loadingMessages = false;
    }
  }

  function scrollToBottom() {
    if (messagesContainer) {
      messagesContainer.scrollTop = messagesContainer.scrollHeight;
    }
  }

  async function handleSend() {
    const text = composeText.trim();
    if (!text || sending) return;

    let targetAddress = '';
    if (isComposingNew) {
      targetAddress = newRecipient.trim();
      if (!targetAddress) return;
    } else if (activeThread) {
      targetAddress = activeThread.address;
    } else {
      return;
    }

    sending = true;
    try {
      const res = await sendSMS(targetAddress, text);
      if (res.ok) {
        composeText = '';
        if (isComposingNew && res.thread_id) {
          isComposingNew = false;
          newRecipient = '';
          await loadThreads(true);
          selectThread(res.thread_id, true);
        } else if (selectedThreadId) {
          // Optimistically append outgoing message locally
          const outgoing: SMSMessage = {
            id: res.message_id ?? Date.now(),
            thread_id: selectedThreadId,
            address: targetAddress,
            body: text,
            date: Date.now(),
            type: 2, // sent
            read: true,
          };
          currentMessages = [...currentMessages, outgoing];
          await tick();
          scrollToBottom();
        }
      } else {
        alert(`Failed to send SMS: ${res.error || 'Unknown error'}`);
      }
    } catch (e: unknown) {
      alert(`Send error: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      sending = false;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void handleSend();
    }
  }

  function startNewConversation(address = '', name = '') {
    isComposingNew = true;
    selectedThreadId = null;
    currentMessages = [];
    newRecipient = address;
    composeText = '';
  }

  function formatTime(timestamp: number): string {
    if (!timestamp) return '';
    const date = new Date(timestamp);
    const now = new Date();
    const isToday =
      date.getDate() === now.getDate() &&
      date.getMonth() === now.getMonth() &&
      date.getFullYear() === now.getFullYear();

    if (isToday) {
      return date.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    }
    return date.toLocaleDateString([], { month: 'short', day: 'numeric' });
  }

  onMount(() => {
    if (paired) {
      void loadThreads(false);
    }

    if (initialRecipient) {
      startNewConversation(initialRecipient, initialDisplayName);
    }

    // Subscribe to messages:changed Wails event (push-driven)
    const win = window as unknown as {
      runtime?: { EventsOn?: (evt: string, cb: () => void) => () => void };
    };
    if (typeof win.runtime?.EventsOn === 'function') {
      const unsub = win.runtime.EventsOn('messages:changed', () => {
        void loadThreads(false);
        if (selectedThreadId) {
          void selectThread(selectedThreadId, false);
        }
      });
      return () => {
        if (typeof unsub === 'function') unsub();
      };
    }
  });

  // Only reload when transition from offline to paired happens, untracked to prevent cyclical effects.
  $effect(() => {
    const isPaired = paired;
    if (isPaired && !prevPaired) {
      untrack(() => void loadThreads(false));
    }
    prevPaired = isPaired;
  });

  // Handle external recipient navigation (e.g. from Contacts pane)
  $effect(() => {
    const recipient = initialRecipient;
    const name = initialDisplayName;
    if (recipient) {
      untrack(() => {
        startNewConversation(recipient, name);
      });
    }
  });
</script>

<div class="flex min-h-0 flex-1 flex-col gap-3">
  <div class="flex items-center justify-between">
    <ContentHeader
      title="Messages"
      subtitle={deviceLabel ? `SMS conversations · ${deviceLabel}` : 'SMS conversations'}
      icon={MessageSquare}
    />
    {#if paired}
      <div class="flex items-center gap-2">
        <button
          onclick={() => startNewConversation()}
          title="New Message"
          aria-label="New Message"
          class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-control px-2.5 text-[12px] font-medium text-label hover:bg-hover active:bg-active"
        >
          <SquarePen size={13} aria-hidden="true" />
          <span>New</span>
        </button>
        <button
          onclick={() => loadThreads(true)}
          disabled={loadingThreads}
          title="Refresh messages"
          aria-label="Refresh messages"
          class="inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-control px-2.5 text-[12px] font-medium text-label hover:bg-hover active:bg-active disabled:opacity-50"
        >
          <RefreshCw size={12} class={loadingThreads ? 'animate-spin' : ''} aria-hidden="true" />
          <span>{loadingThreads ? 'Refreshing…' : 'Refresh'}</span>
        </button>
      </div>
    {/if}
  </div>

  {#if !paired}
    <div class="flex flex-col items-center px-6 py-16 text-center">
      <MessageSquare size={24} class="text-tertiary" aria-hidden="true" />
      <p class="mt-3 text-[13px] font-medium text-label">Phone offline</p>
      <p class="mt-1 max-w-[36ch] text-[12px] leading-relaxed text-secondary">
        Reconnect your phone to send and view SMS messages.
      </p>
    </div>
  {:else if permissionError}
    <div class="flex flex-col items-center px-6 py-16 text-center">
      <AlertCircle size={24} class="text-amber-500" aria-hidden="true" />
      <p class="mt-3 text-[13px] font-medium text-label">SMS permission needed</p>
      <p class="mt-1 max-w-[42ch] text-[12px] leading-relaxed text-secondary">
        Allow SMS permissions on your Android phone under FuseItAll settings or tap "Grant" on the phone app.
      </p>
      <button
        onclick={() => loadThreads(true)}
        class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-control px-3 text-[12px] font-medium text-label hover:bg-hover"
      >
        <RefreshCw size={12} aria-hidden="true" />
        <span>Check Again</span>
      </button>
    </div>
  {:else if error && threads.length === 0}
    <div class="flex flex-col items-center px-6 py-16 text-center">
      <AlertCircle size={24} class="text-rose-500" aria-hidden="true" />
      <p class="mt-3 text-[13px] font-medium text-label">Could not load messages</p>
      <p class="mt-1 max-w-[42ch] text-[12px] leading-relaxed text-secondary">{error}</p>
      <button
        onclick={() => loadThreads(true)}
        class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md border border-separator bg-control px-3 text-[12px] font-medium text-label hover:bg-hover"
      >
        <RefreshCw size={12} aria-hidden="true" />
        <span>Try Again</span>
      </button>
    </div>
  {:else}
    <!-- 2-Pane Split: Threads on Left, Messages Transcript on Right -->
    <div class="flex min-h-0 flex-1 overflow-hidden rounded-lg border border-separator bg-control">
      <!-- Left: Thread List -->
      <div class="flex w-72 flex-none flex-col border-r border-separator bg-window/50">
        <!-- Search bar -->
        <div class="border-b border-separator p-2">
          <div class="relative">
            <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-tertiary" aria-hidden="true" />
            <input
              bind:value={query}
              type="text"
              placeholder="Search conversations…"
              aria-label="Search conversations"
              class="h-7 w-full rounded-md border border-separator bg-altrow pl-8 pr-2 text-[12px] text-label placeholder:text-tertiary focus:outline-none focus:ring-1 focus:ring-focus"
            />
          </div>
        </div>

        <!-- Threads list -->
        <div class="flex-1 overflow-y-auto p-1">
          {#if filteredThreads.length === 0}
            <div class="px-4 py-8 text-center text-[12px] text-tertiary">
              {query ? 'No matching conversations' : 'No messages on phone'}
            </div>
          {:else}
            {#each filteredThreads as thread (thread.thread_id)}
              {@const isSelected = selectedThreadId === thread.thread_id}
              <button
                type="button"
                onclick={() => selectThread(thread.thread_id)}
                class="flex w-full items-start gap-2.5 rounded-md px-2.5 py-2 text-left transition-colors {isSelected
                  ? 'bg-accent text-white font-medium'
                  : 'text-label hover:bg-hover'}"
              >
                <!-- Avatar: contact photo on demand, generic icon fallback -->
                {#if threadAvatars[thread.contact_id || thread.address]}
                  <img
                    src="data:image/jpeg;base64,{threadAvatars[thread.contact_id || thread.address]}"
                    alt={thread.contact_name || thread.address}
                    class="h-8 w-8 flex-none rounded-full object-cover"
                    loading="lazy"
                  />
                {:else}
                  <div
                    class="flex h-8 w-8 flex-none items-center justify-center rounded-full text-[12px] font-semibold {isSelected
                      ? 'bg-white/20 text-white'
                      : 'bg-separator text-secondary'}"
                  >
                    <User size={15} aria-hidden="true" />
                  </div>
                {/if}

                <div class="min-w-0 flex-1">
                  <div class="flex items-baseline justify-between gap-1">
                    <span class="truncate text-[13px] font-semibold">
                      {thread.contact_name || thread.address}
                    </span>
                    <span class="flex-none text-[10px] {isSelected ? 'text-white/80' : 'text-tertiary'}">
                      {formatTime(thread.date)}
                    </span>
                  </div>

                  <div class="mt-0.5 flex items-center justify-between gap-2">
                    <p class="truncate text-[11px] {isSelected ? 'text-white/90' : 'text-secondary'}">
                      {thread.snippet || '(Empty message)'}
                    </p>
                    {#if thread.unread_count && thread.unread_count > 0 && !isSelected}
                      <span class="flex h-4 min-w-[16px] flex-none items-center justify-center rounded-full bg-accent px-1 text-[9px] font-bold text-white">
                        {thread.unread_count}
                      </span>
                    {/if}
                  </div>
                </div>
              </button>
            {/each}
          {/if}
        </div>
      </div>

      <!-- Right: Transcript & Compose -->
      <div class="flex flex-1 flex-col bg-window/30">
        {#if isComposingNew}
          <!-- New Message Header -->
          <div class="flex items-center gap-2 border-b border-separator px-4 py-2.5 bg-control/40">
            <span class="text-[12px] font-medium text-secondary">To:</span>
            <input
              bind:value={newRecipient}
              type="text"
              placeholder="Enter phone number or contact…"
              autofocus
              class="h-7 flex-1 bg-transparent text-[13px] text-label placeholder:text-tertiary focus:outline-none"
            />
          </div>

          <div class="flex flex-1 items-center justify-center text-center p-6">
            <p class="text-[12px] text-secondary">Type a recipient and write your message below.</p>
          </div>
        {:else if activeThread}
          <!-- Conversation Header -->
          <div class="flex items-center justify-between border-b border-separator px-4 py-2 bg-control/40">
            <div class="min-w-0">
              <h3 class="truncate text-[13px] font-semibold text-label">
                {activeThread.contact_name || activeThread.address}
              </h3>
              {#if activeThread.contact_name}
                <p class="truncate text-[11px] text-secondary">{activeThread.address}</p>
              {/if}
            </div>
            <div class="text-[11px] text-tertiary">
              {activeThread.message_count} messages
            </div>
          </div>

          <!-- Message Bubbles Transcript -->
          <div
            bind:this={messagesContainer}
            class="flex-1 overflow-y-auto p-4 space-y-3"
          >
            {#if loadingMessages && currentMessages.length === 0}
              <div class="flex justify-center py-8">
                <RefreshCw size={16} class="animate-spin text-tertiary" />
              </div>
            {:else if currentMessages.length === 0}
              <div class="flex justify-center py-8 text-[12px] text-tertiary">
                No messages in this conversation.
              </div>
            {:else}
              {#each currentMessages as msg (msg.id + '-' + msg.date)}
                {@const isMe = msg.type === 2}
                <div class="flex flex-col {isMe ? 'items-end' : 'items-start'}">
                  <!-- Bubble -->
                  <div
                    class="max-w-[70%] rounded-2xl px-3.5 py-2 text-[13px] leading-relaxed shadow-sm {isMe
                      ? 'bg-accent text-white rounded-br-sm'
                      : 'bg-control text-label border border-separator rounded-bl-sm'}"
                  >
                    {msg.body}
                  </div>
                  <!-- Timestamp -->
                  <span class="mt-1 px-1 text-[10px] text-tertiary">
                    {formatTime(msg.date)}
                  </span>
                </div>
              {/each}
            {/if}
          </div>
        {:else}
          <div class="flex flex-1 flex-col items-center justify-center p-6 text-center">
            <MessageSquare size={32} class="text-tertiary" aria-hidden="true" />
            <p class="mt-3 text-[13px] font-medium text-label">No conversation selected</p>
            <p class="mt-1 text-[12px] text-secondary">
              Select a thread from the left or click "New" to compose a message.
            </p>
          </div>
        {/if}

        <!-- Compose Bar (Active for both existing thread and new message) -->
        {#if activeThread || isComposingNew}
          <div class="border-t border-separator p-3 bg-control/60">
            <div class="flex items-end gap-2 rounded-xl border border-separator bg-window p-1.5 focus-within:ring-2 focus-within:ring-focus">
              <textarea
                bind:value={composeText}
                onkeydown={handleKeydown}
                placeholder="SMS Message…"
                rows={1}
                class="max-h-24 min-h-[28px] flex-1 resize-none bg-transparent px-2 py-1 text-[13px] text-label placeholder:text-tertiary focus:outline-none"
              ></textarea>
              <button
                type="button"
                onclick={handleSend}
                disabled={!composeText.trim() || sending || (isComposingNew && !newRecipient.trim())}
                aria-label="Send message"
                title="Send SMS"
                class="flex h-7 w-7 flex-none items-center justify-center rounded-full bg-accent text-white transition-opacity hover:opacity-90 active:opacity-100 disabled:opacity-40"
              >
                <Send size={13} class={sending ? 'animate-pulse' : ''} aria-hidden="true" />
              </button>
            </div>
            <div class="mt-1 flex justify-between px-1 text-[10px] text-tertiary">
              <span>Press Return to send</span>
              <span>{composeText.length} chars</span>
            </div>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>
