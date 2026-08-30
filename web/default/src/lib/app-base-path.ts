/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

const APP_MOUNT_PATHS = ['/newpreview', '/newapi-dev'] as const

/** Return the configured public mount path for the current browser location. */
export function getAppBasePath(pathname?: string): string {
  const currentPath =
    pathname ?? (typeof window !== 'undefined' ? window.location.pathname : '/')
  return (
    APP_MOUNT_PATHS.find(
      (prefix) => currentPath === prefix || currentPath.startsWith(`${prefix}/`)
    ) ?? ''
  )
}

/** Prefix an internal application URL while leaving absolute URLs untouched. */
export function getAppUrl(path: string, pathname?: string): string {
  if (/^[a-z][a-z\d+.-]*:/i.test(path) || path.startsWith('//')) return path
  const basePath = getAppBasePath(pathname)
  if (!basePath) return path
  return `${basePath}/${path.replace(/^\/+/, '')}`
}
