/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

const DISMISS_SESSION_KEY_PREFIX = 'announcement-popup-dismissed-session'

export function announcementDismissSessionKey(
  userId: number | undefined,
  sessionSID: string | undefined
) {
  return `${DISMISS_SESSION_KEY_PREFIX}:${userId ?? 'unknown'}:${sessionSID ?? 'unknown'}`
}
