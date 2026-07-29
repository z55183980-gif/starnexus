/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Add01Icon,
  AiCloud01Icon,
  AmazonIcon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  ClaudeIcon,
  ComputerTerminal01Icon,
  Delete02Icon,
  GoogleIcon,
  Key01Icon,
  Link01Icon,
  Loading03Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  completeUpstreamOAuth,
  createUpstreamAccount,
  startUpstreamOAuth,
  updateUpstreamAccount,
} from './api'
import type {
  UpstreamAccount,
  UpstreamAccountPayload,
  UpstreamAccountPool,
  UpstreamAccountType,
  UpstreamPlatform,
} from './types'

type OpenAIWebSocketMode = 'off' | 'ctx_pool' | 'passthrough' | 'http_bridge'
type OpenAICompactMode = 'auto' | 'force_on' | 'force_off'
type OpenAIResponsesMode = 'auto' | 'force_responses' | 'force_chat_completions'
type OpenAIEndpointCapability = 'chat_completions' | 'embeddings'
type AnthropicAPIKeyAuthScheme = 'x_api_key' | 'authorization_bearer'
type ModelMapping = { from: string; to: string }

type AccountExtra = {
  intercept_warmup_requests?: boolean
  openai_passthrough?: boolean
  openai_oauth_responses_websockets_v2_mode?: OpenAIWebSocketMode
  openai_apikey_responses_websockets_v2_mode?: OpenAIWebSocketMode
  openai_long_context_billing_enabled?: boolean
  codex_cli_only?: boolean
  codex_cli_only_allow_app_server?: boolean
  openai_compact_mode?: OpenAICompactMode
  compact_model_mapping?: Record<string, string>
  openai_responses_mode?: OpenAIResponsesMode
  openai_capabilities?: OpenAIEndpointCapability[]
  anthropic_passthrough?: boolean
  anthropic_apikey_auth_scheme?: AnthropicAPIKeyAuthScheme
  [key: string]: unknown
}

type AccountDraft = {
  name: string
  notes: string
  platform: UpstreamPlatform
  type: UpstreamAccountType
  apiKey: string
  baseUrl: string
  oauthInput: string
  proxyId: string
  concurrency: string
  priority: string
  weight: string
  loadFactor: string
  rateMultiplier: string
  expiresAt: string
  autoPauseOnExpired: boolean
  status: 'active' | 'inactive' | 'error' | 'expired'
  schedulable: boolean
  starnexusOwnsOAuthRefresh: boolean
  poolIds: number[]
  bedrockAuthMode: 'sigv4' | 'api_key'
  bedrockRegion: string
  bedrockAccessKeyId: string
  bedrockSecretAccessKey: string
  bedrockSessionToken: string
  bedrockApiKey: string
  vertexServiceAccountJson: string
  vertexLocation: string
  interceptWarmupRequests: boolean
  openaiPassthrough: boolean
  openaiWebSocketMode: OpenAIWebSocketMode
  openaiLongContextBilling: boolean
  codexCLIOnly: boolean
  codexCLIOnlyAllowAppServer: boolean
  openaiCompactMode: OpenAICompactMode
  compactModelMappings: ModelMapping[]
  openaiResponsesMode: OpenAIResponsesMode
  openaiEndpointCapabilities: OpenAIEndpointCapability[]
  anthropicPassthrough: boolean
  anthropicAPIKeyAuthScheme: AnthropicAPIKeyAuthScheme
}

const defaultOpenAIEndpointCapabilities: OpenAIEndpointCapability[] = [
  'chat_completions',
  'embeddings',
]

function parseAccountExtra(extra?: string | null): AccountExtra {
  if (!extra?.trim()) return {}
  try {
    const parsed = JSON.parse(extra) as unknown
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as AccountExtra)
      : {}
  } catch {
    return {}
  }
}

function timestampToLocalInput(timestamp?: number | null) {
  if (!timestamp) return ''
  const date = new Date(timestamp * 1000)
  const localDate = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return localDate.toISOString().slice(0, 16)
}

function compactMappingsFromExtra(extra: AccountExtra): ModelMapping[] {
  if (!extra.compact_model_mapping) return []
  return Object.entries(extra.compact_model_mapping).map(([from, to]) => ({
    from,
    to,
  }))
}

function accountDraft(account?: UpstreamAccount | null): AccountDraft {
  const extra = parseAccountExtra(account?.extra)
  const websocketMode =
    account?.type === 'apikey'
      ? extra.openai_apikey_responses_websockets_v2_mode
      : extra.openai_oauth_responses_websockets_v2_mode
  return {
    name: account?.name ?? '',
    notes: account?.notes ?? '',
    platform: account?.platform ?? 'openai',
    type: account?.type ?? 'oauth',
    apiKey: '',
    baseUrl: '',
    oauthInput: '',
    proxyId: account?.proxy_id ? String(account.proxy_id) : '',
    concurrency: String(account?.concurrency ?? 1),
    priority: String(account?.priority ?? 50),
    weight: String(account?.weight ?? 1),
    loadFactor:
      account?.load_factor === null || account?.load_factor === undefined
        ? ''
        : String(account.load_factor),
    rateMultiplier:
      account?.rate_multiplier === null ||
      account?.rate_multiplier === undefined
        ? '1'
        : String(account.rate_multiplier),
    expiresAt: timestampToLocalInput(account?.expires_at),
    autoPauseOnExpired: account?.auto_pause_on_expired ?? true,
    status: account?.status ?? 'active',
    schedulable: account?.schedulable ?? true,
    starnexusOwnsOAuthRefresh: account?.oauth_refresh_owner !== 'external',
    poolIds: account?.pool_ids ?? [],
    bedrockAuthMode: 'sigv4',
    bedrockRegion: 'us-east-1',
    bedrockAccessKeyId: '',
    bedrockSecretAccessKey: '',
    bedrockSessionToken: '',
    bedrockApiKey: '',
    vertexServiceAccountJson: '',
    vertexLocation: 'global',
    interceptWarmupRequests: extra.intercept_warmup_requests === true,
    openaiPassthrough: extra.openai_passthrough === true,
    openaiWebSocketMode: websocketMode || 'off',
    openaiLongContextBilling:
      extra.openai_long_context_billing_enabled === true,
    codexCLIOnly: extra.codex_cli_only === true,
    codexCLIOnlyAllowAppServer: extra.codex_cli_only_allow_app_server === true,
    openaiCompactMode: extra.openai_compact_mode || 'auto',
    compactModelMappings: compactMappingsFromExtra(extra),
    openaiResponsesMode: extra.openai_responses_mode || 'auto',
    openaiEndpointCapabilities: extra.openai_capabilities?.length
      ? extra.openai_capabilities
      : [...defaultOpenAIEndpointCapabilities],
    anthropicPassthrough: extra.anthropic_passthrough === true,
    anthropicAPIKeyAuthScheme:
      extra.anthropic_apikey_auth_scheme || 'x_api_key',
  }
}

