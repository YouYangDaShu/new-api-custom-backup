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
import { zodResolver } from '@hookform/resolvers/zod'
import { RotateCcw } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import { resetAllUserConcurrency } from '@/features/concurrency/api'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const schema = z.object({
  UserConcurrencyDefault: z.number().int().min(1).max(10000),
})

type Values = z.infer<typeof schema>

export function UserConcurrencySection({
  defaultValues,
}: {
  defaultValues: Values
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [resetOpen, setResetOpen] = useState(false)
  const [resetting, setResetting] = useState(false)
  const form = useForm<Values>({
    resolver: zodResolver(schema),
    mode: 'onChange',
    defaultValues,
  })

  useEffect(() => {
    form.reset(defaultValues)
  }, [defaultValues, form])

  const onSubmit = async (values: Values) => {
    if (values.UserConcurrencyDefault === defaultValues.UserConcurrencyDefault) {
      return
    }
    await updateOption.mutateAsync({
      key: 'UserConcurrencyDefault',
      value: values.UserConcurrencyDefault,
    })
  }

  const handleReset = async () => {
    setResetting(true)
    try {
      const result = await resetAllUserConcurrency()
      if (result.success) {
        toast.success(t('All user concurrency limits reset successfully'))
        setResetOpen(false)
        form.reset(defaultValues)
      } else {
        toast.error(result.message || t('Failed to reset user concurrency'))
      }
    } catch {
      toast.error(t('Failed to reset user concurrency'))
    } finally {
      setResetting(false)
    }
  }

  return (
    <SettingsSection title={t('Request Concurrency')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel={t('Save concurrency default')}
          />
          <FormField
            control={form.control}
            name='UserConcurrencyDefault'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Default concurrent requests per user')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={1}
                    max={10000}
                    step={1}
                    {...field}
                    onChange={(event) =>
                      field.onChange(Number.parseInt(event.target.value) || 1)
                    }
                  />
                </FormControl>
                <FormDescription>
                  {t('Applies to users following the system default. Allowed range: 1–10000. Default: 5.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className='border-t pt-4'>
            <Button
              type='button'
              variant='outline'
              onClick={() => setResetOpen(true)}
            >
              <RotateCcw data-icon='inline-start' />
              {t('Reset all users to system default')}
            </Button>
          </div>
        </SettingsForm>
      </Form>
      <ConfirmDialog
        open={resetOpen}
        onOpenChange={setResetOpen}
        title={t('Reset all user concurrency limits')}
        desc={t('This clears every custom user concurrency limit and makes all users follow the current system default. Continue?')}
        confirmText={t('Reset all users')}
        handleConfirm={handleReset}
        isLoading={resetting}
        destructive
      />
    </SettingsSection>
  )
}