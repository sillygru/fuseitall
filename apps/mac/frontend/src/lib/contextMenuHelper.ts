/*
 * SPDX-License-Identifier: AGPL-3.0-only
 *
 * Unified context menu resolver (HIG Context Menus): inspects DOM target
 * hierarchies to build small, strictly relevant context menus for any
 * clicked surface, with fallback for blank window space.
 */

import type { MenuItem } from '../components/ContextMenu.svelte';

export interface ContextActionHandler {
  selectPane: (id: string) => void;
  copyText: (text: string) => Promise<void>;
  reconnect: () => Promise<void>;
  startRename: () => void;
  disconnect: () => void;
  forget: () => Promise<void>;
  refresh: () => Promise<void>;
  clearNotifs?: () => void;
  markNotifsSeen?: () => Promise<void>;
  dismissNotif?: (id: string) => void;
  mutePackage?: (pkg: string) => void;
  sendPlaybackCmd?: (cmd: string) => void;
  openAbout?: () => void;
  deleteContact?: (contactId: string, contactName: string, lookupKey?: string) => void;
  markThreadRead?: (threadId: number) => void;
}

export interface ContextResolution {
  items: MenuItem[];
  onPick: (id: string) => void;
}

export interface ContextResolverOptions {
  paired: boolean;
  hasLastDevice: boolean;
  deviceName: string;
  selectedId: string;
  pairCode?: string;
  pairHostPort?: string;
  fingerprint?: string;
  handlers: ContextActionHandler;
}

