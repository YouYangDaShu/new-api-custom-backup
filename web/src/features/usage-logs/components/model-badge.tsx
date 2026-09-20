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
import { AlertTriangle, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { isResponseModelMismatch } from '../lib/response-model'
import type { LogOtherData } from '../types'

type ResponseModelObservation = NonNullable<
  NonNullable<LogOtherData['admin_info']>['response_model']
>

interface ModelBadgeProps {
  modelName: string
  actualModel?: string
  responseModel?: ResponseModelObservation
  className?: string
  wrapText?: boolean
  onInspect?: () => void
}

interface ModelProvider {
  icon: string
  label: string
}

function resolveModelProvider(modelName: string): ModelProvider | null {
  const model = modelName.toLowerCase()
  const hasAny = (keywords: string[]) =>
    keywords.some((keyword) => model.includes(keyword))

  if (
    hasAny([
      'gpt-',
      'chatgpt-',
      'text-embedding-',
      'omni-moderation',
      'dall-e',
      'whisper',
      'tts-',
    ]) ||
    /\bo[134](?:-|$)/.test(model)
  ) {
    return { icon: 'OpenAI.Color', label: 'OpenAI' }
  }
  if (hasAny(['claude-', 'anthropic'])) {
    return { icon: 'Claude.Color', label: 'Claude' }
  }
  if (hasAny(['gemini-', 'learnlm-'])) {
    return { icon: 'Gemini.Color', label: 'Gemini' }
  }
  if (hasAny(['grok-', 'xai-'])) {
    return { icon: 'Grok.Color', label: 'Grok' }
  }
  if (hasAny(['deepseek-'])) {
    return { icon: 'DeepSeek.Color', label: 'DeepSeek' }
  }
  if (hasAny(['qwen', 'qwq-'])) {
    return { icon: 'Qwen.Color', label: 'Qwen' }
  }
  if (hasAny(['doubao-', 'volcengine'])) {
    return { icon: 'Doubao.Color', label: 'Doubao' }
  }
  if (hasAny(['moonshot-', 'kimi-'])) {
    return { icon: 'Moonshot.Color', label: 'Moonshot' }
  }
  if (hasAny(['minimax', 'abab'])) {
    return { icon: 'Minimax.Color', label: 'MiniMax' }
  }
  if (hasAny(['glm-', 'chatglm', 'cogview', 'cogvideo'])) {
    return { icon: 'Zhipu.Color', label: 'Zhipu' }
  }
  if (hasAny(['mimo-'])) {
    return { icon: 'XiaomiMiMo', label: 'MiMo' }
  }
  if (hasAny(['ernie'])) {
    return { icon: 'Wenxin.Color', label: 'Baidu' }
  }
  if (hasAny(['spark'])) {
    return { icon: 'Spark.Color', label: 'iFlyTek' }
  }
  if (hasAny(['hunyuan'])) {
    return { icon: 'Hunyuan.Color', label: 'Tencent' }
  }
  if (hasAny(['baichuan'])) {
    return { icon: 'Baichuan.Color', label: 'Baichuan' }
  }
  if (hasAny(['internlm'])) {
    return { icon: 'InternLM.Color', label: 'InternLM' }
  }
  if (hasAny(['step-'])) {
    return { icon: 'Stepfun.Color', label: 'StepFun' }
  }
  if (hasAny(['yi-'])) {
    return { icon: 'Yi.Color', label: 'Yi' }
  }
  if (hasAny(['mistral-', 'mixtral-'])) {
    return { icon: 'Mistral.Color', label: 'Mistral' }
  }
  if (hasAny(['llama-', 'meta-'])) {
    return { icon: 'Meta.Color', label: 'Meta' }
  }
  if (hasAny(['command-', 'cohere-'])) {
    return { icon: 'Cohere.Color', label: 'Cohere' }
  }

  return null
}

function ModelBadgeContent(props: ModelBadgeProps) {
  const provider = resolveModelProvider(props.modelName)

  return (
    <StatusBadge
      copyText={props.modelName}
      copyable={!props.onInspect}
      size='sm'
      showDot={!provider}
      autoColor={provider ? undefined : props.modelName}
      className={cn(
        'border-border/60 bg-muted/30 h-6 max-w-none gap-1.5 rounded-md border px-2 [font-family:var(--font-body)]',
        provider && 'text-foreground',
        props.wrapText && 'h-auto min-h-6 max-w-full py-0.5 whitespace-normal',
        props.className
      )}
    >
      <span
        className={cn(
          'flex items-center gap-1.5',
          props.wrapText ? 'max-w-full min-w-0' : 'max-w-none'
        )}
      >
        {provider && (
          <span
            className='flex h-[18px] w-[18px] shrink-0 items-center justify-center'
            title={provider.label}
            aria-label={provider.label}
          >
            {getLobeIcon(provider.icon, 18)}
          </span>
        )}
        <span
          className={
            props.wrapText
              ? 'line-clamp-2 [overflow-wrap:anywhere]'
              : 'whitespace-nowrap'
          }
        >
          {props.modelName}
        </span>
      </span>
    </StatusBadge>
  )
}

export function ModelBadge(props: ModelBadgeProps) {
  const { t } = useTranslation()
  const mismatch = isResponseModelMismatch(props.responseModel)
  const hasDetails = !!props.actualModel || mismatch

  if (props.onInspect) {
    return (
      <Button
        variant='ghost'
        aria-label={`${t('Model')}: ${props.modelName}`}
        aria-haspopup='dialog'
        onClick={props.onInspect}
        className='h-auto min-h-8 max-w-full min-w-0 justify-start gap-1 px-0 py-0 text-left font-normal whitespace-normal'
      >
        <ModelBadgeContent {...props} />
        {mismatch ? (
          <AlertTriangle
            aria-label={t('Response model: {{model}}', {
              model: props.responseModel?.returned_model,
            })}
            className='size-3 shrink-0 text-amber-500'
          />
        ) : (
          props.actualModel && (
            <Route className='text-muted-foreground size-3 shrink-0' />
          )
        )}
      </Button>
    )
  }

  if (!hasDetails) {
    return <ModelBadgeContent {...props} />
  }

  return (
    <Popover>
      <PopoverTrigger
        render={
          <button type='button' className='inline-flex items-center gap-1' />
        }
      >
        <ModelBadgeContent {...props} />
        {mismatch ? (
          <AlertTriangle
            aria-label={t('Response model: {{model}}', {
              model: props.responseModel?.returned_model,
            })}
            className='size-3 shrink-0 text-amber-500'
          />
        ) : (
          <Route className='text-muted-foreground size-3 shrink-0' />
        )}
      </PopoverTrigger>
      <PopoverContent className='w-80'>
        {mismatch && props.responseModel ? (
          <ResponseModelDetails observation={props.responseModel} />
        ) : (
          <div className='space-y-2'>
            <ModelDetailRow
              label={t('Request Model:')}
              value={props.modelName}
            />
            {props.actualModel && (
              <ModelDetailRow
                label={t('Actual Model:')}
                value={props.actualModel}
              />
            )}
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}

function ModelDetailRow(props: { label: string; value: string }) {
  return (
    <div className='flex items-start justify-between gap-3'>
      <span className='text-muted-foreground text-xs'>{props.label}</span>
      <span className='min-w-0 text-right font-mono text-xs font-medium break-all'>
        {props.value}
      </span>
    </div>
  )
}

export function ResponseModelDetails(props: {
  observation: ResponseModelObservation
}) {
  const { t } = useTranslation()
  const mismatch = isResponseModelMismatch(props.observation)

  return (
    <div className='space-y-2'>
      {mismatch && (
        <StatusBadge
          icon={AlertTriangle}
          label={t('Response model: {{model}}', {
            model: props.observation.returned_model,
          })}
          variant='warning'
          copyable={false}
          className='h-auto max-w-full whitespace-normal'
        />
      )}
      <ModelDetailRow
        label={t('Request Model:')}
        value={props.observation.requested_model}
      />
      <ModelDetailRow
        label={t('Upstream Model:')}
        value={props.observation.upstream_model}
      />
      <ModelDetailRow
        label={t('Response Model:')}
        value={props.observation.returned_model}
      />
      {mismatch && (
        <p className='text-muted-foreground text-xs'>
          {t('The upstream response model differs from the final model sent.')}
        </p>
      )}
    </div>
  )
}
