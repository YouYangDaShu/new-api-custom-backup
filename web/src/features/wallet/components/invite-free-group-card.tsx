import { useCallback, useEffect, useState } from 'react'
import { CheckCircle2, KeyRound, Loader2 } from 'lucide-react'
import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { IconBadge } from '@/components/ui/icon-badge'
import { Input } from '@/components/ui/input'
import { getInviteFreeGroupCodes, redeemInviteFreeGroupCode } from '../api'
import type { InviteFreeGroupCode } from '../types'

export function InviteFreeGroupCard() {
  const [codes, setCodes] = useState<InviteFreeGroupCode[]>([])
  const [unlocked, setUnlocked] = useState(false)
  const [code, setCode] = useState('')
  const [loading, setLoading] = useState(true)
  const [redeeming, setRedeeming] = useState(false)
  const [message, setMessage] = useState('')

  const load = useCallback(async () => {
    try {
      const response = await getInviteFreeGroupCodes()
      if (response.success && response.data) {
        setUnlocked(response.data.unlocked)
        setCodes(response.data.codes || [])
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

  const redeem = async () => {
    const value = code.trim()
    if (!value) return
    setRedeeming(true)
    setMessage('')
    try {
      const response = await redeemInviteFreeGroupCode({ key: value })
      if (response.success) {
        setCode('')
        setMessage('分组已解锁')
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

  return (
    <Card data-card-hover='false' className='bg-muted/20 py-0'>
      <CardContent className='grid gap-3 p-4 sm:p-5'>
        <div className='flex items-center gap-2.5'>
          <IconBadge tone='chart-2'>
            <KeyRound />
          </IconBadge>
          <div>
            <h3 className='text-sm font-semibold'>福利免费分组解锁</h3>
            <p className='text-muted-foreground text-xs'>
              邀请新用户注册后，双方各获得一枚解锁码。
            </p>
          </div>
          {unlocked && <CheckCircle2 className='ml-auto size-5 text-emerald-500' />}
        </div>
        {unlocked ? (
          <p className='text-sm text-emerald-600'>
            当前账号已解锁，可使用「token福利」分组。
          </p>
        ) : (
          <>
            {pendingCodes.length > 0 && (
              <div className='grid gap-2'>
                <p className='text-muted-foreground text-xs'>你的解锁码：</p>
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
                  </div>
                ))}
              </div>
            )}
            <div className='flex items-center gap-2'>
              <Input
                value={code}
                onChange={(event) => setCode(event.target.value)}
                placeholder='输入解锁码'
                className='bg-background font-mono text-xs'
              />
              <Button
                onClick={redeem}
                disabled={redeeming || !code.trim()}
                className='shrink-0'
              >
                {redeeming && <Loader2 className='mr-2 size-4 animate-spin' />}
                兑换解锁
              </Button>
            </div>
          </>
        )}
        {message && <p className='text-xs text-amber-600'>{message}</p>}
      </CardContent>
    </Card>
  )
}
