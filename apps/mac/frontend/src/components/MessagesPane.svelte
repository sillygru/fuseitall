<!--
  SPDX-License-Identifier: AGPL-3.0-only

  Messages pane: macOS Messages-style 2-pane split view.
  Left: conversation threads with unread indicators and search.
  Right: message transcript with speech bubbles, infinite upward pagination,
  contact search suggestions, and compose bar to send SMS through phone.
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
  import { Events } from '@wailsio/runtime';
  import ContentHeader from './ContentHeader.svelte';
  import {
    listAllSMSThreads,
    listSMSMessages,
    sendSMS,
    markThreadRead,
    searchContacts,
    getContactAvatar,
    findThreadForAddress,
    type SMSThread,
    type SMSMessage,
    type ContactEntry,
  } from '../contacts_messages_api';

  interface Props {
    paired: boolean;
    deviceLabel: string;
    initialRecipient?: string;
    initialDisplayName?: string;
    onClearRecipient?: () => void;
    onUnreadCountChange?: (count: number) => void;
  }

  let {
    paired,
    deviceLabel,
    initialRecipient,
    initialDisplayName,
    onClearRecipient,
    onUnreadCountChange,
  }: Props = $props();

  let query = $state('');
  let loadingThreads = $state(false);
  let loadingMessages = $state(false);
  let loadingOlder = $state(false);
  let hasMoreOlder = $state(true);
  let nextCursor = $state<string>('');
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
  let recipientDisplayName = $state('');
  let messagesContainer = $state<HTMLDivElement | null>(null);
  let composeInputEl = $state<HTMLTextAreaElement | null>(null);
  let toInputEl = $state<HTMLInputElement | null>(null);
  // Previous paired value for offline→paired edge detection. Starts null so
  // the first effect run just syncs without a spurious reload.
  let prevPaired = $state<boolean | null>(null);
  let messageLoadToken = 0;

  // Search suggestions in "To:"
  interface Suggestion {
    name: string;
    number: string;
    type?: string;
    avatar_b64?: string;
    contact_id?: string;
  }
  let suggestions = $state<Suggestion[]>([]);
  let showSuggestions = $state(false);
  let selectedSuggestionIndex = $state(0);
  let searchingContacts = false;

  let filteredThreads = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return threads;
    const qDigits = q.replace(/\D/g, '');
    const isPhoneQuery = qDigits.length >= 3;
    return threads.filter((t) => {
      const name = (t.contact_name ?? '').toLowerCase();
      const addr = (t.address ?? '').toLowerCase();
      const snip = (t.snippet ?? '').toLowerCase();
      if (name.includes(q)) return true;
      if (addr.includes(q)) return true;
      if (snip.includes(q)) return true;
      if (isPhoneQuery) {
        const addrDigits = addr.replace(/\D/g, '');
        if (addrDigits && (addrDigits.includes(qDigits) || qDigits.includes(addrDigits.slice(-7)))) return true;
      }
      return false;
    });
  });

  let activeThread = $derived.by(() => {
    if (selectedThreadId === null) return null;
    return threads.find((t) => t.thread_id === selectedThreadId) ?? null;
  });

  function updateUnreadBadge() {
    const total = threads.reduce((sum, t) => sum + (t.unread_count || 0), 0);
    onUnreadCountChange?.(total);
  }

  async function loadThreads(force = false) {
    if (!paired) return;
    loadingThreads = true;
    error = '';
    permissionError = false;
    try {
      const res = await listAllSMSThreads(50, undefined, force);
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
        updateUnreadBadge();
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

  function dedupeThreads(rows: SMSThread[]): SMSThread[] {
    const seen = new Set<number>();
    return rows.filter((t) => {
      if (seen.has(t.thread_id)) return false;
      seen.add(t.thread_id);
      return true;
    });
  }

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
          // Keep generic icon
        } finally {
          threadAvatarPending.delete(key);
        }
      }),
    );
  }

  function buildCursorFromOldestMessage(): string {
    if (currentMessages.length === 0) return '';
    const oldest = currentMessages[0];
    const sortKey = String(oldest.date);
    const enc = btoa(sortKey).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    const rowId = Math.max(0, oldest.id);
    return `v2.${enc}.${rowId}`;
  }

  function sortMessagesChronologically(messages: SMSMessage[]): SMSMessage[] {
    return messages.slice().sort((a, b) => {
      const dateOrder = (a.date ?? 0) - (b.date ?? 0);
      return dateOrder !== 0 ? dateOrder : (a.id ?? 0) - (b.id ?? 0);
    });
  }

  async function selectThread(threadId: number, force = false) {
    const loadToken = ++messageLoadToken;
    selectedThreadId = threadId;
    isComposingNew = false;
    newRecipient = '';
    recipientDisplayName = '';
    showSuggestions = false;
    loadingMessages = true;
    currentMessages = [];
    nextCursor = '';
    hasMoreOlder = true;

    // Mark as read on PC immediately
    const thread = threads.find((t) => t.thread_id === threadId);
    if (thread && ((thread.unread_count ?? 0) > 0 || !thread.read)) {
      thread.unread_count = 0;
      thread.read = true;
      void markThreadRead(threadId);
      updateUnreadBadge();
    }

    try {
      const res = await listSMSMessages(threadId, '', 50, force);
      if (loadToken !== messageLoadToken || selectedThreadId !== threadId) return;
      currentMessages = sortMessagesChronologically(res?.messages ?? []);
      for (const m of currentMessages) m.read = true;
      nextCursor = res?.next_cursor ?? '';
      if (currentMessages.length < 50 && !nextCursor) {
        hasMoreOlder = false;
      }
      await tick();
      await scrollToBottom();
      composeInputEl?.focus();
    } catch (e: unknown) {
      if (loadToken === messageLoadToken) error = e instanceof Error ? e.message : String(e);
    } finally {
      if (loadToken === messageLoadToken) loadingMessages = false;
    }
  }

  async function refreshActiveThreadMessages() {
    if (selectedThreadId === null) return;
    try {
      const res = await listSMSMessages(selectedThreadId, '', 50, false);
      if (res?.messages) {
        const fresh = sortMessagesChronologically(res.messages);
        const wasNearBottom = messagesContainer
          ? messagesContainer.scrollHeight - messagesContainer.scrollTop - messagesContainer.clientHeight < 100
          : true;
        const existingIds = new Set(currentMessages.map((m) => m.id));
        const toAppend = fresh.filter((m) => !existingIds.has(m.id));
        if (toAppend.length > 0) {
          currentMessages = [...currentMessages, ...toAppend].sort((a, b) => (a.date ?? 0) - (b.date ?? 0));
          if (wasNearBottom) {
            await tick();
            scrollToBottom();
          }
        }
      }
    } catch {}
  }

  async function handleMessagesScroll() {
    if (!messagesContainer || loadingOlder || !hasMoreOlder || selectedThreadId === null) return;
    if (messagesContainer.scrollTop <= 80) {
      await loadOlderMessages();
    }
  }

  async function loadOlderMessages() {
    if (loadingOlder || !hasMoreOlder || selectedThreadId === null) return;
    const cursor = nextCursor || buildCursorFromOldestMessage();
    if (!cursor) {
      hasMoreOlder = false;
      return;
    }

    loadingOlder = true;
    const container = messagesContainer;
    const prevScrollHeight = container ? container.scrollHeight : 0;
    const prevScrollTop = container ? container.scrollTop : 0;
    try {
      const res = await listSMSMessages(selectedThreadId, cursor, 50, false);
      if (res?.messages && res.messages.length > 0) {
        const older = res.messages.slice().sort((a, b) => (a.date ?? 0) - (b.date ?? 0));
        const existingIds = new Set(currentMessages.map((m) => m.id));
        const toPrepend = older.filter((m) => !existingIds.has(m.id));
        if (toPrepend.length > 0) {
          currentMessages = [...toPrepend, ...currentMessages];
          await tick();
          if (container) {
            const heightDiff = container.scrollHeight - prevScrollHeight;
            container.scrollTop = prevScrollTop + heightDiff;
          }
        } else {
          hasMoreOlder = false;
        }
        if (res.messages.length < 50 && !res.next_cursor) {
          hasMoreOlder = false;
        }
      } else {
        hasMoreOlder = false;
      }
      nextCursor = res?.next_cursor ?? '';
    } catch (e: unknown) {
      console.warn('Failed to load older messages', e);
    } finally {
      loadingOlder = false;
    }
  }

  async function scrollToBottom(): Promise<void> {
    await tick();
    if (!messagesContainer) return;
    messagesContainer.scrollTop = messagesContainer.scrollHeight;
    // The transcript can gain its final height one frame after Svelte's DOM
    // flush (fonts and the compose bar affect the flex layout). Repeat once
    // on the next frame so opening a thread always lands on the newest SMS.
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    messagesContainer.scrollTop = messagesContainer.scrollHeight;
  }

  async function handleRecipientInput() {
    const q = newRecipient.trim();
    if (!q) {
      suggestions = [];
      showSuggestions = false;
      return;
    }
    if (searchingContacts) return;
    searchingContacts = true;
    try {
      const results: Suggestion[] = [];
      // 1. Search cached contacts
      const contacts = await searchContacts(q);
      for (const c of contacts.slice(0, 8)) {
        if (c.phones && c.phones.length > 0) {
          for (const p of c.phones) {
            results.push({
              name: c.display_name,
              number: p.number,
              type: p.type || 'phone',
              avatar_b64: c.avatar_b64,
              contact_id: c.contact_id,
            });
          }
        } else {
          results.push({
            name: c.display_name,
            number: '',
            avatar_b64: c.avatar_b64,
            contact_id: c.contact_id,
          });
        }
      }
      // 2. Also match existing threads
      const lowerQ = q.toLowerCase();
      for (const t of threads) {
        if (
          (t.contact_name?.toLowerCase().includes(lowerQ) || t.address.toLowerCase().includes(lowerQ)) &&
          !results.some((r) => r.number === t.address)
        ) {
          results.push({
            name: t.contact_name || t.address,
            number: t.address,
            type: 'thread',
            contact_id: t.contact_id,
          });
        }
      }
      suggestions = results.slice(0, 10);
      selectedSuggestionIndex = 0;
      showSuggestions = suggestions.length > 0;
    } finally {
      searchingContacts = false;
    }
  }

  function pickSuggestion(s: Suggestion) {
    if (s.number) {
      newRecipient = s.number;
      recipientDisplayName = s.name;
    } else {
      newRecipient = s.name;
      recipientDisplayName = s.name;
    }
    showSuggestions = false;

    // Check if a thread already exists for this number
    const existing = findThreadForAddress(threads, newRecipient);
    if (existing) {
      void selectThread(existing.thread_id, false);
    } else {
      composeInputEl?.focus();
    }
  }

  function handleRecipientKeydown(e: KeyboardEvent) {
    if (!showSuggestions || suggestions.length === 0) {
      if (e.key === 'Enter') {
        e.preventDefault();
        showSuggestions = false;
        composeInputEl?.focus();
      }
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      selectedSuggestionIndex = (selectedSuggestionIndex + 1) % suggestions.length;
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      selectedSuggestionIndex = (selectedSuggestionIndex - 1 + suggestions.length) % suggestions.length;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      const s = suggestions[selectedSuggestionIndex];
      if (s) pickSuggestion(s);
    } else if (e.key === 'Escape') {
      showSuggestions = false;
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
        if (isComposingNew) {
          isComposingNew = false;
          newRecipient = '';
          recipientDisplayName = '';
          showSuggestions = false;
          onClearRecipient?.();
          await loadThreads(false);
          if (res.thread_id) {
            selectThread(res.thread_id, true);
          } else {
            const match = findThreadForAddress(threads, targetAddress);
            if (match) selectThread(match.thread_id, true);
          }
        } else if (selectedThreadId) {
          const outgoing: SMSMessage = {
            id: res.message_id ?? Date.now(),
            thread_id: selectedThreadId,
            address: targetAddress,
            body: text,
            date: Date.now(),
            type: 2,
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
    const trimmed = (address ?? '').trim();
    if (trimmed) {
      const existing = findThreadForAddress(threads, trimmed);
      if (existing) {
        void selectThread(existing.thread_id, false);
        return;
      }
    }
    isComposingNew = true;
    selectedThreadId = null;
    currentMessages = [];
    nextCursor = '';
    hasMoreOlder = true;
    newRecipient = address;
    recipientDisplayName = name;
    composeText = '';
    showSuggestions = false;
    tick().then(() => {
      if (address) composeInputEl?.focus();
      else toInputEl?.focus();
    });
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

    let offChanged: (() => void) | null = null;
    try {
      offChanged = Events.On('messages:changed', () => {
        void loadThreads(false);
        if (selectedThreadId !== null) {
          void refreshActiveThreadMessages();
        }
      });
    } catch {}

    const handleRequestMarkRead = (e: Event) => {
      const custom = e as CustomEvent<{ threadId: number }>;
      if (!custom.detail?.threadId) return;
      const tid = custom.detail.threadId;
      const thread = threads.find((t) => t.thread_id === tid);
      if (thread && ((thread.unread_count ?? 0) > 0 || !thread.read)) {
        thread.unread_count = 0;
        thread.read = true;
        updateUnreadBadge();
      }
    };
    window.addEventListener('request-mark-thread-read', handleRequestMarkRead);

    return () => {
      window.removeEventListener('request-mark-thread-read', handleRequestMarkRead);
      try { offChanged?.(); } catch {}
    };
  });

  $effect(() => {
    const isPaired = paired;
    if (prevPaired === null) {
      prevPaired = isPaired;
      return;
    }
    if (isPaired && !prevPaired) {
      untrack(() => void loadThreads(false));
    }
    prevPaired = isPaired;
  });

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

<div class="native-pane flex min-h-0 flex-1 flex-col">
  <div class="native-toolbar flex min-h-[52px] flex-none items-center justify-between px-5 py-2">
    <ContentHeader
      title="Messages"
      subtitle={deviceLabel ? `SMS conversations · ${deviceLabel}` : 'SMS conversations'}
      icon={MessageSquare}
    />
    {#if paired}
      <div class="flex items-center gap-1">
        <button
          onclick={() => startNewConversation()}
          title="New Message"
          aria-label="New Message"
          class="inline-flex h-7 items-center gap-1.5 rounded-md bg-altrow px-2.5 text-[12px] font-medium text-label transition hover:bg-hover active:bg-active"
        >
          <SquarePen size={13} aria-hidden="true" />
          <span>New</span>
        </button>
        <button
          onclick={() => loadThreads(true)}
          disabled={loadingThreads}
          title="Refresh messages"
          aria-label="Refresh messages"
          class="inline-flex h-7 items-center gap-1.5 rounded-md px-2.5 text-[12px] font-medium text-secondary transition hover:bg-altrow hover:text-label active:bg-active disabled:opacity-50"
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
        class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md bg-altrow px-3 text-[12px] font-medium text-label transition hover:bg-hover active:bg-active"
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
        class="mt-4 inline-flex h-7 items-center gap-1.5 rounded-md bg-altrow px-3 text-[12px] font-medium text-label transition hover:bg-hover active:bg-active"
      >
        <RefreshCw size={12} aria-hidden="true" />
        <span>Try Again</span>
      </button>
    </div>
  {:else}
    <div class="native-split flex min-h-0 flex-1 overflow-hidden bg-control">
      <!-- Left: Thread List, same canvas as the detail, no lifted band -->
      <div class="flex w-72 flex-none flex-col bg-window px-2 pb-2">
        <!-- Search bar: one tonal field -->
        <div class="px-1 pb-2 pt-2">
          <div class="relative">
            <Search size={13} class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-tertiary" aria-hidden="true" />
            <input
              bind:value={query}
              type="text"
              placeholder="Search conversations…"
              aria-label="Search conversations"
              class="h-7 w-full rounded-md bg-altrow pl-8 pr-2 text-[12px] text-label placeholder:text-tertiary transition focus:bg-hover"
            />
          </div>
        </div>

        <!-- Threads list -->
        <div class="scroll-overlay flex-1 overflow-y-auto py-1">
          {#if filteredThreads.length === 0}
            <div class="px-4 py-8 text-center text-[12px] text-tertiary">
              {query ? 'No matching conversations' : 'No messages on phone'}
            </div>
          {:else}
            {#each filteredThreads as thread (thread.thread_id)}
              {@const isSelected = selectedThreadId === thread.thread_id && !isComposingNew}
              <button
                type="button"
                data-menu="message"
                data-thread-id={thread.thread_id}
                data-msg-sender={thread.contact_name || thread.address}
                data-msg-text={thread.snippet || ''}
                onclick={() => selectThread(thread.thread_id)}
                class="flex w-full items-start gap-2.5 rounded-lg px-2.5 py-2 text-left transition-colors {isSelected
                  ? 'bg-accent text-accent-text font-medium'
                  : 'text-label hover:bg-hover'}"
              >
                {#if threadAvatars[threadAvatarKey(thread)]}
                  <img
                    src="data:image/jpeg;base64,{threadAvatars[threadAvatarKey(thread)]}"
                    alt={thread.contact_name || thread.address}
                    class="h-8 w-8 flex-none rounded-full object-cover"
                    loading="lazy"
                  />
                {:else}
                  <div
                    class="flex h-8 w-8 flex-none items-center justify-center rounded-full text-[12px] font-semibold {isSelected
                      ? 'bg-accent-text/15 text-accent-text'
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
                    <span class="flex-none text-[10px] {isSelected ? 'text-accent-text/75' : 'text-tertiary'}">
                      {formatTime(thread.date)}
                    </span>
                  </div>

                  <div class="mt-0.5 flex items-center justify-between gap-2">
                    <p class="truncate text-[11px] {isSelected ? 'text-accent-text/90' : 'text-secondary'}">
                      {thread.snippet || '(Empty message)'}
                    </p>
                    {#if thread.unread_count && thread.unread_count > 0 && !isSelected}
                      <span class="flex h-4 min-w-[16px] flex-none items-center justify-center rounded-full bg-accent px-1 text-[9px] font-bold text-accent-text">
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
      <div class="flex flex-1 flex-col bg-window">
        {#if isComposingNew}
          <!-- New Message Header with Autocomplete -->
          <div class="relative flex min-h-[42px] items-center gap-2 px-5 py-2">
            <span class="text-[12px] font-medium text-secondary">To:</span>
            <input
              bind:this={toInputEl}
              bind:value={newRecipient}
              oninput={handleRecipientInput}
              onkeydown={handleRecipientKeydown}
              onfocus={() => { if (suggestions.length > 0) showSuggestions = true; }}
              type="text"
              placeholder="Enter phone number or contact name…"
              class="h-7 flex-1 bg-transparent text-[13px] text-label placeholder:text-tertiary"
            />
            {#if showSuggestions && suggestions.length > 0}
              <div class="absolute left-4 right-4 top-full z-30 mt-1 max-h-60 overflow-y-auto rounded-lg bg-control p-1 shadow-xl">
                {#each suggestions as s, idx (s.number + '-' + idx)}
                  <button
                    type="button"
                    onmousedown={(e) => { e.preventDefault(); pickSuggestion(s); }}
                    class="flex w-full items-center gap-2.5 rounded-md px-2.5 py-1.5 text-left transition {idx === selectedSuggestionIndex ? 'bg-accent text-accent-text' : 'text-label hover:bg-hover'}"
                  >
                    <div class="flex h-7 w-7 flex-none items-center justify-center rounded-full bg-separator text-[11px] font-medium">
                      {#if s.avatar_b64}
                        <img src="data:image/jpeg;base64,{s.avatar_b64}" alt="" class="h-full w-full rounded-full object-cover" />
                      {:else}
                        <User size={13} />
                      {/if}
                    </div>
                    <div class="min-w-0 flex-1">
                      <div class="flex items-baseline justify-between gap-1">
                        <span class="truncate text-[12px] font-semibold">{s.name}</span>
                        {#if s.type}
                          <span class="text-[10px] opacity-75">{s.type}</span>
                        {/if}
                      </div>
                      <div class="truncate text-[11px] opacity-80">{s.number}</div>
                    </div>
                  </button>
                {/each}
              </div>
            {/if}
          </div>

          <div class="flex flex-1 flex-col items-center justify-center p-6 text-center">
            <div class="flex h-12 w-12 items-center justify-center rounded-full bg-accent/15 text-accent">
              <SquarePen size={22} aria-hidden="true" />
            </div>
            <p class="mt-3 text-[14px] font-semibold text-label">
              {recipientDisplayName ? `New conversation with ${recipientDisplayName}` : 'New Conversation'}
            </p>
            <p class="mt-1 max-w-[36ch] text-[12px] text-secondary">
              {newRecipient ? `Send a message below to ${recipientDisplayName || newRecipient}.` : 'Search for a contact above or type a phone number to start.'}
            </p>
          </div>
        {:else if activeThread}
          <!-- Conversation Header -->
          <div class="flex min-h-[42px] items-center gap-2.5 px-5 py-2">
            {#if threadAvatars[threadAvatarKey(activeThread)]}
              <img
                src="data:image/jpeg;base64,{threadAvatars[threadAvatarKey(activeThread)]}"
                alt=""
                class="h-8 w-8 flex-none rounded-full object-cover"
                loading="lazy"
              />
            {:else}
              <div
                class="flex h-8 w-8 flex-none items-center justify-center rounded-full bg-separator text-secondary text-[12px] font-semibold"
              >
                <User size={15} aria-hidden="true" />
              </div>
            {/if}
            <div class="min-w-0 flex-1">
              <h3 class="truncate text-[13px] font-semibold text-label">
                {activeThread.contact_name || activeThread.address}
              </h3>
              {#if activeThread.contact_name}
                <p class="truncate text-[11px] text-secondary">{activeThread.address}</p>
              {/if}
            </div>
          </div>

          <!-- Message Bubbles Transcript with upward pagination -->
          <div
            bind:this={messagesContainer}
            onscroll={handleMessagesScroll}
            class="scroll-overlay flex-1 overflow-y-auto p-4 space-y-3"
          >
            {#if loadingOlder}
              <div class="flex justify-center py-2">
                <RefreshCw size={14} class="animate-spin text-tertiary" />
              </div>
            {/if}

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
                <div
                  data-menu="message"
                  data-msg-text={msg.body}
                  data-msg-sender={activeThread?.contact_name || activeThread?.address || ''}
                  class="flex flex-col {isMe ? 'items-end' : 'items-start'}"
                >
                  <div
                    class="max-w-[70%] rounded-2xl px-3.5 py-2 text-[13px] leading-relaxed {isMe
                      ? 'bg-accent text-accent-text rounded-br-sm'
                      : 'bg-altrow text-label rounded-bl-sm'}"
                  >
                    {msg.body}
                  </div>
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

        <!-- Compose Bar: input and send sit directly on the pane, no bubble -->
        {#if activeThread || isComposingNew}
          <div class="px-5 py-3">
            <div class="mx-auto flex max-w-[720px] items-end gap-2">
              <textarea
                bind:this={composeInputEl}
                bind:value={composeText}
                onkeydown={handleKeydown}
                placeholder="SMS Message…"
                rows={1}
                class="max-h-24 min-h-[28px] flex-1 resize-none bg-transparent px-2 py-1 text-[13px] text-label placeholder:text-tertiary"
              ></textarea>
              <button
                type="button"
                onclick={handleSend}
                disabled={!composeText.trim() || sending || (isComposingNew && !newRecipient.trim())}
                aria-label="Send message"
                title="Send SMS"
                class="flex h-7 w-7 flex-none items-center justify-center rounded-full bg-accent text-accent-text transition-opacity hover:opacity-90 active:opacity-100 disabled:opacity-40"
              >
                <Send size={13} class={sending ? 'animate-pulse' : ''} aria-hidden="true" />
              </button>
            </div>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>
