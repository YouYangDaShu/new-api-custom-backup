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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { getAppBasePath, getAppUrl } from './app-base-path.ts'

describe('app base path detection', () => {
  test('supports root and known public mount paths', () => {
    assert.equal(getAppBasePath('/'), '')
    assert.equal(getAppBasePath('/login'), '')
    assert.equal(getAppBasePath('/newpreview'), '/newpreview')
    assert.equal(getAppBasePath('/newpreview/dashboard'), '/newpreview')
    assert.equal(getAppBasePath('/newapi-dev'), '/newapi-dev')
    assert.equal(getAppBasePath('/newapi-dev/dashboard'), '/newapi-dev')
  })

  test('does not treat similarly named paths as a mount', () => {
    assert.equal(getAppBasePath('/newpreview-old/dashboard'), '')
    assert.equal(getAppBasePath('/newapi-devtools'), '')
  })

  test('prefixes internal URLs only for mounted deployments', () => {
    assert.equal(getAppUrl('/oauth/github'), '/oauth/github')
    assert.equal(getAppUrl('/oauth/github', '/newpreview/profile'), '/newpreview/oauth/github')
    assert.equal(getAppUrl('/api/status', '/newapi-dev/dashboard'), '/newapi-dev/api/status')
    assert.equal(getAppUrl('https://example.com/callback'), 'https://example.com/callback')
  })
})
