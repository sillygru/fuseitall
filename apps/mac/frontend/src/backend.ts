/*
 * Copyright (C) 2026 FuseItAll contributors.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published
 * by the Free Software Foundation, version 3 of the License. See LICENSE
 * for details.
 *
 * Thin Wails binding shim: re-exports the generated Service and adds
 * forward-compatible access to the remembered-device methods
 * (GetLastDevice, ReconnectToLastDevice). The generated bindings refresh on
 * the next `wails3 build`; until then these wrappers degrade to null so
 * `pnpm run check` passes and the UI simply hides the offline card.
 */

import { Service } from '../bindings/fuseitall/mac/backend';

export interface LastDeviceNotice {
  HasDevice: boolean;
  Host: string;
  Port: number;
  Addr: string;
  LastSeenUnix: number;
}

type LooseService = Record<string, ((...args: never[]) => Promise<unknown>) | undefined>;

const loose = Service as unknown as LooseService;

export async function getLastDevice(): Promise<LastDeviceNotice | null> {
  try {
    const fn = loose['GetLastDevice'];
    if (typeof fn !== 'function') return null;
    const res = (await fn()) as LastDeviceNotice | null;
    if (!res || !res.HasDevice) return null;
    return res;
  } catch {
    return null;
  }
}

export async function reconnectToLastDevice(): Promise<string> {
  const fn = loose['ReconnectToLastDevice'];
  if (typeof fn !== 'function') {
    throw new Error('Reconnect is available after the next app build.');
  }
  return (await fn()) as string;
}

export async function forgetLastDevice(): Promise<string> {
  const fn = loose['ForgetLastDevice'];
  if (typeof fn !== 'function') {
    throw new Error('Forget is available after the next app build.');
  }
  return (await fn()) as string;
}

export { Service };
