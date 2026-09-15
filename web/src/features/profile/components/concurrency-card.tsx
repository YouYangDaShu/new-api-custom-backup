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
import { Gauge } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'

import { useSelfConcurrency } from '../../concurrency/hooks'
import type { ConcurrencyStatus } from '../../concurrency/types'

function valueText(status: ConcurrencyStatus, t: (key: string) => string) {
  if (status.source === 'unavailable') return t('Unavailable')
  if (status.admin_exempt || status.source === 'unlimited') {
    return t('Unlimited')
  }
  return `${status.current} / ${status.limit}`
}

export function ConcurrencyCard() {
  const { t } = useTranslation()
  const { data, isLoading } = useSelfConcurrency()
  const status = data?.data

  if (isLoading) {
    return (
      <Card data-card-hover='false'>
        <CardContent className='space-y-3 p-4 sm:p-5'>
          <Skeleton className='h-6 w-44' />
          <Skeleton className='h-16 w-full' />
        </CardContent>
      </Card>
    )
  }

  if (!status) return null

  return (
    <TitledCard
      title={t('Request Concurrency')}
      description={t('Read-only request concurrency status')}
      icon={<Gauge className='h-4 w-4' />}
      iconTone='info'
      disableHoverEffect
    >
      <div className='grid grid-cols-2 gap-3 p-3 sm:grid-cols-4 sm:p-5'>
        <div>
          <p className='text-muted-foreground text-xs'>{t('Current / Limit')}</p>
          <p className='mt-1 font-mono text-sm font-semibold tabular-nums'>
            {valueText(status, t)}
          </p>
        </div>
        <div>
          <p className='text-muted-foreground text-xs'>{t('Remaining')}</p>
          <p className='mt-1 font-mono text-sm font-semibold tabular-nums'>
            {status.source === 'unlimited' || status.admin_exempt
              ? '—'
              : status.remaining}
          </p>
        </div>
        <div>
          <p className='text-muted-foreground text-xs'>{t('Queued')}</p>
          <p className='mt-1 font-mono text-sm font-semibold tabular-nums'>
            {status.queued}
          </p>
        </div>
        <div>
          <p className='text-muted-foreground text-xs'>{t('Source')}</p>
          <p className='mt-1 text-sm font-semibold'>
            {status.admin_exempt
              ? t('Administrator exempt')
              : status.source === 'custom'
                ? t('Custom')
                : status.source === 'unlimited'
                  ? t('Unlimited')
                  : status.source === 'unavailable'
                    ? t('Unavailable')
                    : t('System default')}
          </p>
        </div>
      </div>
    </TitledCard>
  )
}