function isOAuthAccountType(type: UpstreamAccountType) {
  return type === 'oauth' || type === 'setup_token'
}

export function AccountDialog({
  open,
  onOpenChange,
  account,
  pools,
  proxies,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  account: UpstreamAccount | null
  pools: UpstreamAccountPool[]
  proxies: Array<{ id: number; name: string }>
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<AccountDraft>(() => accountDraft(account))
  const [step, setStep] = useState(account ? 2 : 1)
  const [busy, setBusy] = useState(false)
  const [oauthStarted, setOAuthStarted] = useState(false)
  const contentRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    setDraft(accountDraft(account))
    setStep(account ? 2 : 1)
    setOAuthStarted(false)
  }, [account, open])

  useEffect(() => {
    contentRef.current?.scrollTo({ top: 0 })
  }, [step])

  const set = <K extends keyof AccountDraft>(key: K, value: AccountDraft[K]) =>
    setDraft((current) => ({ ...current, [key]: value }))

  const compatiblePools = useMemo(
    () =>
      pools.filter(
        (pool) =>
          pool.platform === draft.platform &&
          (pool.credential_type === 'mixed' ||
            pool.credential_type === draft.type)
      ),
    [draft.platform, draft.type, pools]
  )

  const accountChoices = useMemo(() => {
    if (draft.platform === 'openai') {
      return [
        {
          value: 'oauth' as const,
          title: t('Codex OAuth'),
          description: t('Authorize a ChatGPT account for Codex requests'),
          icon: ComputerTerminal01Icon,
        },
        {
          value: 'apikey' as const,
          title: t('OpenAI API Key'),
          description: t('Use a standard OpenAI API key'),
          icon: Key01Icon,
        },
      ]
    }
    return [
      {
        value: 'oauth' as const,
        title: t('Claude Code'),
        description: t('OAuth or Setup Token'),
        icon: ClaudeIcon,
      },
      {
        value: 'apikey' as const,
        title: t('Claude Console'),
        description: t('Anthropic API Key'),
        icon: Key01Icon,
      },
      {
        value: 'bedrock' as const,
        title: t('AWS Bedrock'),
        description: t('SigV4 or API Key'),
        icon: AmazonIcon,
      },
      {
        value: 'service_account' as const,
        title: t('Vertex AI'),
        description: t('Google Service Account'),
        icon: GoogleIcon,
      },
    ]
  }, [draft.platform, t])

  const selectedChoice =
    draft.platform === 'anthropic' && draft.type === 'setup_token'
      ? 'oauth'
      : draft.type

  const changePlatform = (platform: UpstreamPlatform) => {
    if (account || platform === draft.platform) return
    setDraft((current) => ({
      ...current,
      platform,
      type: 'oauth',
      poolIds: [],
    }))
  }

  const changeAccountType = (type: UpstreamAccountType) => {
    if (account) return
    setDraft((current) => ({ ...current, type, poolIds: [] }))
  }

  const startOAuth = async () => {
    setBusy(true)
    try {
      const response = await startUpstreamOAuth({
        account_id: account?.id,
        proxy_id: draft.proxyId ? Number(draft.proxyId) : null,
        platform: draft.platform,
        credential_type: draft.type === 'setup_token' ? 'setup_token' : 'oauth',
      })
      if (!response.success || !response.data?.authorize_url) {
        throw new Error(response.message || t('Failed to start authorization'))
      }
      window.open(response.data.authorize_url, '_blank', 'noopener,noreferrer')
      setOAuthStarted(true)
      toast.success(t('Authorization page opened'))
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Request failed'))
    } finally {
      setBusy(false)
    }
  }

  const nextStep = () => {
    if (!draft.name.trim()) {
      toast.error(t('Account name is required'))
      return
    }
    setStep(2)
  }

  const buildCredentialPayload = (): Record<string, string> | undefined => {
    if (draft.type === 'apikey') {
      if (!draft.apiKey.trim()) return undefined
      const credentials: Record<string, string> = {
        api_key: draft.apiKey.trim(),
      }
      if (draft.baseUrl.trim()) credentials.base_url = draft.baseUrl.trim()
      return credentials
    }
    if (draft.type === 'bedrock') {
      const region = draft.bedrockRegion.trim()
      if (!region) throw new Error(t('AWS region is required'))
      if (draft.bedrockAuthMode === 'api_key') {
        if (!draft.bedrockApiKey.trim()) return undefined
        return {
          auth_mode: 'api_key',
          aws_region: region,
          api_key: draft.bedrockApiKey.trim(),
        }
      }
      if (
        !draft.bedrockAccessKeyId.trim() &&
        !draft.bedrockSecretAccessKey.trim()
      ) {
        return undefined
      }
      if (
        !draft.bedrockAccessKeyId.trim() ||
        !draft.bedrockSecretAccessKey.trim()
      ) {
        throw new Error(
          t('AWS access key ID and secret access key are required')
        )
      }
      const credentials: Record<string, string> = {
        auth_mode: 'sigv4',
        aws_region: region,
        aws_access_key_id: draft.bedrockAccessKeyId.trim(),
        aws_secret_access_key: draft.bedrockSecretAccessKey.trim(),
      }
      if (draft.bedrockSessionToken.trim()) {
        credentials.aws_session_token = draft.bedrockSessionToken.trim()
      }
      return credentials
    }
    if (draft.type === 'service_account') {
      const raw = draft.vertexServiceAccountJson.trim()
      if (!raw) return undefined
      let parsed: { project_id?: string; client_email?: string }
      try {
        parsed = JSON.parse(raw) as typeof parsed
      } catch {
        throw new Error(t('Service account JSON is invalid'))
      }
      if (!draft.vertexLocation.trim()) {
        throw new Error(t('Vertex location is required'))
      }
      return {
        service_account_json: raw,
        project_id: parsed.project_id || '',
        client_email: parsed.client_email || '',
        location: draft.vertexLocation.trim(),
        tier_id: 'vertex',
      }
    }
    return undefined
  }

  const buildAccountExtra = () => {
    const extra = parseAccountExtra(account?.extra)
    const managedKeys = [
      'intercept_warmup_requests',
      'openai_passthrough',
      'openai_oauth_passthrough',
      'openai_oauth_responses_websockets_v2_mode',
      'openai_oauth_responses_websockets_v2_enabled',
      'openai_apikey_responses_websockets_v2_mode',
      'openai_apikey_responses_websockets_v2_enabled',
      'responses_websockets_v2_enabled',
      'openai_ws_enabled',
      'openai_long_context_billing_enabled',
      'codex_cli_only',
      'codex_cli_only_allow_app_server',
      'codex_cli_only_allowed_clients',
      'openai_compact_mode',
      'compact_model_mapping',
      'openai_responses_mode',
      'openai_capabilities',
      'anthropic_passthrough',
      'anthropic_apikey_auth_scheme',
    ]
    managedKeys.forEach((key) => delete extra[key])

    if (draft.interceptWarmupRequests) {
      extra.intercept_warmup_requests = true
    }

    if (draft.platform === 'openai') {
      if (draft.openaiPassthrough) extra.openai_passthrough = true
      extra.openai_long_context_billing_enabled = draft.openaiLongContextBilling
      if (draft.type === 'oauth') {
        extra.openai_oauth_responses_websockets_v2_mode =
          draft.openaiWebSocketMode
        if (draft.codexCLIOnly) {
          extra.codex_cli_only = true
          if (draft.codexCLIOnlyAllowAppServer) {
            extra.codex_cli_only_allow_app_server = true
          }
        }
      } else if (draft.type === 'apikey') {
        extra.openai_apikey_responses_websockets_v2_mode =
          draft.openaiWebSocketMode
        if (
          draft.openaiEndpointCapabilities.includes('chat_completions') &&
          draft.openaiResponsesMode !== 'auto'
        ) {
          extra.openai_responses_mode = draft.openaiResponsesMode
        }
      }
      if (draft.openaiCompactMode !== 'auto') {
        extra.openai_compact_mode = draft.openaiCompactMode
      }
      const compactModelMapping: Record<string, string> = {}
      for (const mapping of draft.compactModelMappings) {
        const from = mapping.from.trim()
        const to = mapping.to.trim()
        if (!from && !to) continue
        if (!from || !to) {
          throw new Error(t('Compact model mappings require both models'))
        }
        if (compactModelMapping[from]) {
          throw new Error(t('Compact model mapping sources must be unique'))
        }
        compactModelMapping[from] = to
      }
      if (Object.keys(compactModelMapping).length > 0) {
        extra.compact_model_mapping = compactModelMapping
      }
      const capabilities = defaultOpenAIEndpointCapabilities.filter((value) =>
        draft.openaiEndpointCapabilities.includes(value)
      )
      if (capabilities.length !== defaultOpenAIEndpointCapabilities.length) {
        extra.openai_capabilities = capabilities
      }
    }

    if (draft.platform === 'anthropic' && draft.type === 'apikey') {
      if (draft.anthropicPassthrough) extra.anthropic_passthrough = true
      if (draft.anthropicAPIKeyAuthScheme !== 'x_api_key') {
        extra.anthropic_apikey_auth_scheme = draft.anthropicAPIKeyAuthScheme
      }
    }

    return JSON.stringify(extra)
  }

  const toggleOpenAIEndpointCapability = (
    capability: OpenAIEndpointCapability
  ) => {
    const selected = draft.openaiEndpointCapabilities.includes(capability)
    if (selected && draft.openaiEndpointCapabilities.length === 1) return
    set(
      'openaiEndpointCapabilities',
      selected
        ? draft.openaiEndpointCapabilities.filter(
            (value) => value !== capability
          )
        : [...draft.openaiEndpointCapabilities, capability]
    )
    if (capability === 'chat_completions' && selected) {
      set('openaiResponsesMode', 'auto')
    }
  }

  const updateCompactMapping = (
    index: number,
    key: keyof ModelMapping,
    value: string
  ) => {
    set(
      'compactModelMappings',
      draft.compactModelMappings.map((mapping, mappingIndex) =>
        mappingIndex === index ? { ...mapping, [key]: value } : mapping
      )
    )
  }

  const submit = async () => {
    if (!draft.name.trim()) {
      toast.error(t('Account name is required'))
      return
    }
    setBusy(true)
    try {
      const oauthType = isOAuthAccountType(draft.type)
      const loadFactor = draft.loadFactor.trim()
        ? Number(draft.loadFactor)
        : null
      const rateMultiplier = draft.rateMultiplier.trim()
        ? Number(draft.rateMultiplier)
        : 1
      if (
        loadFactor !== null &&
        (!Number.isFinite(loadFactor) || loadFactor < 1)
      ) {
        throw new Error(t('Load factor must be at least 1'))
      }
      if (!Number.isFinite(rateMultiplier) || rateMultiplier < 0) {
        throw new Error(t('Billing rate multiplier cannot be negative'))
      }
      const expiresAt = draft.expiresAt
        ? Math.floor(new Date(draft.expiresAt).getTime() / 1000)
        : null
      if (draft.expiresAt && !Number.isFinite(expiresAt)) {
        throw new Error(t('Expiration time is invalid'))
      }
      const payload: UpstreamAccountPayload = {
        name: draft.name.trim(),
        notes: draft.notes.trim(),
        platform: draft.platform,
        type: draft.type,
        extra: buildAccountExtra(),
        proxy_id: draft.proxyId ? Number(draft.proxyId) : null,
        concurrency: Math.max(1, Number(draft.concurrency) || 1),
        priority: Number(draft.priority) || 0,
        weight: Math.max(1, Number(draft.weight) || 1),
        load_factor: loadFactor,
        rate_multiplier: rateMultiplier,
        status: draft.status,
        schedulable: draft.schedulable,
        expires_at: expiresAt,
        auto_pause_on_expired: draft.autoPauseOnExpired,
        oauth_refresh_owner:
          oauthType && draft.starnexusOwnsOAuthRefresh
            ? 'starnexus'
            : 'external',
        pool_ids: draft.poolIds,
      }

      if (oauthType) {
        if (draft.oauthInput.trim() && !draft.starnexusOwnsOAuthRefresh) {
          throw new Error(
            t(
              'Enable StarNexus OAuth refresh ownership before reauthorizing this account.'
            )
          )
        }
        if (!account && !draft.oauthInput.trim()) {
          throw new Error(t('Paste the OAuth callback URL or code'))
        }
        let accountId = account?.id
        if (draft.oauthInput.trim()) {
          const oauthResponse = await completeUpstreamOAuth({
            input: draft.oauthInput.trim(),
            name: draft.name.trim(),
            pool_ids: account ? draft.poolIds : [],
            proxy_id: draft.proxyId ? Number(draft.proxyId) : undefined,
          })
          if (!oauthResponse.success || !oauthResponse.data?.id) {
            throw new Error(oauthResponse.message || t('Save failed'))
          }
          accountId = oauthResponse.data.id
        }
        if (!accountId) throw new Error(t('Save failed'))
        const response = await updateUpstreamAccount(accountId, payload)
        if (!response.success) {
          throw new Error(response.message || t('Save failed'))
        }
      } else {
        payload.credentials = buildCredentialPayload()
        if (!account && !payload.credentials) {
          throw new Error(t('Credentials are required'))
        }
        const response = account
          ? await updateUpstreamAccount(account.id, payload)
          : await createUpstreamAccount(payload)
        if (!response.success) {
          throw new Error(response.message || t('Save failed'))
        }
      }
      toast.success(account ? t('Account updated') : t('Account created'))
      onSaved()
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Save failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='flex max-h-[92vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl'>
        <DialogHeader className='shrink-0 border-b px-6 py-5 pr-12'>
          <DialogTitle>
            {account ? t('Edit account') : t('Add account')}
          </DialogTitle>
          <DialogDescription>
            {account
              ? t('Update account credentials and scheduling settings.')
              : t(
                  'Choose an authorization method, then complete the account authorization.'
                )}
          </DialogDescription>
        </DialogHeader>

        <div
          ref={contentRef}
          className='min-h-0 flex-1 overflow-y-auto px-6 py-4'
        >
          {!account && (
            <div className='flex items-center justify-center gap-3 pb-4'>
              <div className='flex items-center gap-2 text-sm font-medium'>
                <span
                  className={
                    step === 1
                      ? 'bg-primary text-primary-foreground flex size-7 items-center justify-center rounded-full'
                      : 'bg-muted text-muted-foreground flex size-7 items-center justify-center rounded-full'
                  }
                >
                  1
                </span>
                <span>{t('Authorization method')}</span>
              </div>
              <div className='bg-border h-px w-10' />
              <div className='flex items-center gap-2 text-sm font-medium'>
                <span
                  className={
                    step === 2
                      ? 'bg-primary text-primary-foreground flex size-7 items-center justify-center rounded-full'
                      : 'bg-muted text-muted-foreground flex size-7 items-center justify-center rounded-full'
                  }
                >
                  2
                </span>
                <span>{t('Account authorization')}</span>
              </div>
            </div>
          )}

          {step === 1 ? (
            <FieldGroup>
              <div className='grid gap-4 sm:grid-cols-2'>
                <Field>
                  <FieldLabel htmlFor='account-name'>
                    {t('Account name')}
                  </FieldLabel>
                  <Input
                    id='account-name'
                    value={draft.name}
                    placeholder={t('Enter account name')}
                    onChange={(event) => set('name', event.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor='account-notes'>{t('Notes')}</FieldLabel>
                  <Input
                    id='account-notes'
                    value={draft.notes}
                    placeholder={t('Optional notes')}
                    onChange={(event) => set('notes', event.target.value)}
                  />
                </Field>
              </div>

              <Field>
                <FieldLabel>{t('Platform')}</FieldLabel>
                <ToggleGroup
                  variant='outline'
                  value={[draft.platform]}
                  onValueChange={(values) => {
                    const platform = values[0] as UpstreamPlatform | undefined
                    if (platform) changePlatform(platform)
                  }}
                  className='grid w-full grid-cols-2'
                >
                  <ToggleGroupItem value='openai' className='w-full gap-2'>
                    <HugeiconsIcon icon={AiCloud01Icon} strokeWidth={2} />
                    OpenAI
                  </ToggleGroupItem>
                  <ToggleGroupItem value='anthropic' className='w-full gap-2'>
                    <HugeiconsIcon icon={ClaudeIcon} strokeWidth={2} />
                    Anthropic
                  </ToggleGroupItem>
                </ToggleGroup>
              </Field>

              <Field>
                <FieldLabel>{t('Account type')}</FieldLabel>
                <ToggleGroup
                  variant='outline'
                  value={[selectedChoice]}
                  onValueChange={(values) => {
                    const type = values[0] as UpstreamAccountType | undefined
                    if (type) changeAccountType(type)
                  }}
                  className='grid w-full grid-cols-1 gap-2 sm:grid-cols-2'
                >
                  {accountChoices.map((choice) => (
                    <ToggleGroupItem
                      key={choice.value}
                      value={choice.value}
                      className='data-pressed:border-primary data-pressed:bg-primary/5 h-auto min-h-20 w-full items-start justify-start gap-3 rounded-lg border px-4 py-3 text-left whitespace-normal'
                    >
                      <HugeiconsIcon
                        icon={choice.icon}
                        className='mt-0.5 size-5 shrink-0'
                        strokeWidth={2}
                      />
                      <span className='flex min-w-0 flex-col items-start gap-1'>
                        <span className='font-medium'>{choice.title}</span>
                        <span className='text-muted-foreground text-xs font-normal'>
                          {choice.description}
                        </span>
                      </span>
                    </ToggleGroupItem>
                  ))}
                </ToggleGroup>
              </Field>

              {draft.platform === 'anthropic' &&
                (draft.type === 'oauth' || draft.type === 'setup_token') && (
                  <Field>
                    <FieldLabel>{t('Add method')}</FieldLabel>
                    <RadioGroup
                      value={draft.type}
                      onValueChange={(value) =>
                        changeAccountType(value as 'oauth' | 'setup_token')
                      }
                      className='grid gap-3 sm:grid-cols-2'
                    >
                      <label className='has-data-checked:border-primary flex cursor-pointer items-start gap-3 rounded-lg border p-3'>
                        <RadioGroupItem value='oauth' className='mt-0.5' />
                        <span className='flex flex-col gap-1'>
                          <span className='text-sm font-medium'>
                            {t('OAuth')}
                          </span>
                          <span className='text-muted-foreground text-xs'>
                            {t(
                              'Full account authorization with profile and inference scopes'
                            )}
                          </span>
                        </span>
                      </label>
                      <label className='has-data-checked:border-primary flex cursor-pointer items-start gap-3 rounded-lg border p-3'>
                        <RadioGroupItem
                          value='setup_token'
                          className='mt-0.5'
                        />
                        <span className='flex flex-col gap-1'>
                          <span className='text-sm font-medium'>
                            {t('Setup Token')}
                          </span>
                          <span className='text-muted-foreground text-xs'>
                            {t(
                              'Long-lived inference authorization for Claude Code'
                            )}
                          </span>
                        </span>
                      </label>
                    </RadioGroup>
                  </Field>
                )}
            </FieldGroup>
          ) : (
            <FieldGroup>
              <div className='bg-muted/40 flex flex-wrap items-center gap-2 rounded-lg border p-3'>
                <Badge variant='outline'>{draft.platform}</Badge>
                <Badge variant='secondary'>
                  {draft.type === 'setup_token' ? t('Setup Token') : draft.type}
                </Badge>
                <span className='min-w-0 flex-1 truncate text-sm font-medium'>
                  {draft.name}
                </span>
              </div>

              {isOAuthAccountType(draft.type) && (
                <>
                  <Field>
                    <div className='flex flex-wrap items-center justify-between gap-3'>
                      <div>
                        <FieldLabel htmlFor='oauth-result'>
                          {draft.platform === 'openai'
                            ? t('Codex OAuth')
                            : draft.type === 'setup_token'
                              ? t('Claude Setup Token authorization')
                              : t('Claude account authorization')}
                        </FieldLabel>
                        <FieldDescription>
                          {t(
                            'Open the authorization page, then paste the callback URL or code below.'
                          )}
                        </FieldDescription>
                      </div>
                      <Button
                        type='button'
                        variant='outline'
                        onClick={startOAuth}
                        disabled={busy || !draft.starnexusOwnsOAuthRefresh}
                      >
                        <HugeiconsIcon icon={Link01Icon} strokeWidth={2} />
                        {t(
                          account || oauthStarted
                            ? 'Open authorization again'
                            : 'Start authorization'
                        )}
                      </Button>
                    </div>
                    <Textarea
                      id='oauth-result'
                      rows={4}
                      value={draft.oauthInput}
                      placeholder={t(
                        'Paste the callback URL, authorization code, or code#state'
                      )}
                      onChange={(event) =>
                        set('oauthInput', event.target.value)
                      }
                    />
                    <FieldDescription>
                      {t(
                        'Authorization state is encrypted on the server and can be completed on another instance.'
                      )}
                    </FieldDescription>
                  </Field>
                  <Field
                    orientation='horizontal'
                    className='items-center justify-between rounded-lg border p-3'
                  >
                    <div className='flex flex-col gap-1'>
                      <FieldLabel htmlFor='oauth-refresh-owner'>
                        {t('StarNexus manages OAuth refresh')}
                      </FieldLabel>
                      <FieldDescription>
                        {t(
                          'Keep this off while another system uses the same OAuth account to avoid refresh token conflicts.'
                        )}
                      </FieldDescription>
                    </div>
                    <Switch
                      id='oauth-refresh-owner'
                      checked={draft.starnexusOwnsOAuthRefresh}
                      onCheckedChange={(checked) =>
                        set('starnexusOwnsOAuthRefresh', checked)
                      }
                    />
                  </Field>
                </>
              )}

              {draft.type === 'apikey' && (
                <div className='grid gap-4 sm:grid-cols-2'>
                  <Field>
                    <FieldLabel htmlFor='account-key'>
                      {t('API Key')}
                    </FieldLabel>
                    <Input
                      id='account-key'
                      type='password'
                      autoComplete='new-password'
                      value={draft.apiKey}
                      placeholder={
                        account ? t('Leave empty to keep the current key') : ''
                      }
                      onChange={(event) => set('apiKey', event.target.value)}
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor='account-base-url'>
                      {t('Base URL')}
                    </FieldLabel>
                    <Input
                      id='account-base-url'
                      value={draft.baseUrl}
                      placeholder={
                        draft.platform === 'openai'
                          ? 'https://api.openai.com'
                          : 'https://api.anthropic.com'
                      }
                      onChange={(event) => set('baseUrl', event.target.value)}
                    />
                  </Field>
                </div>
              )}

              {draft.type === 'bedrock' && (
                <>
                  <Field>
                    <FieldLabel>{t('Authentication method')}</FieldLabel>
                    <RadioGroup
                      value={draft.bedrockAuthMode}
                      onValueChange={(value) =>
                        set(
                          'bedrockAuthMode',
                          value as AccountDraft['bedrockAuthMode']
                        )
                      }
                      className='grid gap-3 sm:grid-cols-2'
                    >
                      <label className='has-data-checked:border-primary flex cursor-pointer items-center gap-3 rounded-lg border p-3'>
                        <RadioGroupItem value='sigv4' />
                        <span className='text-sm font-medium'>AWS SigV4</span>
                      </label>
                      <label className='has-data-checked:border-primary flex cursor-pointer items-center gap-3 rounded-lg border p-3'>
                        <RadioGroupItem value='api_key' />
                        <span className='text-sm font-medium'>
                          {t('Bedrock API Key')}
                        </span>
                      </label>
                    </RadioGroup>
                  </Field>
                  <div className='grid gap-4 sm:grid-cols-2'>
                    <Field>
                      <FieldLabel htmlFor='bedrock-region'>
                        {t('AWS region')}
                      </FieldLabel>
                      <Input
                        id='bedrock-region'
                        value={draft.bedrockRegion}
                        onChange={(event) =>
                          set('bedrockRegion', event.target.value)
                        }
                      />
                    </Field>
                    {draft.bedrockAuthMode === 'api_key' ? (
                      <Field>
                        <FieldLabel htmlFor='bedrock-api-key'>
                          {t('Bedrock API Key')}
                        </FieldLabel>
                        <Input
                          id='bedrock-api-key'
                          type='password'
                          autoComplete='new-password'
                          value={draft.bedrockApiKey}
                          placeholder={
                            account
                              ? t('Leave empty to keep the current key')
                              : ''
                          }
                          onChange={(event) =>
                            set('bedrockApiKey', event.target.value)
                          }
                        />
                      </Field>
                    ) : (
                      <>
                        <Field>
                          <FieldLabel htmlFor='bedrock-access-key'>
                            {t('AWS access key ID')}
                          </FieldLabel>
                          <Input
                            id='bedrock-access-key'
                            value={draft.bedrockAccessKeyId}
                            onChange={(event) =>
                              set('bedrockAccessKeyId', event.target.value)
                            }
                          />
                        </Field>
                        <Field>
                          <FieldLabel htmlFor='bedrock-secret-key'>
                            {t('AWS secret access key')}
                          </FieldLabel>
                          <Input
                            id='bedrock-secret-key'
                            type='password'
                            autoComplete='new-password'
                            value={draft.bedrockSecretAccessKey}
                            onChange={(event) =>
                              set('bedrockSecretAccessKey', event.target.value)
                            }
                          />
                        </Field>
                        <Field>
                          <FieldLabel htmlFor='bedrock-session-token'>
                            {t('AWS session token')}
                          </FieldLabel>
                          <Input
                            id='bedrock-session-token'
                            type='password'
                            autoComplete='new-password'
                            value={draft.bedrockSessionToken}
                            placeholder={t('Optional')}
                            onChange={(event) =>
                              set('bedrockSessionToken', event.target.value)
                            }
                          />
                        </Field>
                      </>
                    )}
                  </div>
                </>
              )}

              {draft.type === 'service_account' && (
                <div className='grid gap-4 sm:grid-cols-[1fr_12rem]'>
                  <Field>
                    <FieldLabel htmlFor='vertex-service-account'>
                      {t('Service account JSON')}
                    </FieldLabel>
                    <Textarea
                      id='vertex-service-account'
                      rows={8}
                      value={draft.vertexServiceAccountJson}
                      placeholder={
                        account
                          ? t('Leave empty to keep the current service account')
                          : '{\n  "type": "service_account",\n  "project_id": "..."\n}'
                      }
                      onChange={(event) =>
                        set('vertexServiceAccountJson', event.target.value)
                      }
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor='vertex-location'>
                      {t('Vertex location')}
                    </FieldLabel>
                    <Input
                      id='vertex-location'
                      value={draft.vertexLocation}
                      onChange={(event) =>
                        set('vertexLocation', event.target.value)
                      }
                    />
                  </Field>
                </div>
              )}

              <div className='grid gap-4 sm:grid-cols-2'>
                <Field>
                  <FieldLabel>{t('Proxy')}</FieldLabel>
                  <Select
                    items={[
                      { value: 'none', label: t('Use pool or channel proxy') },
                      ...proxies.map((proxy) => ({
                        value: String(proxy.id),
                        label: proxy.name,
                      })),
                    ]}
                    value={draft.proxyId || 'none'}
                    onValueChange={(value) =>
                      set('proxyId', value === 'none' ? '' : String(value))
                    }
                  >
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value='none'>
                          {t('Use pool or channel proxy')}
                        </SelectItem>
                        {proxies.map((proxy) => (
                          <SelectItem key={proxy.id} value={String(proxy.id)}>
                            {proxy.name}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field>
                  <FieldLabel>{t('Status')}</FieldLabel>
                  <Select
                    items={[
                      { value: 'active', label: t('Active') },
                      { value: 'inactive', label: t('Inactive') },
                      { value: 'error', label: t('Error') },
                      { value: 'expired', label: t('Expired') },
                    ]}
                    value={draft.status}
                    onValueChange={(value) =>
                      set('status', value as AccountDraft['status'])
                    }
                  >
                    <SelectTrigger className='w-full'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value='active'>{t('Active')}</SelectItem>
                        <SelectItem value='inactive'>
                          {t('Inactive')}
                        </SelectItem>
                        <SelectItem value='error'>{t('Error')}</SelectItem>
                        <SelectItem value='expired'>{t('Expired')}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              </div>

              <div className='grid gap-4 sm:grid-cols-2 lg:grid-cols-5'>
                <Field>
                  <FieldLabel htmlFor='account-concurrency'>
                    {t('Concurrency')}
                  </FieldLabel>
                  <Input
                    id='account-concurrency'
                    type='number'
                    min={1}
                    value={draft.concurrency}
                    onChange={(event) => set('concurrency', event.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor='account-priority'>
                    {t('Priority')}
                  </FieldLabel>
                  <Input
                    id='account-priority'
                    type='number'
                    min={0}
                    value={draft.priority}
                    onChange={(event) => set('priority', event.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor='account-load-factor'>
                    {t('Load factor')}
                  </FieldLabel>
                  <Input
                    id='account-load-factor'
                    type='number'
                    min={1}
                    placeholder={draft.concurrency}
                    value={draft.loadFactor}
                    onChange={(event) => set('loadFactor', event.target.value)}
                  />
                  <FieldDescription>
                    {t('Defaults to concurrency when empty')}
                  </FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor='account-weight'>
                    {t('Weight')}
                  </FieldLabel>
                  <Input
                    id='account-weight'
                    type='number'
                    min={1}
                    value={draft.weight}
                    onChange={(event) => set('weight', event.target.value)}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor='account-rate-multiplier'>
                    {t('Billing rate multiplier')}
                  </FieldLabel>
                  <Input
                    id='account-rate-multiplier'
                    type='number'
                    min={0}
                    step='0.001'
                    value={draft.rateMultiplier}
                    onChange={(event) =>
                      set('rateMultiplier', event.target.value)
                    }
                  />
                </Field>
              </div>

              <Field className='border-t pt-4'>
                <FieldLabel htmlFor='account-expires-at'>
                  {t('Expiration time')}
                </FieldLabel>
                <Input
                  id='account-expires-at'
                  type='datetime-local'
                  value={draft.expiresAt}
                  onChange={(event) => set('expiresAt', event.target.value)}
                />
                <FieldDescription>
                  {t('Leave empty for no expiration')}
                </FieldDescription>
              </Field>

              <Field
                orientation='horizontal'
                className='items-center justify-between border-t pt-4'
              >
                <div className='flex flex-col gap-1'>
                  <FieldLabel htmlFor='account-auto-pause-expired'>
                    {t('Automatically pause scheduling after expiration')}
                  </FieldLabel>
                  <FieldDescription>
                    {t('Expired accounts stop receiving new requests.')}
                  </FieldDescription>
                </div>
                <Switch
                  id='account-auto-pause-expired'
                  checked={draft.autoPauseOnExpired}
                  onCheckedChange={(checked) =>
                    set('autoPauseOnExpired', checked)
                  }
                />
              </Field>

              {draft.platform === 'openai' && (
                <div className='space-y-4 border-t pt-4'>
                  <Field
                    orientation='horizontal'
                    className='items-center justify-between gap-4'
                  >
                    <div className='flex flex-col gap-1'>
                      <FieldLabel htmlFor='openai-passthrough'>
                        {t('OpenAI request passthrough')}
                      </FieldLabel>
                      <FieldDescription>
                        {t(
                          'Forward the original request body and only replace authentication.'
                        )}
                      </FieldDescription>
                    </div>
                    <Switch
                      id='openai-passthrough'
                      checked={draft.openaiPassthrough}
                      onCheckedChange={(checked) =>
                        set('openaiPassthrough', checked)
                      }
                    />
                  </Field>

                  <Field
                    orientation='horizontal'
                    className='items-center justify-between gap-4'
                  >
                    <div className='flex flex-col gap-1'>
                      <FieldLabel>{t('Responses WebSocket mode')}</FieldLabel>
                      <FieldDescription>
                        {t(
                          'Controls the upstream WebSocket transport for this account.'
                        )}
                      </FieldDescription>
                    </div>
                    <Select
                      items={[
                        { value: 'off', label: t('Off') },
                        { value: 'ctx_pool', label: t('Context pool') },
                        { value: 'passthrough', label: t('Passthrough') },
                        { value: 'http_bridge', label: t('HTTP bridge') },
                      ]}
                      value={draft.openaiWebSocketMode}
                      onValueChange={(value) =>
                        set('openaiWebSocketMode', value as OpenAIWebSocketMode)
                      }
                    >
                      <SelectTrigger className='w-48'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value='off'>{t('Off')}</SelectItem>
                        <SelectItem value='ctx_pool'>
                          {t('Context pool')}
                        </SelectItem>
                        <SelectItem value='passthrough'>
                          {t('Passthrough')}
                        </SelectItem>
                        <SelectItem value='http_bridge'>
                          {t('HTTP bridge')}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>

                  <Field
                    orientation='horizontal'
                    className='items-center justify-between gap-4'
                  >
                    <div className='flex flex-col gap-1'>
                      <FieldLabel htmlFor='openai-long-context-billing'>
                        {t('OpenAI long-context billing')}
                      </FieldLabel>
                      <FieldDescription>
                        {t('Apply the upstream long-context pricing policy.')}
                      </FieldDescription>
                    </div>
                    <Switch
                      id='openai-long-context-billing'
                      checked={draft.openaiLongContextBilling}
                      onCheckedChange={(checked) =>
                        set('openaiLongContextBilling', checked)
                      }
                    />
                  </Field>

                  {draft.type === 'oauth' && (
                    <>
                      <Field
                        orientation='horizontal'
                        className='items-center justify-between gap-4'
                      >
                        <div className='flex flex-col gap-1'>
                          <FieldLabel htmlFor='openai-codex-only'>
                            {t('Only allow official Codex clients')}
                          </FieldLabel>
                          <FieldDescription>
                            {t('Restrict this OAuth account to Codex clients.')}
                          </FieldDescription>
                        </div>
                        <Switch
                          id='openai-codex-only'
                          checked={draft.codexCLIOnly}
                          onCheckedChange={(checked) => {
                            set('codexCLIOnly', checked)
                            if (!checked) {
                              set('codexCLIOnlyAllowAppServer', false)
                            }
                          }}
                        />
                      </Field>
                      {draft.codexCLIOnly && (
                        <Field
                          orientation='horizontal'
                          className='ml-4 items-center justify-between gap-4 border-l-2 pl-4'
                        >
                          <div className='flex flex-col gap-1'>
                            <FieldLabel htmlFor='openai-codex-app-server'>
                              {t('Allow Codex app-server clients')}
                            </FieldLabel>
                            <FieldDescription>
                              {t('Also permit Codex app-server requests.')}
                            </FieldDescription>
                          </div>
                          <Switch
                            id='openai-codex-app-server'
                            checked={draft.codexCLIOnlyAllowAppServer}
                            onCheckedChange={(checked) =>
                              set('codexCLIOnlyAllowAppServer', checked)
                            }
                          />
                        </Field>
                      )}
                    </>
                  )}

                  <Field
                    orientation='horizontal'
                    className='items-center justify-between gap-4'
                  >
                    <div className='flex flex-col gap-1'>
                      <FieldLabel>{t('Compact mode')}</FieldLabel>
                      <FieldDescription>
                        {t(
                          'Controls eligibility for /responses/compact requests.'
                        )}
                      </FieldDescription>
                    </div>
                    <Select
                      items={[
                        { value: 'auto', label: t('Automatic') },
                        { value: 'force_on', label: t('Force on') },
                        { value: 'force_off', label: t('Force off') },
                      ]}
                      value={draft.openaiCompactMode}
                      onValueChange={(value) =>
                        set('openaiCompactMode', value as OpenAICompactMode)
                      }
                    >
                      <SelectTrigger className='w-48'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value='auto'>{t('Automatic')}</SelectItem>
                        <SelectItem value='force_on'>
                          {t('Force on')}
                        </SelectItem>
                        <SelectItem value='force_off'>
                          {t('Force off')}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>

                  <Field>
                    <FieldLabel>{t('Compact model mapping')}</FieldLabel>
                    <FieldDescription>
                      {t('Only applies to /responses/compact requests.')}
                    </FieldDescription>
                    {draft.compactModelMappings.length > 0 && (
                      <div className='space-y-2'>
                        {draft.compactModelMappings.map((mapping, index) => (
                          <div
                            key={`${index}-${mapping.from}`}
                            className='grid grid-cols-[1fr_auto_1fr_auto] items-center gap-2'
                          >
                            <Input
                              value={mapping.from}
                              placeholder={t('Source model')}
                              onChange={(event) =>
                                updateCompactMapping(
                                  index,
                                  'from',
                                  event.target.value
                                )
                              }
                            />
                            <span className='text-muted-foreground'>→</span>
                            <Input
                              value={mapping.to}
                              placeholder={t('Target model')}
                              onChange={(event) =>
                                updateCompactMapping(
                                  index,
                                  'to',
                                  event.target.value
                                )
                              }
                            />
                            <Button
                              type='button'
                              variant='ghost'
                              size='icon'
                              title={t('Delete mapping')}
                              onClick={() =>
                                set(
                                  'compactModelMappings',
                                  draft.compactModelMappings.filter(
                                    (_, mappingIndex) => mappingIndex !== index
                                  )
                                )
                              }
                            >
                              <HugeiconsIcon
                                icon={Delete02Icon}
                                strokeWidth={2}
                              />
                            </Button>
                          </div>
                        ))}
                      </div>
                    )}
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      className='w-fit'
                      onClick={() =>
                        set('compactModelMappings', [
                          ...draft.compactModelMappings,
                          { from: '', to: '' },
                        ])
                      }
                    >
                      <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
                      {t('Add mapping')}
                    </Button>
                  </Field>

                  {draft.type === 'apikey' && (
                    <>
                      <Field
                        orientation='horizontal'
                        className='items-center justify-between gap-4'
                      >
                        <div className='flex flex-col gap-1'>
                          <FieldLabel>{t('Responses API mode')}</FieldLabel>
                          <FieldDescription>
                            {t(
                              'Choose the preferred text generation endpoint.'
                            )}
                          </FieldDescription>
                        </div>
                        <Select
                          disabled={
                            !draft.openaiEndpointCapabilities.includes(
                              'chat_completions'
                            )
                          }
                          items={[
                            { value: 'auto', label: t('Automatic') },
                            {
                              value: 'force_responses',
                              label: t('Force Responses API'),
                            },
                            {
                              value: 'force_chat_completions',
                              label: t('Force Chat Completions'),
                            },
                          ]}
                          value={draft.openaiResponsesMode}
                          onValueChange={(value) =>
                            set(
                              'openaiResponsesMode',
                              value as OpenAIResponsesMode
                            )
                          }
                        >
                          <SelectTrigger className='w-56'>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value='auto'>
                              {t('Automatic')}
                            </SelectItem>
                            <SelectItem value='force_responses'>
                              {t('Force Responses API')}
                            </SelectItem>
                            <SelectItem value='force_chat_completions'>
                              {t('Force Chat Completions')}
                            </SelectItem>
                          </SelectContent>
                        </Select>
                      </Field>

                      <Field>
                        <FieldLabel>{t('Endpoint capabilities')}</FieldLabel>
                        <div className='grid gap-2 sm:grid-cols-2'>
                          {defaultOpenAIEndpointCapabilities.map(
                            (capability) => (
                              <label
                                key={capability}
                                className='flex cursor-pointer items-center gap-2 border px-3 py-2 text-sm'
                              >
                                <Checkbox
                                  checked={draft.openaiEndpointCapabilities.includes(
                                    capability
                                  )}
                                  onCheckedChange={() =>
                                    toggleOpenAIEndpointCapability(capability)
                                  }
                                />
                                <span>
                                  {capability === 'chat_completions'
                                    ? t('Text generation')
                                    : t('Embeddings')}
                                </span>
                              </label>
                            )
                          )}
                        </div>
                      </Field>
                    </>
                  )}
                </div>
              )}

              {draft.platform === 'anthropic' && draft.type === 'apikey' && (
                <div className='space-y-4 border-t pt-4'>
                  <Field
                    orientation='horizontal'
                    className='items-center justify-between gap-4'
                  >
                    <div className='flex flex-col gap-1'>
                      <FieldLabel htmlFor='anthropic-passthrough'>
                        {t('Anthropic request passthrough')}
                      </FieldLabel>
                      <FieldDescription>
                        {t(
                          'Forward the original request body and only replace authentication.'
                        )}
                      </FieldDescription>
                    </div>
                    <Switch
                      id='anthropic-passthrough'
                      checked={draft.anthropicPassthrough}
                      onCheckedChange={(checked) =>
                        set('anthropicPassthrough', checked)
                      }
                    />
                  </Field>
                  <Field
                    orientation='horizontal'
                    className='items-center justify-between gap-4'
                  >
                    <div className='flex flex-col gap-1'>
                      <FieldLabel>
                        {t('API key authentication scheme')}
                      </FieldLabel>
                      <FieldDescription>
                        {t(
                          'Select the authentication header required upstream.'
                        )}
                      </FieldDescription>
                    </div>
                    <Select
                      items={[
                        { value: 'x_api_key', label: 'x-api-key' },
                        {
                          value: 'authorization_bearer',
                          label: 'Authorization: Bearer',
                        },
                      ]}
                      value={draft.anthropicAPIKeyAuthScheme}
                      onValueChange={(value) =>
                        set(
                          'anthropicAPIKeyAuthScheme',
                          value as AnthropicAPIKeyAuthScheme
                        )
                      }
                    >
                      <SelectTrigger className='w-56'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value='x_api_key'>x-api-key</SelectItem>
                        <SelectItem value='authorization_bearer'>
                          Authorization: Bearer
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                </div>
              )}

              <Field
                orientation='horizontal'
                className='items-center rounded-lg border p-3'
              >
                <Checkbox
                  id='account-schedulable'
                  checked={draft.schedulable}
                  onCheckedChange={(checked) =>
                    set('schedulable', checked === true)
                  }
                />
                <div className='flex flex-col gap-1'>
                  <FieldLabel htmlFor='account-schedulable'>
                    {t('Allow scheduling')}
                  </FieldLabel>
                  <FieldDescription>
                    {t(
                      'Allow this account to receive requests from local channels.'
                    )}
                  </FieldDescription>
                </div>
              </Field>

              <Field>
                <FieldLabel>{t('Account pools')}</FieldLabel>
                <div className='grid gap-2 rounded-lg border p-3 sm:grid-cols-2'>
                  {compatiblePools.length === 0 ? (
                    <p className='text-muted-foreground text-sm sm:col-span-2'>
                      {t('No compatible account pools are available')}
                    </p>
                  ) : (
                    compatiblePools.map((pool) => (
                      <label
                        key={pool.id}
                        className='hover:bg-muted/60 flex cursor-pointer items-center gap-2 rounded-md p-2 text-sm'
                      >
                        <Checkbox
                          checked={draft.poolIds.includes(pool.id)}
                          onCheckedChange={(checked) =>
                            set(
                              'poolIds',
                              checked === true
                                ? [...draft.poolIds, pool.id]
                                : draft.poolIds.filter((id) => id !== pool.id)
                            )
                          }
                        />
                        <span className='min-w-0 flex-1 truncate'>
                          {pool.name}
                        </span>
                        <Badge variant='outline'>{pool.credential_type}</Badge>
                      </label>
                    ))
                  )}
                </div>
              </Field>
            </FieldGroup>
          )}
        </div>

        <DialogFooter className='bg-background mx-0 mb-0 shrink-0 flex-row justify-end rounded-b-lg px-6 py-4'>
          {step === 1 ? (
            <>
              <Button variant='outline' onClick={() => onOpenChange(false)}>
                {t('Cancel')}
              </Button>
              <Button onClick={nextStep}>
                {t('Next')}
                <HugeiconsIcon
                  data-icon='inline-end'
                  icon={ArrowRight01Icon}
                  strokeWidth={2}
                />
              </Button>
            </>
          ) : (
            <>
              {!account && (
                <Button variant='outline' onClick={() => setStep(1)}>
                  <HugeiconsIcon
                    data-icon='inline-start'
                    icon={ArrowLeft01Icon}
                    strokeWidth={2}
                  />
                  {t('Previous')}
                </Button>
              )}
              <Button onClick={submit} disabled={busy}>
                {busy && (
                  <HugeiconsIcon
                    icon={Loading03Icon}
                    className='animate-spin'
                    strokeWidth={2}
                  />
                )}
                {t('Save')}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
