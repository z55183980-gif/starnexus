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
  ArrowDown01Icon,
  ArrowLeft01Icon,
  ArrowRight01Icon,
  ArrowUp01Icon,
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
import { CopyButton } from '@/components/copy-button'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  buildHeaderOverridesObject,
  buildValidatedModelMapping,
  credentialBackedSettingsChanged,
  editableAccountStatuses,
  parseIntegerAtLeast,
  splitHeaderOverridesObject,
  validateHeaderOverrideRows,
  type CredentialBackedSettings,
  type HeaderOverrideRow,
  type ModelMappingValidationError,
} from './account-dialog-validation'
import {
  AccountModelRestriction,
  splitAccountModelMapping,
  type AccountModelMapping,
  type AccountModelRestrictionMode,
} from './account-model-restriction'
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
type TempUnschedRuleDraft = {
  error_code: string
  keywords: string
  duration_minutes: string
  description: string
}

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
  temp_unschedulable_enabled?: boolean
  temp_unschedulable_rules?: Array<{
    error_code?: number
    keywords?: string[] | string
    duration_minutes?: number
    description?: string
  }>
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
  headerOverrideEnabled: boolean
  headerOverrideRows: HeaderOverrideRow[]
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
  modelRestrictionMode: AccountModelRestrictionMode
  allowedModels: string[]
  modelMappings: AccountModelMapping[]
  tempUnschedEnabled: boolean
  tempUnschedRules: TempUnschedRuleDraft[]
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

function compactMappingsFromMap(
  mapping?: Record<string, string>
): ModelMapping[] {
  if (!mapping) return []
  return Object.entries(mapping).map(([from, to]) => ({
    from,
    to,
  }))
}

function formatTempUnschedKeywords(value: unknown): string {
  if (Array.isArray(value)) {
    return value
      .filter((item): item is string => typeof item === 'string')
      .map((item) => item.trim())
      .filter(Boolean)
      .join(', ')
  }
  if (typeof value === 'string') return value
  return ''
}

function loadTempUnschedRules(
  rules?: Array<{
    error_code?: number
    keywords?: string[] | string
    duration_minutes?: number
    description?: string
  }> | null
): TempUnschedRuleDraft[] {
  if (!Array.isArray(rules)) return []
  return rules.map((rule) => ({
    error_code:
      typeof rule.error_code === 'number' && Number.isFinite(rule.error_code)
        ? String(rule.error_code)
        : '',
    keywords: formatTempUnschedKeywords(rule.keywords),
    duration_minutes:
      typeof rule.duration_minutes === 'number' &&
      Number.isFinite(rule.duration_minutes)
        ? String(rule.duration_minutes)
        : '',
    description:
      typeof rule.description === 'string' ? rule.description : '',
  }))
}

function buildTempUnschedRules(rules: TempUnschedRuleDraft[]) {
  const out: Array<{
    error_code: number
    keywords: string[]
    duration_minutes: number
    description: string
  }> = []
  for (const rule of rules) {
    const errorCode = Number(rule.error_code)
    const duration = Number(rule.duration_minutes)
    const keywords = rule.keywords
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean)
    if (!Number.isFinite(errorCode) || errorCode < 100 || errorCode > 599) {
      continue
    }
    if (!Number.isFinite(duration) || duration <= 0) continue
    if (keywords.length === 0) continue
    out.push({
      error_code: Math.trunc(errorCode),
      keywords,
      duration_minutes: Math.trunc(duration),
      description: rule.description.trim(),
    })
  }
  return out
}

function emptyTempUnschedRule(): TempUnschedRuleDraft {
  return {
    error_code: '',
    keywords: '',
    duration_minutes: '',
    description: '',
  }
}

