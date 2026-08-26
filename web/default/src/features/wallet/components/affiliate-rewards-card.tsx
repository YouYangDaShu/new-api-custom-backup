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
import { Share2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'

import type { UserWalletData } from '../types'

interface AffiliateRewardsCardProps {
  user: UserWalletData | null
  affiliateLink: string
  onTransfer: () => void
  complianceConfirmed?: boolean
  loading?: boolean
  /** Reward rate, e.g. 0.05 => 5% */
  rewardRate?: number
}

export function AffiliateRewardsCard({
  user,
  affiliateLink,
  onTransfer,
  complianceConfirmed = true,
  loading,
  rewardRate = 0.05,
}: AffiliateRewardsCardProps) {
  const { t } = useTranslation()
  if (loading) {
    return (
      <Card data-card-hover='false' className='bg-muted/20 py-0'>
        <CardContent className='space-y-4 p-4 sm:p-5'>
          <Skeleton className='h-6 w-40' />
          <Skeleton className='h-4 w-64' />
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
            <Skeleton className='h-24 rounded-lg' />
            <Skeleton className='h-24 rounded-lg' />
            <Skeleton className='h-24 rounded-lg' />
            <Skeleton className='h-24 rounded-lg' />
          </div>
          <Skeleton className='h-10 rounded-lg' />
          <Skeleton className='h-16 rounded-lg' />
        </CardContent>
      </Card>
    )
  }

  const pending = user?.aff_quota ?? 0
  const totalEarned = user?.aff_history_quota ?? 0
  const invites = user?.aff_count ?? 0
  const hasRewards = pending > 0
  const canTransfer = complianceConfirmed && hasRewards
  const ratePercent = Number.isFinite(rewardRate)
    ? Math.round(rewardRate * 10000) / 100
    : 5

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='space-y-4 p-4 sm:p-5'>
        <div className='flex items-start gap-3'>
          <div className='bg-background flex size-9 shrink-0 items-center justify-center rounded-lg border'>
            <Share2 className='text-muted-foreground size-4' />
          </div>
          <div className='min-w-0'>
            <h3 className='text-base font-semibold'>{t('Invite Rewards')}</h3>
            <p className='text-muted-foreground mt-0.5 text-sm'>
              {t('Invite friends and earn extra rewards')}
            </p>
          </div>
        </div>

        <div>
          <div className='text-muted-foreground mb-2 text-xs font-medium tracking-wider uppercase'>
            {t('Earnings Stats')}
          </div>
          {/* 手机 2x2，宽屏一排 4 个 */}
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-4'>
            <div className='bg-background/70 rounded-lg border p-3'>
              <div className='text-muted-foreground text-[11px] font-medium tracking-wider uppercase'>
                {t('Reward Rate')}
              </div>
              <div className='mt-1 text-lg font-semibold tabular-nums'>
                {ratePercent}%
              </div>
            </div>

            {/* 待使用收益：标题 + 小按钮同一行，金额在下 */}
            <div className='bg-background/70 rounded-lg border p-3'>
              <div className='flex items-center justify-between gap-1.5'>
                <div className='text-muted-foreground min-w-0 truncate text-[11px] font-medium tracking-wider uppercase'>
                  {t('Pending Rewards')}
                </div>
                <Button
                  onClick={onTransfer}
                  disabled={!canTransfer}
                  variant='outline'
                  size='sm'
                  className='h-5 shrink-0 px-1.5 text-[10px] leading-none'
                >
                  {t('Transfer to Balance')}
                </Button>
              </div>
              <div className='mt-1 text-lg font-semibold tabular-nums'>
                {formatQuota(pending)}
              </div>
            </div>

            <div className='bg-background/70 rounded-lg border p-3'>
              <div className='text-muted-foreground text-[11px] font-medium tracking-wider uppercase'>
                {t('Total Earnings')}
              </div>
              <div className='mt-1 text-lg font-semibold tabular-nums'>
                {formatQuota(totalEarned)}
              </div>
            </div>

            <div className='bg-background/70 rounded-lg border p-3'>
              <div className='text-muted-foreground text-[11px] font-medium tracking-wider uppercase'>
                {t('Invite Count')}
              </div>
              <div className='mt-1 text-lg font-semibold tabular-nums'>
                {invites}
              </div>
            </div>
          </div>
        </div>

        <div className='space-y-2'>
          <div className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
            {t('Invite Link')}
          </div>
          <div className='flex items-center gap-2'>
            <Input
              value={affiliateLink}
              readOnly
              className='border-muted bg-background/70 h-9 min-w-0 flex-1 font-mono text-xs'
            />
            <CopyButton
              value={affiliateLink}
              variant='outline'
              className='bg-background size-9 shrink-0'
              iconClassName='size-4'
              tooltip={t('Copy referral link')}
              aria-label={t('Copy referral link')}
            />
          </div>
        </div>

        <div className='bg-background/50 rounded-lg border p-3'>
          <div className='text-muted-foreground mb-2 text-xs font-medium tracking-wider uppercase'>
            {t('Reward Rules')}
          </div>
          <ul className='text-muted-foreground space-y-1.5 text-sm'>
            <li>
              {t(
                'Invite friends to register. When they top up or redeem codes, you earn rewards.'
              )}
            </li>
            <li>
              {t(
                'Use transfer to move pending rewards into your account balance.'
              )}
            </li>
            <li>
              {t('The more friends you invite, the more rewards you can earn.')}
            </li>
          </ul>
        </div>
      </CardContent>
    </Card>
  )
}
