/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { type Table } from '@tanstack/react-table'
import { Gauge } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { batchUpdateUserConcurrency } from '@/features/concurrency/api'

import { type User } from '../types'
import { useUsers } from './users-provider'

interface DataTableBulkActionsProps {
  table: Table<User>
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useUsers()
  const [limitDialogOpen, setLimitDialogOpen] = useState(false)
  const [resetDialogOpen, setResetDialogOpen] = useState(false)
  const [limit, setLimit] = useState('5')
  const [isSaving, setIsSaving] = useState(false)
  const selectedIds = table
    .getFilteredSelectedRowModel()
    .rows.map((row) => row.original.id)

  const refreshSelection = () => {
    table.resetRowSelection()
  }

  const updateSelected = async (source: 'default' | 'custom') => {
    const parsed = Number.parseInt(limit, 10)
    if (source === 'custom' && (!Number.isInteger(parsed) || parsed < 0 || parsed > 10000)) {
      toast.error(t('Concurrency must be an integer between 0 and 10000'))
      return
    }
    setIsSaving(true)
    try {
      const result = await batchUpdateUserConcurrency({
        user_ids: selectedIds,
        source,
        limit: source === 'custom' ? parsed : 0,
      })
      if (!result.success) {
        toast.error(result.message || t('Failed to update concurrency'))
        return
      }
      toast.success(t('Concurrency updated successfully'))
      setLimitDialogOpen(false)
      setResetDialogOpen(false)
      refreshSelection()
      triggerRefresh()
    } catch {
      toast.error(t('Failed to update concurrency'))
    } finally {
      setIsSaving(false)
    }
  }

  return (
    <BulkActionsToolbar table={table} entityName='user'>
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => setLimitDialogOpen(true)}
      >
        <Gauge data-icon='inline-start' />
        {t('Set concurrency')}
      </Button>
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={() => setResetDialogOpen(true)}
      >
        {t('Follow system default')}
      </Button>
      <Dialog
        open={limitDialogOpen}
        onOpenChange={setLimitDialogOpen}
        title={t('Set concurrency for selected users')}
        description={t('Enter 0 to remove the limit.')}
        footer={
          <>
            <Button type='button' variant='outline' onClick={() => setLimitDialogOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button type='button' onClick={() => updateSelected('custom')} disabled={isSaving}>
              {isSaving ? t('Saving...') : t('Save')}
            </Button>
          </>
        }
      >
        <div className='space-y-2'>
          <Label htmlFor='bulk-concurrency-limit'>{t('Custom concurrent requests')}</Label>
          <Input
            id='bulk-concurrency-limit'
            type='number'
            min={0}
            max={10000}
            step={1}
            value={limit}
            onChange={(event) => setLimit(event.target.value)}
          />
        </div>
      </Dialog>
      <ConfirmDialog
        open={resetDialogOpen}
        onOpenChange={setResetDialogOpen}
        title={t('Restore system default concurrency')}
        desc={t('Selected users will follow the current system default. Continue?')}
        confirmText={t('Confirm')}
        handleConfirm={() => updateSelected('default')}
        isLoading={isSaving}
      />
    </BulkActionsToolbar>
  )
}