function accountDraft(account?: UpstreamAccount | null): AccountDraft {
  const extra = parseAccountExtra(account?.extra)
  const metadata = account?.metadata
  const modelRestriction = splitAccountModelMapping(metadata?.model_mapping)
  const websocketMode =
    account?.type === 'apikey'
      ? extra.openai_apikey_responses_websockets_v2_mode
      : extra.openai_oauth_responses_websockets_v2_mode
  // metadata.temp_unschedulable_enabled is a non-omitempty bool (always present).
  // OR with extra so Extra-only config still shows when credentials option
  // parsing failed and metadata stayed at the false zero-value.
  const tempUnschedEnabled =
    metadata?.temp_unschedulable_enabled === true ||
    extra.temp_unschedulable_enabled === true
  const tempUnschedRules = loadTempUnschedRules(
    metadata?.temp_unschedulable_rules?.length
      ? metadata.temp_unschedulable_rules
      : extra.temp_unschedulable_rules
  )
  return {
    name: account?.name ?? '',
    notes: account?.notes ?? '',
    platform: account?.platform ?? 'openai',
    type: account?.type ?? 'oauth',
    apiKey: '',
    baseUrl: metadata?.base_url ?? '',
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
    bedrockAuthMode:
      metadata?.bedrock_auth_mode === 'apikey' ||
      metadata?.bedrock_auth_mode === 'api_key'
        ? 'api_key'
        : 'sigv4',
    bedrockRegion: metadata?.aws_region || 'us-east-1',
    bedrockAccessKeyId: metadata?.aws_access_key_id || '',
    bedrockSecretAccessKey: '',
    bedrockSessionToken: '',
    bedrockApiKey: '',
    vertexServiceAccountJson: '',
    vertexLocation: metadata?.vertex_location || 'global',
    interceptWarmupRequests:
      metadata?.intercept_warmup_requests ??
      extra.intercept_warmup_requests === true,
    headerOverrideEnabled: metadata?.header_override_enabled === true,
    headerOverrideRows: splitHeaderOverridesObject(metadata?.header_overrides),
    openaiPassthrough: extra.openai_passthrough === true,
    openaiWebSocketMode: websocketMode || 'off',
    openaiLongContextBilling:
      extra.openai_long_context_billing_enabled === true,
    codexCLIOnly: extra.codex_cli_only === true,
    codexCLIOnlyAllowAppServer: extra.codex_cli_only_allow_app_server === true,
    openaiCompactMode: extra.openai_compact_mode || 'auto',
    compactModelMappings: compactMappingsFromMap(
      metadata?.compact_model_mapping || extra.compact_model_mapping
    ),
    openaiResponsesMode: extra.openai_responses_mode || 'auto',
    openaiEndpointCapabilities: metadata?.openai_capabilities?.length
      ? (metadata.openai_capabilities as OpenAIEndpointCapability[])
      : extra.openai_capabilities?.length
        ? extra.openai_capabilities
        : [...defaultOpenAIEndpointCapabilities],
    anthropicPassthrough: extra.anthropic_passthrough === true,
    anthropicAPIKeyAuthScheme:
      extra.anthropic_apikey_auth_scheme || 'x_api_key',
    modelRestrictionMode:
      modelRestriction.modelMappings.length > 0 &&
      modelRestriction.allowedModels.length === 0
        ? 'mapping'
        : 'whitelist',
    allowedModels: modelRestriction.allowedModels,
    modelMappings: modelRestriction.modelMappings,
    tempUnschedEnabled,
    tempUnschedRules,
  }
}

function isOAuthAccountType(type: UpstreamAccountType) {
  return type === 'oauth' || type === 'setup_token'
}

function isHeaderOverrideCapable(
  platform: UpstreamPlatform,
  type: UpstreamAccountType
) {
  return (
    (platform === 'openai' || platform === 'anthropic') && type === 'apikey'
  )
}

function credentialBackedSettings(
  draft: AccountDraft
): CredentialBackedSettings {
  return {
    baseUrl: draft.baseUrl,
    bedrockAuthMode: draft.bedrockAuthMode,
    bedrockRegion: draft.bedrockRegion,
    vertexLocation: draft.vertexLocation,
    interceptWarmupRequests: draft.interceptWarmupRequests,
    tempUnschedEnabled: draft.tempUnschedEnabled,
    tempUnschedRules: draft.tempUnschedRules,
    compactModelMappings: draft.compactModelMappings,
    openaiEndpointCapabilities: draft.openaiEndpointCapabilities,
    allowedModels: draft.allowedModels,
    modelMappings: draft.modelMappings,
    headerOverrideEnabled: draft.headerOverrideEnabled,
    headerOverrideRows: draft.headerOverrideRows,
  }
}

function hasReplacementCredentials(draft: AccountDraft) {
  if (isOAuthAccountType(draft.type)) return Boolean(draft.oauthInput.trim())
  if (draft.type === 'apikey') return Boolean(draft.apiKey.trim())
  if (draft.type === 'bedrock') {
    return draft.bedrockAuthMode === 'api_key'
      ? Boolean(draft.bedrockApiKey.trim())
      : Boolean(
          draft.bedrockAccessKeyId.trim() || draft.bedrockSecretAccessKey.trim()
        )
  }
  if (draft.type === 'service_account') {
    return Boolean(draft.vertexServiceAccountJson.trim())
  }
  return false
}

