import { useCallback, useEffect, useState } from 'react'
import { CheckCircle2, Clock, KeyRound, Loader2 } from 'lucide-react'
import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { getInviteFreeGroupCodes, redeemInviteFreeGroupCode } from '../api'
import type { InviteFreeGroupCode } from '../types'

function formatExpiry(timestamp?: number) {
  if (!timestamp || timestamp <= 0) return ''
  const date = new Date(timestamp * 1000)
  const pad = (n: number) => (n < 10 ? `0${n}` : `${n}`)
  const y = date.getFullYear()
  const m = pad(date.getMonth() + 1)
  const d = pad(date.getDate())
  const h = pad(date.getHours())
  const min = pad(date.getMinutes())
  const s = pad(date.getSeconds())
  return `${y}-${m}-${d} ${h}:${min}:${s}`
}

export function InviteFreeGroupCard() {
  const [codes, setCodes] = useState<InviteFreeGroupCode[]>([])
  const [unlocked, setUnlocked] = useState(false)
  const [expiresAt, setExpiresAt] = useState<number | undefined>()
  const [groupName, setGroupName] = useState('token福利')
  const [code, setCode] = useState('')
  const [loading, setLoading] = useState(true)
  const [redeeming, setRedeeming] = useState(false)
  const [message, setMessage] = useState('')

  const load = useCallback(async () => {
    try {
      const response = await getInviteFreeGroupCodes()
      if (response.success && response.data) {
        setUnlocked(response.data.unlocked)
        setExpiresAt(response.data.expires_at)
        setCodes(response.data.codes || [])
        if (response.data.group_name) {
          setGroupName(response.data.group_name)
        }
      }
    } catch {
      /* supplementary card */
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const redeemWithKey = async (keyToRedeem: string) => {
    const value = keyToRedeem.trim()
    if (!value) return
    setRedeeming(true)
    setMessage('')
    try {
      const response = await redeemInviteFreeGroupCode({ key: value })
      if (response.success) {
        setCode('')
        setMessage(response.message || '兑换成功，有效时长已更新！')
        await load()
      } else {
        setMessage(response.message || '解锁失败，请检查兑换码')
      }
    } catch {
      setMessage('解锁失败，请检查兑换码')
    } finally {
      setRedeeming(false)
    }
  }

  if (loading) return null

  const pendingCodes = codes.filter((item) => item.status === 1)
  const isExpired = !unlocked && Boolean(expiresAt && expiresAt > 0)

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='grid gap-3 p-4 sm:p-5'>
        <div className='flex items-center gap-2.5'>
          <IconBadge tone='chart-2'>
            <KeyRound />
          </IconBadge>
          <div>
            <h3 className='text-sm font-semibold'>福利免费分组解锁（1天/次）</h3>
            <p className='text-muted-foreground text-xs'>
              邀请新用户注册后双方各获 1 枚解锁码，每码激活 24 小时权限，支持多码叠加续期。
            </p>
          </div>
          {unlocked && <CheckCircle2 className='ml-auto size-5 text-emerald-500' />}
        </div>

        {unlocked ? (
          <div className='rounded-md border border-emerald-500/20 bg-emerald-500/10 p-3 text-xs'>
            <div className='flex items-center gap-1.5 font-medium text-emerald-600 dark:text-emerald-400'>
              <Clock className='size-3.5' />
              <span>当前已解锁「{groupName}」分组</span>
            </div>
            {expiresAt && expiresAt > 0 && (
              <p className='mt-1 text-emerald-700/80 dark:text-emerald-300/80'>
                有效截止时间：<span className='font-mono font-semibold'>{formatExpiry(expiresAt)}</span>
              </p>
            )}
          </div>
        ) : isExpired ? (
          <div className='rounded-md border border-amber-500/20 bg-amber-500/10 p-3 text-xs text-amber-700 dark:text-amber-400'>
            <span>上次福利时效已到期（截止于 {formatExpiry(expiresAt)}）。兑换新邀请码可立即恢复 24 小时使用权限。</span>
          </div>
        ) : null}

        {pendingCodes.length > 0 && (
          <div className='grid gap-2'>
            <p className='text-muted-foreground text-xs font-medium'>
              待使用的专属解锁码（共 {pendingCodes.length} 枚）：
            </p>
            {pendingCodes.map((item) => (
              <div className='flex items-center gap-2' key={item.id}>
                <Input
                  value={item.key}
                  readOnly
                  className='bg-background font-mono text-xs'
                />
                <CopyButton
                  value={item.key}
                  variant='outline'
                  className='size-9 shrink-0'
                  aria-label='复制解锁码'
                />
                <Button
                  size='sm'
                  variant='secondary'
                  disabled={redeeming}
                  onClick={() => redeemWithKey(item.key)}
                  className='shrink-0 text-xs'
                >
                  {unlocked ? '续期 +24h' : '立即解锁'}
                </Button>
              </div>
            ))}
          </div>
        )}

        <div className='flex items-center gap-2'>
          <Input
            value={code}
            onChange={(event) => setCode(event.target.value)}
            placeholder={unlocked ? '输入其他解锁码叠加续期 (+24h)' : '输入专属解锁码'}
            className='bg-background font-mono text-xs'
          />
          <Button
            onClick={() => redeemWithKey(code)}
            disabled={redeeming || !code.trim()}
            className='shrink-0'
          >
            {redeeming && <Loader2 className='mr-2 size-4 animate-spin' />}
            {unlocked ? '叠加续期' : '兑换解锁 (24h)'}
          </Button>
        </div>

        {message && <p className='text-xs text-amber-600 dark:text-amber-400'>{message}</p>}
      </CardContent>
    </Card>
  )
}