export function resolveContextMenu(
  target: HTMLElement | null,
  options: ContextResolverOptions,
): ContextResolution | null {
  const { paired, hasLastDevice, deviceName, handlers, pairCode, pairHostPort, fingerprint } = options;

  // 1. Text selection inside inputs or selectable blocks
  const selection = window.getSelection()?.toString().trim();
  const isInsideInput = target?.closest('input, textarea');

  // 2. Specific data-menu triggers
  const pairingEl = target?.closest?.('[data-menu="pairing"]') as HTMLElement | null;
  if (pairingEl) {
    const items: MenuItem[] = [];
    if (pairCode) items.push({ id: 'copy-code', label: 'Copy Pairing Code' });
    if (pairHostPort) items.push({ id: 'copy-host', label: 'Copy Host Address' });
    if (fingerprint) items.push({ id: 'copy-fp', label: 'Copy Fingerprint' });
    items.push({ id: 'refresh', label: 'Refresh Connection Info' });
    return {
      items,
      onPick: (id) => {
        if (id === 'copy-code' && pairCode) void handlers.copyText(pairCode);
        else if (id === 'copy-host' && pairHostPort) void handlers.copyText(pairHostPort);
        else if (id === 'copy-fp' && fingerprint) void handlers.copyText(fingerprint);
        else if (id === 'refresh') void handlers.refresh();
      },
    };
  }

  // 3. Device Hero or phone identification card
  const deviceEl = target?.closest?.('[data-menu="device"], [data-device-hero]') as HTMLElement | null;
  if (deviceEl || target?.closest?.('[aria-label="Phone details"]')) {
    const items: MenuItem[] = [];
    if (deviceName) {
      items.push({ id: 'copy-device-name', label: `Copy "${deviceName}"` });
    }
    if (paired) {
      items.push({ id: 'rename', label: 'Rename Device…' });
      items.push({ id: 'disconnect', label: 'Disconnect…' });
    } else if (hasLastDevice) {
      items.push({ id: 'reconnect', label: 'Reconnect' });
      items.push({ id: 'rename', label: 'Rename Device…' });
      items.push({ id: 'forget', label: 'Forget Device…', destructive: true });
    }
    items.push({ id: 'refresh', label: 'Refresh Status' });
    return {
      items,
      onPick: (id) => {
        if (id === 'copy-device-name') void handlers.copyText(deviceName);
        else if (id === 'rename') handlers.startRename();
        else if (id === 'disconnect') handlers.disconnect();
        else if (id === 'reconnect') void handlers.reconnect();
        else if (id === 'forget') void handlers.forget();
        else if (id === 'refresh') void handlers.refresh();
      },
    };
  }

  // 4. Sidebar navigation item
  const navEl = target?.closest?.('[data-source-id], [data-menu="nav"]') as HTMLElement | null;
  if (navEl) {
    const navId = navEl.getAttribute('data-source-id') || navEl.dataset.sourceId || navEl.dataset.navId || '';
    const navLabel = navEl.querySelector('span')?.textContent?.trim() || navId;
    const items: MenuItem[] = [];
    if (navId) {
      items.push({ id: 'open-pane', label: `Open ${navLabel}` });
    }
    if (navId === 'notifications') {
      if (handlers.markNotifsSeen) items.push({ id: 'mark-read', label: 'Mark All as Read' });
      if (handlers.clearNotifs) items.push({ id: 'clear-notifs', label: 'Clear All Notifications' });
    } else if (navId === 'settings') {
      items.push({ id: 'refresh', label: 'Reload Settings' });
    }
    return {
      items,
      onPick: (id) => {
        if (id === 'open-pane' && navId) handlers.selectPane(navId);
        else if (id === 'mark-read' && handlers.markNotifsSeen) void handlers.markNotifsSeen();
        else if (id === 'clear-notifs' && handlers.clearNotifs) handlers.clearNotifs();
        else if (id === 'refresh') void handlers.refresh();
      },
    };
  }

  // 5. Notifications
  const notifEl = target?.closest?.('[data-menu="notification"]') as HTMLElement | null;
  if (notifEl) {
    const notifId = notifEl.dataset.notifId || '';
    const notifText = notifEl.dataset.notifText || notifEl.textContent?.trim() || '';
    const notifPkg = notifEl.dataset.notifPkg || '';
    const appTitle = notifEl.dataset.notifApp || 'App';
    const items: MenuItem[] = [];
    if (notifId && handlers.dismissNotif) {
      items.push({ id: 'dismiss', label: 'Dismiss Notification' });
    }
    if (notifText) {
      items.push({ id: 'copy-text', label: 'Copy Notification Text' });
    }
    if (notifPkg && handlers.mutePackage) {
      items.push({ id: 'mute-app', label: `Mute Notifications from ${appTitle}` });
    }
    if (handlers.clearNotifs) {
      items.push({ id: 'clear-all', label: 'Clear All Notifications' });
    }
    return {
      items,
      onPick: (id) => {
        if (id === 'dismiss' && handlers.dismissNotif) handlers.dismissNotif(notifId);
        else if (id === 'copy-text') void handlers.copyText(notifText);
        else if (id === 'mute-app' && handlers.mutePackage) handlers.mutePackage(notifPkg);
        else if (id === 'clear-all' && handlers.clearNotifs) handlers.clearNotifs();
      },
    };
  }

  // 6. Messages
  const msgEl = target?.closest?.('[data-menu="message"]') as HTMLElement | null;
  if (msgEl) {
    const msgText = msgEl.dataset.msgText || msgEl.textContent?.trim() || '';
    const msgSender = msgEl.dataset.msgSender || '';
    const threadIdStr = msgEl.dataset.threadId || msgEl.closest('[data-thread-id]')?.getAttribute('data-thread-id');
    const threadId = threadIdStr ? parseInt(threadIdStr, 10) : 0;
    const items: MenuItem[] = [];
    if (threadId > 0 && handlers.markThreadRead) {
      items.push({ id: 'mark-read', label: 'Mark as Read' });
    }
    if (msgText) items.push({ id: 'copy-msg', label: 'Copy Message' });
    if (msgSender) items.push({ id: 'copy-sender', label: `Copy Address (${msgSender})` });
    return {
      items,
      onPick: (id) => {
        if (id === 'mark-read' && threadId > 0 && handlers.markThreadRead) handlers.markThreadRead(threadId);
        else if (id === 'copy-msg') void handlers.copyText(msgText);
        else if (id === 'copy-sender') void handlers.copyText(msgSender);
      },
    };
  }

  // 7. Contacts
  const contactEl = target?.closest?.('[data-menu="contact"]') as HTMLElement | null;
  if (contactEl) {
    const contactId = contactEl.dataset.contactId || '';
    const contactLookupKey = contactEl.dataset.contactLookupKey || '';
    const contactName = contactEl.dataset.contactName || '';
    const contactNumber = contactEl.dataset.contactNumber || '';
    const items: MenuItem[] = [];
    if (contactNumber) {
      items.push({ id: 'msg-contact', label: `Message ${contactName || contactNumber}` });
      items.push({ id: 'copy-number', label: 'Copy Phone Number' });
    }
    if (contactName) {
      items.push({ id: 'copy-name', label: 'Copy Contact Name' });
    }
    if (contactId && handlers.deleteContact) {
      items.push({ id: 'delete-contact', label: 'Delete Contact…', destructive: true });
    }
    return {
      items,
      onPick: (id) => {
        if (id === 'msg-contact' && contactNumber) handlers.selectPane('messages');
        else if (id === 'copy-number') void handlers.copyText(contactNumber);
        else if (id === 'copy-name') void handlers.copyText(contactName);
        else if (id === 'delete-contact' && contactId && handlers.deleteContact) {
          handlers.deleteContact(contactId, contactName, contactLookupKey);
        }
      },
    };
  }

  // 8. Photos
  const photoEl = target?.closest?.('[data-menu="photo"]') as HTMLElement | null;
  if (photoEl) {
    const photoName = photoEl.dataset.photoName || 'Photo';
    const items: MenuItem[] = [
      { id: 'view-photo', label: 'Open Preview' },
      { id: 'download-photo', label: 'Download Photo' },
      { id: 'copy-name', label: `Copy "${photoName}"` },
    ];
    return {
      items,
      onPick: (id) => {
        if (id === 'copy-name') void handlers.copyText(photoName);
        else {
          const btn = photoEl.querySelector<HTMLButtonElement>('button');
          btn?.click();
        }
      },
    };
  }

  // 9. Media player controls
  const mediaEl = target?.closest?.('[data-menu="media"], .media-player') as HTMLElement | null;
  if (mediaEl) {
    const trackTitle = mediaEl.dataset.trackTitle || '';
    const items: MenuItem[] = [
      { id: 'toggle-playback', label: 'Play / Pause' },
      { id: 'next-track', label: 'Next Track' },
      { id: 'prev-track', label: 'Previous Track' },
    ];
    if (trackTitle) items.push({ id: 'copy-track', label: `Copy "${trackTitle}"` });
    return {
      items,
      onPick: (id) => {
        if (id === 'toggle-playback' && handlers.sendPlaybackCmd) handlers.sendPlaybackCmd('toggle');
        else if (id === 'next-track' && handlers.sendPlaybackCmd) handlers.sendPlaybackCmd('next');
        else if (id === 'prev-track' && handlers.sendPlaybackCmd) handlers.sendPlaybackCmd('previous');
        else if (id === 'copy-track' && trackTitle) void handlers.copyText(trackTitle);
      },
    };
  }

  // 10. Generic copyable elements or active text selection
  const copyEl = target?.closest?.('[data-copy]') as HTMLElement | null;
  if (copyEl?.dataset.copy) {
    const copyVal = copyEl.dataset.copy;
    return {
      items: [{ id: 'copy-data', label: 'Copy' }],
      onPick: () => void handlers.copyText(copyVal),
    };
  }

  if (selection && !isInsideInput) {
    return {
      items: [{ id: 'copy-selection', label: `Copy "${selection.length > 24 ? selection.slice(0, 24) + '…' : selection}"` }],
      onPick: () => void handlers.copyText(selection),
    };
  }

  // 11. General window / background fallback
  const fallbackItems: MenuItem[] = [
    { id: 'refresh-app', label: 'Refresh Connection & Status' },
    { id: 'goto-phone', label: paired || hasLastDevice ? 'Device Overview' : 'Pair New Device' },
    { id: 'open-settings', label: 'Settings…' },
  ];

  return {
    items: fallbackItems,
    onPick: (id) => {
      if (id === 'refresh-app') void handlers.refresh();
      else if (id === 'goto-phone') handlers.selectPane(paired || hasLastDevice ? 'phone' : 'pair');
      else if (id === 'open-settings') handlers.selectPane('settings');
    },
  };
}