export function AccountDialog({
  open,
  onOpenChange,
  account,
  pools,
  proxies,
  onSaved,
  initialAuthorizeUrl = '',
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  account: UpstreamAccount | null
  pools: UpstreamAccountPool[]
  proxies: Array<{ id: number; name: string }>
  onSaved: () => void
  initialAuthorizeUrl?: string
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<AccountDraft>(() => accountDraft(account))
  const [step, setStep] = useState(account ? 2 : 1)
  const [busy, setBusy] = useState(false)
  const [authorizeUrl, setAuthorizeUrl] = useState('')
  const contentRef = useRef<HTMLDivElement>(null)
  const initialCredentialSettingsRef = useRef<CredentialBackedSettings>(
    credentialBackedSettings(accountDraft(account))
  )

  useEffect(() => {
    if (!open) return
    const nextDraft = accountDraft(account)
    setDraft(nextDraft)
    initialCredentialSettingsRef.current = credentialBackedSettings(nextDraft)
    setStep(account ? 2 : 1)
    setAuthorizeUrl(initialAuthorizeUrl)
  }, [account, initialAuthorizeUrl, open])

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

  const credentialSettingsDisabled = Boolean(
    account?.metadata.credential_readable === false &&
    !hasReplacementCredentials(draft)
  )

  const statusItems = editableAccountStatuses(account?.status).map(
    (status) => ({
      value: status,
      label:
        status === 'active'
          ? t('Active')
          : status === 'inactive'
            ? t('Inactive')
            : status === 'error'
              ? t('Error')
              : t('Expired'),
    })
  )

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
      setAuthorizeUrl(response.data.authorize_url)
      toast.success(t('Authorization URL generated'))
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

  const buildCredentialPayload = (): Record<string, unknown> | undefined => {
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
          auth_mode: 'apikey',
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

  const modelMappingErrorMessage = (
    error: ModelMappingValidationError,
    compact: boolean
  ) => {
    if (error === 'missingPair') {
      return t(
        compact
          ? 'Compact model mappings require both models'
          : 'Model mappings require both model names'
      )
    }
    if (error === 'invalidSourceWildcard') {
      return t('A wildcard can only appear once at the end')
    }
    if (error === 'wildcardTarget') {
      return t('Target models cannot contain wildcards')
    }
    return t('Compact model mapping sources must be unique')
  }

  const buildModelRestrictionMapping = () => {
    const mapping: Record<string, string> = {}
    for (const rawModel of draft.allowedModels) {
      const model = rawModel.trim()
      if (model && !model.includes('*')) mapping[model] = model
    }
    const result = buildValidatedModelMapping(draft.modelMappings)
    if (result.error) {
      throw new Error(modelMappingErrorMessage(result.error, false))
    }
    for (const [from, to] of Object.entries(result.mapping || {})) {
      mapping[from] = to
    }
    return Object.keys(mapping).length > 0 ? mapping : null
  }

  const buildCompactModelMapping = () => {
    const result = buildValidatedModelMapping(draft.compactModelMappings, true)
    if (result.error) {
      throw new Error(modelMappingErrorMessage(result.error, true))
    }
    return result.mapping
  }

  const applyCredentialBackedSettings = (
    credentials: Record<string, unknown>
  ) => {
    if (!(draft.platform === 'openai' && draft.openaiPassthrough)) {
      const modelMapping = buildModelRestrictionMapping()
      if (modelMapping) credentials.model_mapping = modelMapping
      else delete credentials.model_mapping
    }
    if (draft.interceptWarmupRequests) {
      credentials.intercept_warmup_requests = true
    } else {
      delete credentials.intercept_warmup_requests
    }
    if (draft.tempUnschedEnabled) {
      credentials.temp_unschedulable_enabled = true
      credentials.temp_unschedulable_rules = buildTempUnschedRules(
        draft.tempUnschedRules
      )
    } else {
      delete credentials.temp_unschedulable_enabled
      delete credentials.temp_unschedulable_rules
    }
    if (isHeaderOverrideCapable(draft.platform, draft.type)) {
      if (draft.headerOverrideEnabled) {
        credentials.header_override_enabled = true
        credentials.header_overrides = buildHeaderOverridesObject(
          draft.headerOverrideRows
        )
      } else {
        delete credentials.header_override_enabled
        delete credentials.header_overrides
      }
    }
    if (draft.platform === 'openai') {
      const compactModelMapping = buildCompactModelMapping()
      if (compactModelMapping) {
        credentials.compact_model_mapping = compactModelMapping
      } else {
        delete credentials.compact_model_mapping
      }
      if (
        draft.type === 'apikey' &&
        draft.openaiEndpointCapabilities.length !==
          defaultOpenAIEndpointCapabilities.length
      ) {
        credentials.openai_capabilities =
          draft.openaiEndpointCapabilities.filter((value) =>
            defaultOpenAIEndpointCapabilities.includes(value)
          )
      } else {
        delete credentials.openai_capabilities
      }
    }
  }

  const buildCredentialPatch = (allowUnreadable = false) => {
    const patch: Record<string, unknown> = {}
    if (
      account &&
      account.metadata.credential_readable === false &&
      !allowUnreadable
    ) {
      return patch
    }
    if (!(draft.platform === 'openai' && draft.openaiPassthrough)) {
      patch.model_mapping = buildModelRestrictionMapping()
    }
    patch.intercept_warmup_requests = draft.interceptWarmupRequests
      ? true
      : null
    if (draft.tempUnschedEnabled) {
      patch.temp_unschedulable_enabled = true
      patch.temp_unschedulable_rules = buildTempUnschedRules(
        draft.tempUnschedRules
      )
    } else {
      patch.temp_unschedulable_enabled = null
      patch.temp_unschedulable_rules = null
    }
    if (isHeaderOverrideCapable(draft.platform, draft.type)) {
      patch.header_override_enabled = draft.headerOverrideEnabled ? true : null
      patch.header_overrides = draft.headerOverrideEnabled
        ? buildHeaderOverridesObject(draft.headerOverrideRows)
        : null
    }
    if (draft.platform === 'openai') {
      patch.compact_model_mapping = buildCompactModelMapping()
      if (draft.type === 'apikey') {
        const capabilities = draft.openaiEndpointCapabilities.filter((value) =>
          defaultOpenAIEndpointCapabilities.includes(value)
        )
        patch.openai_capabilities =
          capabilities.length === defaultOpenAIEndpointCapabilities.length
            ? null
            : capabilities
      }
    }
    if (draft.type === 'apikey') {
      patch.base_url = draft.baseUrl.trim() || null
      if (draft.apiKey.trim()) patch.api_key = draft.apiKey.trim()
    } else if (draft.type === 'bedrock') {
      const region = draft.bedrockRegion.trim()
      if (!region) throw new Error(t('AWS region is required'))
      patch.auth_mode = draft.bedrockAuthMode === 'api_key' ? 'apikey' : 'sigv4'
      patch.aws_region = region
      if (draft.bedrockAuthMode === 'api_key') {
        if (draft.bedrockApiKey.trim()) {
          patch.api_key = draft.bedrockApiKey.trim()
        }
      } else {
        if (draft.bedrockAccessKeyId.trim()) {
          patch.aws_access_key_id = draft.bedrockAccessKeyId.trim()
        }
        if (draft.bedrockSecretAccessKey.trim()) {
          patch.aws_secret_access_key = draft.bedrockSecretAccessKey.trim()
        }
        if (draft.bedrockSessionToken.trim()) {
          patch.aws_session_token = draft.bedrockSessionToken.trim()
        }
      }
    } else if (draft.type === 'service_account') {
      if (!draft.vertexLocation.trim()) {
        throw new Error(t('Vertex location is required'))
      }
      patch.location = draft.vertexLocation.trim()
      const raw = draft.vertexServiceAccountJson.trim()
      if (raw) {
        let parsed: { project_id?: string; client_email?: string }
        try {
          parsed = JSON.parse(raw) as typeof parsed
        } catch {
          throw new Error(t('Service account JSON is invalid'))
        }
        patch.service_account_json = raw
        patch.project_id = parsed.project_id || ''
        patch.client_email = parsed.client_email || ''
        patch.tier_id = 'vertex'
      }
    }
    return patch
  }

  const buildAccountExtra = () => {
    const extra = parseAccountExtra(account?.extra)
    const managedKeys = [
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
      'openai_responses_mode',
      'anthropic_passthrough',
      'anthropic_apikey_auth_scheme',
      'temp_unschedulable_enabled',
      'temp_unschedulable_rules',
    ]
    managedKeys.forEach((key) => delete extra[key])
    if (!account || account.metadata.credential_readable !== false) {
      for (const key of [
        'intercept_warmup_requests',
        'compact_model_mapping',
        'openai_capabilities',
        'temp_unschedulable_enabled',
        'temp_unschedulable_rules',
      ]) {
        delete extra[key]
      }
    }

    if (draft.tempUnschedEnabled) {
      const rules = buildTempUnschedRules(draft.tempUnschedRules)
      extra.temp_unschedulable_enabled = true
      extra.temp_unschedulable_rules = rules
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

  const updateHeaderOverride = (
    index: number,
    key: keyof HeaderOverrideRow,
    value: string
  ) => {
    set(
      'headerOverrideRows',
      draft.headerOverrideRows.map((row, rowIndex) =>
        rowIndex === index ? { ...row, [key]: value } : row
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
      const concurrency = parseIntegerAtLeast(draft.concurrency, 1)
      const priority = parseIntegerAtLeast(draft.priority, 0)
      const weight = parseIntegerAtLeast(draft.weight, 1)
      if (concurrency === null) {
        throw new Error(t('Concurrency must be a whole number of at least 1'))
      }
      if (priority === null) {
        throw new Error(t('Priority must be a non-negative whole number'))
      }
      if (weight === null) {
        throw new Error(t('Weight must be a whole number of at least 1'))
      }
      if (draft.headerOverrideEnabled) {
        const headerError = validateHeaderOverrideRows(draft.headerOverrideRows)
        if (headerError === 'invalidName') {
          throw new Error(t('Invalid HTTP header override name'))
        }
        if (headerError === 'blockedName') {
          throw new Error(t('This HTTP header cannot be overridden'))
        }
        if (headerError === 'duplicateName') {
          throw new Error(t('HTTP header override names must be unique'))
        }
        if (headerError === 'invalidValue') {
          throw new Error(t('Invalid HTTP header override value'))
        }
        if (headerError === 'tooManyEntries') {
          throw new Error(t('HTTP header overrides support at most 64 entries'))
        }
      }
      if (
        account?.metadata.credential_readable === false &&
        !hasReplacementCredentials(draft) &&
        credentialBackedSettingsChanged(
          initialCredentialSettingsRef.current,
          credentialBackedSettings(draft)
        )
      ) {
        throw new Error(
          t(
            'Replace the stored credentials before changing credential-backed settings.'
          )
        )
      }
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
      if (draft.tempUnschedEnabled) {
        const rules = buildTempUnschedRules(draft.tempUnschedRules)
        if (rules.length === 0) {
          throw new Error(
            t(
              'Add at least one rule with an error code, keywords, and duration.'
            )
          )
        }
      }
      const payload: UpstreamAccountPayload = {
        name: draft.name.trim(),
        notes: draft.notes.trim(),
        platform: draft.platform,
        type: draft.type,
        extra: buildAccountExtra(),
        proxy_id: draft.proxyId ? Number(draft.proxyId) : null,
        concurrency,
        priority,
        weight,
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
      if (account || oauthType) {
        const credentialPatch = buildCredentialPatch(
          oauthType && Boolean(draft.oauthInput.trim())
        )
        if (Object.keys(credentialPatch).length > 0) {
          payload.credential_patch = credentialPatch
        }
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
        if (!account) {
          payload.credentials = buildCredentialPayload()
          if (!payload.credentials) {
            throw new Error(t('Credentials are required'))
          }
          applyCredentialBackedSettings(payload.credentials)
        } else if (account.metadata.credential_readable === false) {
          const replacementCredentials = buildCredentialPayload()
          if (replacementCredentials) {
            applyCredentialBackedSettings(replacementCredentials)
            payload.credentials = replacementCredentials
            delete payload.credential_patch
          }
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
      <DialogContent className='flex max-h-[92vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl'>
        <DialogHeader className='shrink-0 border-b px-6 py-5 pr-12'>
          <DialogTitle>
            {account ? t('Edit account') : t('Add account')}
          </DialogTitle>
          <DialogDescription className={account ? 'sr-only' : undefined}>
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
              {account ? (
                <>
                  <Field>
                    <FieldLabel htmlFor='edit-account-name'>
                      {t('Name')}
                    </FieldLabel>
                    <Input
                      id='edit-account-name'
                      value={draft.name}
                      onChange={(event) => set('name', event.target.value)}
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor='edit-account-notes'>
                      {t('Notes')}
                    </FieldLabel>
                    <Textarea
                      id='edit-account-notes'
                      rows={3}
                      value={draft.notes}
                      placeholder={t('Optional notes')}
                      onChange={(event) => set('notes', event.target.value)}
                    />
                    <FieldDescription>
                      {t('Notes are optional')}
                    </FieldDescription>
                  </Field>
                  {account.metadata.credential_readable === false && (
                    <Alert variant='destructive'>
                      <AlertDescription>
                        {t(
                          'Stored credentials could not be read. Replace the credentials to restore credential-backed settings.'
                        )}
                      </AlertDescription>
                    </Alert>
                  )}
                  <Separator />
                  <Field>
                    <FieldLabel>{t('Model restriction (optional)')}</FieldLabel>
                    {draft.platform === 'openai' && draft.openaiPassthrough && (
                      <Alert>
                        <AlertDescription>
                          {t(
                            'Model whitelist and mapping are disabled while passthrough is enabled.'
                          )}
                        </AlertDescription>
                      </Alert>
                    )}
                    <AccountModelRestriction
                      platform={draft.platform}
                      mode={draft.modelRestrictionMode}
                      allowedModels={draft.allowedModels}
                      modelMappings={draft.modelMappings}
                      disabled={
                        credentialSettingsDisabled ||
                        (draft.platform === 'openai' && draft.openaiPassthrough)
                      }
                      onModeChange={(value) =>
                        set('modelRestrictionMode', value)
                      }
                      onAllowedModelsChange={(value) =>
                        set('allowedModels', value)
                      }
                      onModelMappingsChange={(value) =>
                        set('modelMappings', value)
                      }
                    />
                  </Field>
                  <Separator />
                </>
              ) : (
                <div className='bg-muted/40 flex flex-wrap items-center gap-2 rounded-lg border p-3'>
                  <Badge variant='outline'>{draft.platform}</Badge>
                  <Badge variant='secondary'>
                    {draft.type === 'setup_token'
                      ? t('Setup Token')
                      : draft.type}
                  </Badge>
                  <span className='min-w-0 flex-1 truncate text-sm font-medium'>
                    {draft.name}
                  </span>
                </div>
              )}

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
                            'Generate the authorization URL, copy and open it in your browser, then paste the callback URL or code below.'
                          )}
                        </FieldDescription>
                      </div>
                      {!authorizeUrl ? (
                        <Button
                          type='button'
                          variant='outline'
                          onClick={startOAuth}
                          disabled={busy || !draft.starnexusOwnsOAuthRefresh}
                        >
                          <HugeiconsIcon icon={Link01Icon} strokeWidth={2} />
                          {t('Generate authorization URL')}
                        </Button>
                      ) : null}
                    </div>
                    {authorizeUrl ? (
                      <div className='space-y-2'>
                        <div className='flex items-center gap-2'>
                          <Input
                            readOnly
                            value={authorizeUrl}
                            className='font-mono text-xs'
                            aria-label={t('Authorization URL')}
                          />
                          <CopyButton
                            value={authorizeUrl}
                            variant='outline'
                            tooltip={t('Copy authorization link')}
                            aria-label={t('Copy authorization link')}
                          />
                        </div>
                        <Button
                          type='button'
                          variant='link'
                          className='h-auto px-0'
                          onClick={startOAuth}
                          disabled={busy || !draft.starnexusOwnsOAuthRefresh}
                        >
                          {t('Regenerate authorization URL')}
                        </Button>
                      </div>
                    ) : null}
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

              {isHeaderOverrideCapable(draft.platform, draft.type) && (
                <>
                  <Field
                    data-disabled={credentialSettingsDisabled || undefined}
                    orientation='horizontal'
                    className='items-center justify-between rounded-lg border p-3'
                  >
                    <div className='flex flex-col gap-1'>
                      <FieldLabel htmlFor='account-header-override-enabled'>
                        {t('Header override')}
                      </FieldLabel>
                      <FieldDescription>
                        {t(
                          'Override same-named outbound request headers for this account.'
                        )}
                      </FieldDescription>
                    </div>
                    <Switch
                      id='account-header-override-enabled'
                      disabled={credentialSettingsDisabled}
                      checked={draft.headerOverrideEnabled}
                      onCheckedChange={(checked) =>
                        set('headerOverrideEnabled', checked)
                      }
                    />
                  </Field>
                  {draft.headerOverrideEnabled && (
                    <Field
                      data-disabled={credentialSettingsDisabled || undefined}
                    >
                      <FieldDescription>
                        {t(
                          'Authentication and connection-control headers cannot be overridden.'
                        )}
                      </FieldDescription>
                      <FieldGroup className='gap-2'>
                        {draft.headerOverrideRows.map((row, index) => (
                          <FieldGroup
                            key={index}
                            className='grid grid-cols-[1fr_1fr_auto] items-end gap-2'
                          >
                            <Field>
                              <FieldLabel
                                htmlFor={`account-header-name-${index}`}
                                className='sr-only'
                              >
                                {t('Header name')}
                              </FieldLabel>
                              <Input
                                id={`account-header-name-${index}`}
                                disabled={credentialSettingsDisabled}
                                value={row.name}
                                placeholder={t('Header name')}
                                onChange={(event) =>
                                  updateHeaderOverride(
                                    index,
                                    'name',
                                    event.target.value
                                  )
                                }
                              />
                            </Field>
                            <Field>
                              <FieldLabel
                                htmlFor={`account-header-value-${index}`}
                                className='sr-only'
                              >
                                {t('Header value')}
                              </FieldLabel>
                              <Input
                                id={`account-header-value-${index}`}
                                disabled={credentialSettingsDisabled}
                                value={row.value}
                                placeholder={t('Header value')}
                                onChange={(event) =>
                                  updateHeaderOverride(
                                    index,
                                    'value',
                                    event.target.value
                                  )
                                }
                              />
                            </Field>
                            <Button
                              type='button'
                              variant='ghost'
                              size='icon'
                              disabled={credentialSettingsDisabled}
                              title={t('Delete header')}
                              onClick={() =>
                                set(
                                  'headerOverrideRows',
                                  draft.headerOverrideRows.filter(
                                    (_, rowIndex) => rowIndex !== index
                                  )
                                )
                              }
                            >
                              <HugeiconsIcon
                                data-icon='inline-start'
                                icon={Delete02Icon}
                                strokeWidth={2}
                              />
                            </Button>
                          </FieldGroup>
                        ))}
                      </FieldGroup>
                      <Button
                        type='button'
                        variant='outline'
                        size='sm'
                        className='w-fit'
                        disabled={credentialSettingsDisabled}
                        onClick={() =>
                          set('headerOverrideRows', [
                            ...draft.headerOverrideRows,
                            { name: '', value: '' },
                          ])
                        }
                      >
                        <HugeiconsIcon
                          data-icon='inline-start'
                          icon={Add01Icon}
                          strokeWidth={2}
                        />
                        {t('Add header')}
                      </Button>
                    </Field>
                  )}
                </>
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

              <Field
                data-disabled={credentialSettingsDisabled || undefined}
                orientation='horizontal'
                className='items-center justify-between rounded-lg border p-3'
              >
                <div className='flex flex-col gap-1'>
                  <FieldLabel htmlFor='account-intercept-warmup'>
                    {t('Intercept warmup requests')}
                  </FieldLabel>
                  <FieldDescription>
                    {t(
                      'Return mock responses for warmup requests without consuming upstream tokens.'
                    )}
                  </FieldDescription>
                </div>
                <Switch
                  id='account-intercept-warmup'
                  disabled={credentialSettingsDisabled}
                  checked={draft.interceptWarmupRequests}
                  onCheckedChange={(checked) =>
                    set('interceptWarmupRequests', checked)
                  }
                />
              </Field>

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
                    items={statusItems}
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
                        {statusItems.map((item) => (
                          <SelectItem key={item.value} value={item.value}>
                            {item.label}
                          </SelectItem>
                        ))}
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

                  <Field
                    data-disabled={credentialSettingsDisabled || undefined}
                  >
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
                              disabled={credentialSettingsDisabled}
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
                              disabled={credentialSettingsDisabled}
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
                              disabled={credentialSettingsDisabled}
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
                      disabled={credentialSettingsDisabled}
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

                      <Field
                        data-disabled={credentialSettingsDisabled || undefined}
                      >
                        <FieldLabel>{t('Endpoint capabilities')}</FieldLabel>
                        <div className='grid gap-2 sm:grid-cols-2'>
                          {defaultOpenAIEndpointCapabilities.map(
                            (capability) => (
                              <label
                                key={capability}
                                className='flex cursor-pointer items-center gap-2 border px-3 py-2 text-sm'
                              >
                                <Checkbox
                                  disabled={credentialSettingsDisabled}
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
                className='items-center justify-between gap-4 rounded-lg border p-3'
              >
                <div className='flex flex-col gap-1'>
                  <FieldLabel htmlFor='account-temp-unsched'>
                    {t('Temporarily unschedulable')}
                  </FieldLabel>
                  <FieldDescription>
                    {t(
                      'When the error code and keywords match, the account is temporarily disabled for the configured duration. Rules for 401 take precedence over OAuth refresh handling.'
                    )}
                  </FieldDescription>
                </div>
                <Switch
                  id='account-temp-unsched'
                  checked={draft.tempUnschedEnabled}
                  onCheckedChange={(checked) =>
                    set('tempUnschedEnabled', checked === true)
                  }
                />
              </Field>

              {draft.tempUnschedEnabled && (
                <div className='space-y-3 rounded-lg border p-3'>
                  <Alert>
                    <AlertDescription>
                      {t(
                        'Rules are matched in order. Both the error code and at least one keyword must match.'
                      )}
                    </AlertDescription>
                  </Alert>
                  <div className='flex flex-wrap gap-2'>
                    {(
                      [
                        {
                          label: t('529 overload'),
                          rule: {
                            error_code: '529',
                            keywords: 'overloaded, too many',
                            duration_minutes: '60',
                            description: t(
                              'Service overload - pause 60 minutes'
                            ),
                          },
                        },
                        {
                          label: t('429 rate limit'),
                          rule: {
                            error_code: '429',
                            keywords: 'rate limit, too many requests',
                            duration_minutes: '10',
                            description: t(
                              'Rate limited - pause 10 minutes'
                            ),
                          },
                        },
                        {
                          label: t('503 unavailable'),
                          rule: {
                            error_code: '503',
                            keywords: 'unavailable, maintenance',
                            duration_minutes: '30',
                            description: t(
                              'Service unavailable - pause 30 minutes'
                            ),
                          },
                        },
                      ] as Array<{
                        label: string
                        rule: TempUnschedRuleDraft
                      }>
                    ).map((preset) => (
                      <Button
                        key={preset.label}
                        type='button'
                        variant='outline'
                        size='sm'
                        onClick={() =>
                          set('tempUnschedRules', [
                            ...draft.tempUnschedRules,
                            { ...preset.rule },
                          ])
                        }
                      >
                        + {preset.label}
                      </Button>
                    ))}
                  </div>
                  {draft.tempUnschedRules.map((rule, index) => (
                    <div
                      key={`temp-unsched-rule-${index}`}
                      className='space-y-3 rounded-lg border p-3'
                    >
                      <div className='flex items-center justify-between gap-2'>
                        <span className='text-muted-foreground text-xs font-medium'>
                          {t('Rule #{{index}}', { index: index + 1 })}
                        </span>
                        <div className='flex items-center gap-1'>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            className='size-7'
                            disabled={index === 0}
                            onClick={() => {
                              const next = [...draft.tempUnschedRules]
                              ;[next[index - 1], next[index]] = [
                                next[index],
                                next[index - 1],
                              ]
                              set('tempUnschedRules', next)
                            }}
                          >
                            <HugeiconsIcon
                              icon={ArrowUp01Icon}
                              strokeWidth={2}
                            />
                          </Button>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            className='size-7'
                            disabled={
                              index === draft.tempUnschedRules.length - 1
                            }
                            onClick={() => {
                              const next = [...draft.tempUnschedRules]
                              ;[next[index + 1], next[index]] = [
                                next[index],
                                next[index + 1],
                              ]
                              set('tempUnschedRules', next)
                            }}
                          >
                            <HugeiconsIcon
                              icon={ArrowDown01Icon}
                              strokeWidth={2}
                            />
                          </Button>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            className='text-destructive size-7'
                            onClick={() =>
                              set(
                                'tempUnschedRules',
                                draft.tempUnschedRules.filter(
                                  (_, ruleIndex) => ruleIndex !== index
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
                      </div>
                      <div className='grid gap-3 sm:grid-cols-2'>
                        <Field>
                          <FieldLabel>{t('Error code')}</FieldLabel>
                          <Input
                            type='number'
                            min={100}
                            max={599}
                            value={rule.error_code}
                            placeholder={t('e.g. 429')}
                            onChange={(event) => {
                              const next = [...draft.tempUnschedRules]
                              next[index] = {
                                ...rule,
                                error_code: event.target.value,
                              }
                              set('tempUnschedRules', next)
                            }}
                          />
                        </Field>
                        <Field>
                          <FieldLabel>
                            {t('Duration (minutes)')}
                          </FieldLabel>
                          <Input
                            type='number'
                            min={1}
                            value={rule.duration_minutes}
                            placeholder={t('e.g. 30')}
                            onChange={(event) => {
                              const next = [...draft.tempUnschedRules]
                              next[index] = {
                                ...rule,
                                duration_minutes: event.target.value,
                              }
                              set('tempUnschedRules', next)
                            }}
                          />
                        </Field>
                        <Field className='sm:col-span-2'>
                          <FieldLabel>{t('Keywords')}</FieldLabel>
                          <Input
                            value={rule.keywords}
                            placeholder={t(
                              'e.g. overloaded, too many requests'
                            )}
                            onChange={(event) => {
                              const next = [...draft.tempUnschedRules]
                              next[index] = {
                                ...rule,
                                keywords: event.target.value,
                              }
                              set('tempUnschedRules', next)
                            }}
                          />
                          <FieldDescription>
                            {t(
                              'Separate multiple keywords with commas. Matching requires at least one hit.'
                            )}
                          </FieldDescription>
                        </Field>
                        <Field className='sm:col-span-2'>
                          <FieldLabel>{t('Description')}</FieldLabel>
                          <Input
                            value={rule.description}
                            placeholder={t(
                              'Optional note to remember what this rule is for'
                            )}
                            onChange={(event) => {
                              const next = [...draft.tempUnschedRules]
                              next[index] = {
                                ...rule,
                                description: event.target.value,
                              }
                              set('tempUnschedRules', next)
                            }}
                          />
                        </Field>
                      </div>
                    </div>
                  ))}
                  <Button
                    type='button'
                    variant='outline'
                    className='w-full border-dashed'
                    onClick={() =>
                      set('tempUnschedRules', [
                        ...draft.tempUnschedRules,
                        emptyTempUnschedRule(),
                      ])
                    }
                  >
                    <HugeiconsIcon icon={Add01Icon} strokeWidth={2} />
                    {t('Add rule')}
                  </Button>
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

        <DialogFooter className='bg-background mx-0 mb-0 shrink-0 flex-row justify-end rounded-b-lg border-t px-6 py-4'>
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
              {account && (
                <Button variant='outline' onClick={() => onOpenChange(false)}>
                  {t('Cancel')}
                </Button>
              )}
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
                {account ? t('Update') : t('Save')}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
