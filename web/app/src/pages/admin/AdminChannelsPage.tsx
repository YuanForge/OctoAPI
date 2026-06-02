import { type FormEvent, useMemo, useRef, useState } from 'react'
import { CopyIcon, PlusIcon, RotateCcwIcon, SaveIcon, SearchIcon } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/shared/PageHeader'
import { TablePagination } from '@/components/shared/TablePagination'
import { TableSkeleton } from '@/components/shared/TableSkeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { adminApi, type AdminChannel, type AdminChannelLog, type AdminKeyPool } from '@/lib/api/admin'
import { useAsync } from '@/hooks/use-async'

type ChannelForm = {
  id?: number
  name: string
  model: string
  type: string
  protocol: string
  base_url: string
  method: string
  query_url: string
  query_method: string
  timeout_ms: string
  query_timeout_ms: string
  billing_type: string
  headers_text: string
  billing_config_text: string
  pricing_groups: PricingGroupForm[]
  // markup multiplier (selling price = cost * markup)
  billing_markup: string
  // token billing
  billing_input_price: string
  billing_output_price: string
  billing_input_cost: string
  billing_output_cost: string
  billing_cache_create_price: string
  billing_cache_create_cost: string
  billing_cache_read_price: string
  billing_cache_read_cost: string
  billing_input_from_response: boolean
  // image billing
  billing_base_price: string
  billing_base_cost: string
  billing_size_price_1k: string
  billing_size_price_2k: string
  billing_size_price_3k: string
  billing_size_price_4k: string
  billing_size_cost_1k: string
  billing_size_cost_2k: string
  billing_size_cost_3k: string
  billing_size_cost_4k: string
  billing_default_size_price: string
  billing_default_size_cost: string
  // video / audio billing
  billing_price_per_second: string
  billing_cost_per_second: string
  // count billing
  billing_price_per_call: string
  billing_cost_per_call: string
  billing_script: string
  request_script: string
  response_script: string
  query_script: string
  error_script: string
  key_pool_id: string
  auth_type: string
  auth_param_name: string
  auth_region: string
  auth_service: string
  passthrough_headers: boolean
  passthrough_body: boolean
  weight: string
  priority: string
  icon_url: string
  description: string
  display_name: string
  groups: string[]
  is_active: boolean
}

type PricingGroupForm = {
  group: string
  input_price_per_1m_tokens: string
  output_price_per_1m_tokens: string
  base_price: string
  default_size_price: string
  size_price_1k: string
  size_price_2k: string
  size_price_3k: string
  size_price_4k: string
  price_per_second: string
  price_per_call: string
  extra_config: Record<string, unknown>
}

const emptyJson = '{}'
const sizeTierKeys = ['1k', '2k', '3k', '4k'] as const
const creditsPrecision = 1_000_000
const structuredBillingConfigKeys = new Set([
  'input_price_per_1m_tokens',
  'output_price_per_1m_tokens',
  'input_cost_per_1m_tokens',
  'output_cost_per_1m_tokens',
  'cache_creation_price_per_1m_tokens',
  'cache_creation_cost_per_1m_tokens',
  'cache_read_price_per_1m_tokens',
  'cache_read_cost_per_1m_tokens',
  'input_from_response',
  'base_price',
  'base_cost',
  'size_prices',
  'size_costs',
  'default_size_price',
  'default_size_cost',
  'price_per_second',
  'cost_per_second',
  'price_per_call',
  'cost_per_call',
  'pricing_groups',
])
const pricingGroupOverrideKeys = new Set([
  'input_price_per_1m_tokens',
  'output_price_per_1m_tokens',
  'base_price',
  'default_size_price',
  'size_prices',
  'price_per_second',
  'price_per_call',
])

function createEmptyPricingGroup(): PricingGroupForm {
  return {
    group: '',
    input_price_per_1m_tokens: '',
    output_price_per_1m_tokens: '',
    base_price: '',
    default_size_price: '',
    size_price_1k: '',
    size_price_2k: '',
    size_price_3k: '',
    size_price_4k: '',
    price_per_second: '',
    price_per_call: '',
    extra_config: {},
  }
}

const emptyForm: ChannelForm = {
  name: '',
  model: '',
  type: 'llm',
  protocol: 'openai',
  base_url: '',
  method: 'POST',
  query_url: '',
  query_method: 'GET',
  timeout_ms: '60000',
  query_timeout_ms: '30000',
  billing_type: 'token',
  headers_text: emptyJson,
  billing_config_text: emptyJson,
  pricing_groups: [],
  billing_markup: '1.2',
  billing_input_price: '',
  billing_output_price: '',
  billing_input_cost: '',
  billing_output_cost: '',
  billing_cache_create_price: '',
  billing_cache_create_cost: '',
  billing_cache_read_price: '',
  billing_cache_read_cost: '',
  billing_input_from_response: false,
  billing_base_price: '',
  billing_base_cost: '',
  billing_size_price_1k: '',
  billing_size_price_2k: '',
  billing_size_price_3k: '',
  billing_size_price_4k: '',
  billing_size_cost_1k: '',
  billing_size_cost_2k: '',
  billing_size_cost_3k: '',
  billing_size_cost_4k: '',
  billing_default_size_price: '',
  billing_default_size_cost: '',
  billing_price_per_second: '',
  billing_cost_per_second: '',
  billing_price_per_call: '',
  billing_cost_per_call: '',
  billing_script: '',
  request_script: '',
  response_script: '',
  query_script: '',
  error_script: '',
  key_pool_id: '',
  auth_type: 'bearer',
  auth_param_name: '',
  auth_region: '',
  auth_service: '',
  passthrough_headers: false,
  passthrough_body: false,
  weight: '1',
  priority: '0',
  icon_url: '',
  description: '',
  display_name: '',
  groups: [],
  is_active: true,
}

function prettyJson(value: unknown) {
  if (!value || (typeof value === 'object' && Object.keys(value as object).length === 0)) {
    return emptyJson
  }
  return JSON.stringify(value, null, 2)
}

function parseJsonField(label: string, value: string) {
  try {
    return JSON.parse(value || emptyJson) as Record<string, unknown>
  } catch {
    throw new Error(`${label} 不是合法 JSON`)
  }
}

function parseAmount(value: unknown) {
  if (value === undefined || value === null || value === '') {
    return undefined
  }
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : undefined
}

function formatCnyValue(value: number) {
  return value.toFixed(6).replace(/\.0+$/, '').replace(/(\.\d*?[1-9])0+$/, '$1')
}

function toCnyInput(value: unknown): string {
  const amount = parseAmount(value)
  if (amount === undefined) {
    return ''
  }
  return formatCnyValue(amount / creditsPrecision)
}

function formatCnyDisplay(value: unknown): string {
  const amount = parseAmount(value)
  if (amount === undefined) {
    return 'CNY 0'
  }
  return `CNY ${formatCnyValue(amount / creditsPrecision)}`
}

function fromCnyInput(value: string) {
  const trimmed = value.trim()
  if (!trimmed) {
    return undefined
  }
  const parsed = Number(trimmed)
  if (!Number.isFinite(parsed)) {
    return undefined
  }
  return Math.round(parsed * creditsPrecision)
}

function getNum(cfg: Record<string, unknown>, key: string): string {
  return toCnyInput(cfg[key])
}

function getTierNum(cfg: Record<string, unknown>, key: 'size_prices' | 'size_costs', tier: (typeof sizeTierKeys)[number]): string {
  const raw = cfg[key]
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    return ''
  }
  const value = (raw as Record<string, unknown>)[tier]
  return toCnyInput(value)
}

function buildAdvancedBillingConfigText(cfg: Record<string, unknown>) {
  const rest = Object.fromEntries(
    Object.entries(cfg).filter(([key]) => !structuredBillingConfigKeys.has(key))
  )
  return prettyJson(rest)
}

function buildSizeMap(entries: Array<[string, string]>) {
  const mapped = Object.fromEntries(
    entries
      .filter(([, value]) => value.trim() !== '')
      .flatMap(([key, value]) => {
        const parsed = fromCnyInput(value)
        return parsed === undefined ? [] : [[key, parsed]]
      })
  )
  return Object.keys(mapped).length > 0 ? mapped : undefined
}

function extractPricingGroups(cfg: Record<string, unknown>): PricingGroupForm[] {
  const raw = cfg.pricing_groups
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    return []
  }

  return Object.entries(raw)
    .filter(([, value]) => value && typeof value === 'object' && !Array.isArray(value))
    .map(([group, value]) => {
      const override = value as Record<string, unknown>
      return {
        group,
        input_price_per_1m_tokens: getNum(override, 'input_price_per_1m_tokens'),
        output_price_per_1m_tokens: getNum(override, 'output_price_per_1m_tokens'),
        base_price: getNum(override, 'base_price'),
        default_size_price: getNum(override, 'default_size_price'),
        size_price_1k: getTierNum(override, 'size_prices', '1k'),
        size_price_2k: getTierNum(override, 'size_prices', '2k'),
        size_price_3k: getTierNum(override, 'size_prices', '3k'),
        size_price_4k: getTierNum(override, 'size_prices', '4k'),
        price_per_second: getNum(override, 'price_per_second'),
        price_per_call: getNum(override, 'price_per_call'),
        extra_config: Object.fromEntries(
          Object.entries(override).filter(([key]) => !pricingGroupOverrideKeys.has(key))
        ),
      }
    })
}

function buildPricingGroups(groups: PricingGroupForm[], billingType: string): Record<string, unknown> | undefined {
  const entries = groups.flatMap((group) => {
    const name = group.group.trim()
    if (!name) {
      return []
    }

    const override: Record<string, unknown> = { ...group.extra_config }
    for (const key of pricingGroupOverrideKeys) {
      delete override[key]
    }

    const setNumber = (key: string, value: string) => {
      const parsed = fromCnyInput(value)
      if (parsed === undefined) {
        return
      }
      override[key] = parsed
    }

    switch (billingType) {
      case 'token':
        setNumber('input_price_per_1m_tokens', group.input_price_per_1m_tokens)
        setNumber('output_price_per_1m_tokens', group.output_price_per_1m_tokens)
        break
      case 'image': {
        setNumber('base_price', group.base_price)
        setNumber('default_size_price', group.default_size_price)
        const sizePrices = buildSizeMap([
          ['1k', group.size_price_1k],
          ['2k', group.size_price_2k],
          ['3k', group.size_price_3k],
          ['4k', group.size_price_4k],
        ])
        if (sizePrices) {
          override.size_prices = sizePrices
        }
        break
      }
      case 'video':
      case 'audio':
        setNumber('price_per_second', group.price_per_second)
        break
      case 'count':
        setNumber('price_per_call', group.price_per_call)
        break
    }

    if (Object.keys(override).length === 0) {
      return []
    }

    return [[name, override] as const]
  })

  if (entries.length === 0) {
    return undefined
  }

  return Object.fromEntries(entries)
}

function formatBillingSummary(billingType: string | undefined, cfg: Record<string, unknown>, mode: 'price' | 'cost' = 'price') {
  const sizeMapKey = mode === 'price' ? 'size_prices' : 'size_costs'
  const defaultSizeKey = mode === 'price' ? 'default_size_price' : 'default_size_cost'
  const baseKey = mode === 'price' ? 'base_price' : 'base_cost'
  const pricePerSecondKey = mode === 'price' ? 'price_per_second' : 'cost_per_second'
  const pricePerCallKey = mode === 'price' ? 'price_per_call' : 'cost_per_call'
  const inputKey = mode === 'price' ? 'input_price_per_1m_tokens' : 'input_cost_per_1m_tokens'
  const outputKey = mode === 'price' ? 'output_price_per_1m_tokens' : 'output_cost_per_1m_tokens'

  switch (billingType) {
    case 'token':
      return `输入 ${formatCnyDisplay(cfg[inputKey])} / 输出 ${formatCnyDisplay(cfg[outputKey])}`
    case 'image': {
      const sizePrices = cfg[sizeMapKey]
      if (sizePrices && typeof sizePrices === 'object' && !Array.isArray(sizePrices)) {
        const parts = sizeTierKeys
          .map((key) => {
            const value = (sizePrices as Record<string, unknown>)[key]
            return value !== undefined && value !== null && value !== 0 ? `${key}:${formatCnyDisplay(value)}` : null
          })
          .filter(Boolean)
        if (parts.length > 0) {
          return parts.join(' / ')
        }
      }
      return `基础 ${formatCnyDisplay(cfg[defaultSizeKey] ?? cfg[baseKey] ?? 0)}`
    }
    case 'video':
    case 'audio':
      return `${formatCnyDisplay(cfg[pricePerSecondKey] ?? 0)} /秒`
    case 'count':
      return `${formatCnyDisplay(cfg[pricePerCallKey] ?? 0)} /次`
    default:
      return '—'
  }
}

function formatGroupPricing(channel: AdminChannel) {
  if (channel.billing_type === 'custom') {
    return '—'
  }

  const pricingGroups = channel.billing_config?.pricing_groups
  if (!pricingGroups || typeof pricingGroups !== 'object' || Array.isArray(pricingGroups)) {
    return '—'
  }

  const entries = Object.entries(pricingGroups)
    .filter(([, value]) => value && typeof value === 'object' && !Array.isArray(value))
    .map(([group, value]) => `${group}: ${formatBillingSummary(channel.billing_type, value as Record<string, unknown>, 'price')}`)

  if (entries.length === 0) {
    return '—'
  }

  if (entries.length <= 2) {
    return entries.join(' | ')
  }

  return `${entries.slice(0, 2).join(' | ')} | +${entries.length - 2}组`
}

function buildBillingConfig(form: ChannelForm): Record<string, unknown> {
  const cfg = parseJsonField('高级计费配置', form.billing_config_text)

  for (const key of structuredBillingConfigKeys) {
    delete cfg[key]
  }

  const markup = parseFloat(form.billing_markup) || 1

  const setNumber = (key: string, value: string) => {
    const parsed = fromCnyInput(value)
    if (parsed === undefined) {
      return
    }
    cfg[key] = parsed
  }

  // Compute selling price from cost * markup
  const setCostAndPrice = (costKey: string, priceKey: string, costValue: string) => {
    const cost = fromCnyInput(costValue)
    if (cost === undefined) return
    cfg[costKey] = cost
    cfg[priceKey] = Math.round(cost * markup)
  }

  switch (form.billing_type) {
    case 'token':
      setCostAndPrice('input_cost_per_1m_tokens', 'input_price_per_1m_tokens', form.billing_input_cost)
      setCostAndPrice('output_cost_per_1m_tokens', 'output_price_per_1m_tokens', form.billing_output_cost)
      setCostAndPrice('cache_creation_cost_per_1m_tokens', 'cache_creation_price_per_1m_tokens', form.billing_cache_create_cost)
      setCostAndPrice('cache_read_cost_per_1m_tokens', 'cache_read_price_per_1m_tokens', form.billing_cache_read_cost)
      if (form.billing_input_from_response) {
        cfg.input_from_response = true
      }
      break
    case 'image': {
      setCostAndPrice('base_cost', 'base_price', form.billing_base_cost)
      setCostAndPrice('default_size_cost', 'default_size_price', form.billing_default_size_cost)

      const sizeCosts = buildSizeMap([
        ['1k', form.billing_size_cost_1k],
        ['2k', form.billing_size_cost_2k],
        ['3k', form.billing_size_cost_3k],
        ['4k', form.billing_size_cost_4k],
      ])
      if (sizeCosts) {
        cfg.size_costs = sizeCosts
        // build size_prices from size_costs * markup
        const sizePrices: Record<string, number> = {}
        for (const [k, v] of Object.entries(sizeCosts)) {
          sizePrices[k] = Math.round((v as number) * markup)
        }
        cfg.size_prices = sizePrices
      } else {
        const sizePrices = buildSizeMap([
          ['1k', form.billing_size_price_1k],
          ['2k', form.billing_size_price_2k],
          ['3k', form.billing_size_price_3k],
          ['4k', form.billing_size_price_4k],
        ])
        if (sizePrices) {
          cfg.size_prices = sizePrices
        }
        setNumber('base_price', form.billing_base_price)
        setNumber('default_size_price', form.billing_default_size_price)
      }
      break
    }
    case 'video':
    case 'audio':
      setCostAndPrice('cost_per_second', 'price_per_second', form.billing_cost_per_second)
      break
    case 'count':
      setCostAndPrice('cost_per_call', 'price_per_call', form.billing_cost_per_call)
      break
  }

  const pricingGroups = buildPricingGroups(form.pricing_groups, form.billing_type)
  if (pricingGroups) {
    cfg.pricing_groups = pricingGroups
  }

  return cfg
}

function buildFormFromChannel(row: AdminChannel, isCopy = false): ChannelForm {
  const billingConfig = row.billing_config ?? {}

  return {
    ...emptyForm,
    id: isCopy ? undefined : row.id,
    name: isCopy ? `${row.name ?? ''} - 副本` : row.name ?? '',
    model: row.model ?? row.routing_model ?? '',
    type: row.type ?? 'llm',
    protocol: row.protocol ?? 'openai',
    base_url: row.base_url ?? '',
    method: row.method ?? 'POST',
    query_url: row.query_url ?? '',
    query_method: row.query_method ?? 'GET',
    timeout_ms: String(row.timeout_ms ?? 60000),
    query_timeout_ms: String(row.query_timeout_ms ?? 30000),
    billing_type: row.billing_type ?? 'token',
    headers_text: prettyJson(row.headers),
    billing_config_text: buildAdvancedBillingConfigText(billingConfig),
    pricing_groups: extractPricingGroups(billingConfig),
    billing_markup: (() => {
      // Infer markup from input price / input cost ratio; fallback to 1.2
      const price = parseAmount(billingConfig.input_price_per_1m_tokens)
      const cost = parseAmount(billingConfig.input_cost_per_1m_tokens)
      if (price && cost && cost > 0) {
        return String(Math.round((price / cost) * 100) / 100)
      }
      const bPrice = parseAmount(billingConfig.base_price)
      const bCost = parseAmount(billingConfig.base_cost)
      if (bPrice && bCost && bCost > 0) {
        return String(Math.round((bPrice / bCost) * 100) / 100)
      }
      const sPrice = parseAmount(billingConfig.price_per_second)
      const sCost = parseAmount(billingConfig.cost_per_second)
      if (sPrice && sCost && sCost > 0) {
        return String(Math.round((sPrice / sCost) * 100) / 100)
      }
      return '1.2'
    })(),
    billing_input_price: getNum(billingConfig, 'input_price_per_1m_tokens'),
    billing_output_price: getNum(billingConfig, 'output_price_per_1m_tokens'),
    billing_input_cost: getNum(billingConfig, 'input_cost_per_1m_tokens'),
    billing_output_cost: getNum(billingConfig, 'output_cost_per_1m_tokens'),
    billing_cache_create_price: getNum(billingConfig, 'cache_creation_price_per_1m_tokens'),
    billing_cache_create_cost: getNum(billingConfig, 'cache_creation_cost_per_1m_tokens'),
    billing_cache_read_price: getNum(billingConfig, 'cache_read_price_per_1m_tokens'),
    billing_cache_read_cost: getNum(billingConfig, 'cache_read_cost_per_1m_tokens'),
    billing_input_from_response: Boolean(billingConfig.input_from_response),
    billing_base_price: getNum(billingConfig, 'base_price'),
    billing_base_cost: getNum(billingConfig, 'base_cost'),
    billing_size_price_1k: getTierNum(billingConfig, 'size_prices', '1k'),
    billing_size_price_2k: getTierNum(billingConfig, 'size_prices', '2k'),
    billing_size_price_3k: getTierNum(billingConfig, 'size_prices', '3k'),
    billing_size_price_4k: getTierNum(billingConfig, 'size_prices', '4k'),
    billing_size_cost_1k: getTierNum(billingConfig, 'size_costs', '1k'),
    billing_size_cost_2k: getTierNum(billingConfig, 'size_costs', '2k'),
    billing_size_cost_3k: getTierNum(billingConfig, 'size_costs', '3k'),
    billing_size_cost_4k: getTierNum(billingConfig, 'size_costs', '4k'),
    billing_default_size_price: getNum(billingConfig, 'default_size_price'),
    billing_default_size_cost: getNum(billingConfig, 'default_size_cost'),
    billing_price_per_second: getNum(billingConfig, 'price_per_second'),
    billing_cost_per_second: getNum(billingConfig, 'cost_per_second'),
    billing_price_per_call: getNum(billingConfig, 'price_per_call'),
    billing_cost_per_call: getNum(billingConfig, 'cost_per_call'),
    billing_script: row.billing_script ?? '',
    request_script: row.request_script ?? '',
    response_script: row.response_script ?? '',
    query_script: row.query_script ?? '',
    error_script: row.error_script ?? '',
    key_pool_id: row.key_pool_id ? String(row.key_pool_id) : '',
    auth_type: row.auth_type ?? 'bearer',
    auth_param_name: row.auth_param_name ?? '',
    auth_region: row.auth_region ?? '',
    auth_service: row.auth_service ?? '',
    passthrough_headers: row.passthrough_headers ?? false,
    passthrough_body: row.passthrough_body ?? false,
    weight: String(row.weight ?? 1),
    priority: String(row.priority ?? 0),
    icon_url: row.icon_url ?? '',
    description: row.description ?? '',
    display_name: row.display_name ?? '',
    groups: row.groups ?? [],
    is_active: row.is_active ?? true,
  }
}

function formatBilling(channel: AdminChannel) {
  return formatBillingSummary(channel.billing_type, channel.billing_config ?? {}, 'price')
}

function formatBillingCost(channel: AdminChannel) {
  return formatBillingSummary(channel.billing_type, channel.billing_config ?? {}, 'cost')
}

const channelPageSize = 20

const emptyChannelFilters = {
  q: '',
  name: '',
  display_name: '',
  price_min: '',
  price_max: '',
  price_order: '',
}

export function AdminChannelsPage() {
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState({ ...emptyChannelFilters })
  const [queryParams, setQueryParams] = useState<Record<string, string>>({})
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())

  const { data, loading, error: loadError, reload } = useAsync(async () => {
    const [channelResponse, poolResponse] = await Promise.all([
      adminApi.listChannels({ page, size: channelPageSize, ...queryParams }),
      adminApi.listKeyPools(),
    ])
    const rows = Array.isArray(channelResponse)
      ? channelResponse
      : channelResponse.channels ?? channelResponse.items ?? []
    const total = Array.isArray(channelResponse) ? rows.length : channelResponse.total ?? rows.length
    const pools = Array.isArray(poolResponse) ? poolResponse : poolResponse.pools ?? []
    setSelectedIds(new Set())
    return { rows, pools, total }
  }, { rows: [] as AdminChannel[], pools: [] as AdminKeyPool[], total: 0 }, [page, queryParams])

  const rows = data.rows
  const pools = data.pools
  const total = data.total

  const [mutError, setMutError] = useState('')
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<ChannelForm>(emptyForm)
  const [pendingDeleteChannel, setPendingDeleteChannel] = useState<AdminChannel | undefined>()
  const [uploadingIcon, setUploadingIcon] = useState(false)
  const iconUploadRef = useRef<HTMLInputElement>(null)

  // 批量选择
  const [batchRateOpen, setBatchRateOpen] = useState(false)
  const [batchRate, setBatchRate] = useState('')
  const [batchMutating, setBatchMutating] = useState(false)

  // 变更日志侧面板
  const [logChannel, setLogChannel] = useState<AdminChannel | null>(null)

  const error = loadError || mutError

  const poolOptions = useMemo(
    () =>
      pools.filter((pool) =>
        form.id
          ? pool.channel_id === form.id || String(pool.channel_id) === form.key_pool_id
          : pool.channel_id === Number(form.key_pool_id || form.id || 0) || pool.channel_id === 0
      ),
    [form.id, form.key_pool_id, pools]
  )

  function openCreate() {
    setForm(emptyForm)
    setOpen(true)
    setMutError('')
  }

  function openEdit(row: AdminChannel) {
    setForm(buildFormFromChannel(row))
    setOpen(true)
    setMutError('')
  }

  function openCopy(row: AdminChannel) {
    setForm(buildFormFromChannel(row, true))
    setOpen(true)
    setMutError('')
  }

  function updatePricingGroup(index: number, patch: Partial<PricingGroupForm>) {
    setForm((current) => ({
      ...current,
      pricing_groups: current.pricing_groups.map((group, groupIndex) =>
        groupIndex === index ? { ...group, ...patch } : group
      ),
    }))
  }

  function addPricingGroup() {
    setForm((current) => ({
      ...current,
      pricing_groups: [...current.pricing_groups, createEmptyPricingGroup()],
    }))
  }

  function removePricingGroup(index: number) {
    setForm((current) => ({
      ...current,
      pricing_groups: current.pricing_groups.filter((_, groupIndex) => groupIndex !== index),
    }))
  }

  async function uploadChannelIcon(file: File | undefined) {
    if (!file) {
      return
    }
    setMutError('')
    setUploadingIcon(true)
    try {
      const response = await adminApi.uploadImage(file, 'channel-icon')
      const url = response.url ?? ''
      if (!url) {
        throw new Error('上传失败，未返回图片地址')
      }
      setForm((current) => ({ ...current, icon_url: url }))
      toast.success('渠道图标上传成功')
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      const msg = getApiErrorMessage(err)
      setMutError(msg)
      toast.error(msg)
    } finally {
      setUploadingIcon(false)
    }
  }

  async function saveChannel() {
    setMutError('')
    try {
      const payload = {
        name: form.name.trim(),
        model: form.model.trim(),
        type: form.type,
        protocol: form.protocol,
        base_url: form.base_url.trim(),
        method: form.method,
        query_url: form.query_url.trim(),
        query_method: form.query_method,
        timeout_ms: Number(form.timeout_ms || '60000'),
        query_timeout_ms: Number(form.query_timeout_ms || '30000'),
        billing_type: form.billing_type,
        headers: parseJsonField('请求头', form.headers_text),
        billing_config: buildBillingConfig(form),
        billing_script: form.billing_script,
        request_script: form.request_script,
        response_script: form.response_script,
        query_script: form.query_script,
        error_script: form.error_script,
        key_pool_id: Number(form.key_pool_id || '0'),
        auth_type: form.auth_type,
        auth_param_name: form.auth_param_name.trim(),
        auth_region: form.auth_region.trim(),
        auth_service: form.auth_service.trim(),
        passthrough_headers: form.passthrough_headers,
        passthrough_body: form.passthrough_body,
        weight: Number(form.weight || '1'),
        priority: Number(form.priority || '0'),
        icon_url: form.icon_url.trim(),
        description: form.description.trim(),
        display_name: form.display_name.trim(),
        groups: form.groups,
        is_active: form.is_active,
      }
      if (form.id) {
        await adminApi.updateChannel(form.id, payload)
      } else {
        await adminApi.createChannel(payload)
      }
      setOpen(false)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function toggleChannel(row: AdminChannel) {
    if (!row.id) return
    setMutError('')
    try {
      await adminApi.toggleChannel(row.id, !(row.is_active ?? true))
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function executeDeleteChannel() {
    if (!pendingDeleteChannel?.id) return
    setMutError('')
    try {
      await adminApi.deleteChannel(pendingDeleteChannel.id)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    } finally {
      setPendingDeleteChannel(undefined)
    }
  }

  async function batchToggleActive(isActive: boolean) {
    if (selectedIds.size === 0) return
    setBatchMutating(true)
    setMutError('')
    try {
      await adminApi.batchUpdateChannels({ action: 'toggle_active', ids: Array.from(selectedIds), is_active: isActive })
      setSelectedIds(new Set())
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    } finally {
      setBatchMutating(false)
    }
  }

  async function batchSetRate() {
    if (selectedIds.size === 0 || !batchRate.trim()) return
    setBatchMutating(true)
    setMutError('')
    try {
      await adminApi.batchUpdateChannels({ action: 'set_rate', ids: Array.from(selectedIds), rate_ratio: Number(batchRate) })
      setSelectedIds(new Set())
      setBatchRateOpen(false)
      setBatchRate('')
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    } finally {
      setBatchMutating(false)
    }
  }

  function toggleSelect(id: number) {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  function doSearch(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault()
    const params: Record<string, string> = {}
    const q = filters.q.trim()
    const name = filters.name.trim()
    const displayName = filters.display_name.trim()
    const priceMin = filters.price_min.trim()
    const priceMax = filters.price_max.trim()
    if (q) params.q = q
    if (name) params.name = name
    if (displayName) params.display_name = displayName
    if (priceMin) params.price_min = priceMin
    if (priceMax) params.price_max = priceMax
    if (filters.price_order) {
      params.sort_by = 'price'
      params.sort_order = filters.price_order
    }
    setPage(1)
    setQueryParams(params)
  }

  function resetSearch() {
    setFilters({ ...emptyChannelFilters })
    setPage(1)
    setQueryParams({})
  }

  const allOnPageSelected = rows.length > 0 && rows.every((r) => r.id != null && selectedIds.has(r.id))

  function toggleSelectAll() {
    if (allOnPageSelected) {
      setSelectedIds(new Set())
    } else {
      setSelectedIds(new Set(rows.filter((r) => r.id != null).map((r) => r.id as number)))
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Catalog"
        title="渠道管理"
        description="管理 API 渠道，支持认证、计费、脚本、轮询、号池和负载参数。"
        actions={
          <>
            {error ? (
              <Button size="sm" variant="outline" onClick={reload}>
                重试
              </Button>
            ) : null}
            <Button onClick={openCreate}>
              <PlusIcon data-icon="inline-start" />
              新增渠道
            </Button>
          </>
        }
      />
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {/* 批量操作工具栏 */}
      <Card>
        <CardContent className="py-4">
          <form className="flex flex-wrap items-end gap-3" onSubmit={doSearch}>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">全部</label>
              <Input
                className="w-44"
                placeholder="名称 / 模型"
                value={filters.q}
                onChange={(event) => setFilters((current) => ({ ...current, q: event.target.value }))}
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">渠道名称</label>
              <Input
                className="w-44"
                placeholder="渠道名称"
                value={filters.name}
                onChange={(event) => setFilters((current) => ({ ...current, name: event.target.value }))}
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">自定义名称</label>
              <Input
                className="w-44"
                placeholder="展示名称"
                value={filters.display_name}
                onChange={(event) => setFilters((current) => ({ ...current, display_name: event.target.value }))}
              />
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">价格范围(CNY)</label>
              <div className="flex items-center gap-1">
                <Input
                  className="w-24"
                  inputMode="decimal"
                  placeholder="最低"
                  value={filters.price_min}
                  onChange={(event) => setFilters((current) => ({ ...current, price_min: event.target.value }))}
                />
                <span className="text-muted-foreground">-</span>
                <Input
                  className="w-24"
                  inputMode="decimal"
                  placeholder="最高"
                  value={filters.price_max}
                  onChange={(event) => setFilters((current) => ({ ...current, price_max: event.target.value }))}
                />
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">价格排序</label>
              <NativeSelect
                className="w-32"
                value={filters.price_order}
                onChange={(event) => setFilters((current) => ({ ...current, price_order: event.target.value }))}
              >
                <option value="">默认</option>
                <option value="asc">低到高</option>
                <option value="desc">高到低</option>
              </NativeSelect>
            </div>
            <Button type="submit">
              <SearchIcon data-icon="inline-start" />
              搜索
            </Button>
            <Button type="button" variant="outline" onClick={resetSearch}>
              <RotateCcwIcon data-icon="inline-start" />
              重置
            </Button>
          </form>
        </CardContent>
      </Card>

      {selectedIds.size > 0 ? (
        <div className="flex items-center gap-3 rounded-lg border bg-muted/40 px-4 py-2.5">
          <span className="text-sm font-medium">已选 {selectedIds.size} 个渠道</span>
          <div className="flex items-center gap-2 ml-2">
            <Button size="sm" variant="outline" disabled={batchMutating} onClick={() => batchToggleActive(true)}>批量启用</Button>
            <Button size="sm" variant="outline" disabled={batchMutating} onClick={() => batchToggleActive(false)}>批量停用</Button>
            <Button size="sm" variant="outline" disabled={batchMutating} onClick={() => { setBatchRate(''); setBatchRateOpen(true) }}>批量设权重</Button>
          </div>
          <Button size="sm" variant="ghost" className="ml-auto" onClick={() => setSelectedIds(new Set())}>取消</Button>
        </div>
      ) : null}

      <Card className="overflow-hidden">
        <Table className="min-w-[1500px]">
          <TableHeader>
            <TableRow>
              <TableHead className="w-10">
                <Checkbox checked={allOnPageSelected} onCheckedChange={toggleSelectAll} aria-label="全选" />
              </TableHead>
              <TableHead className="w-14">ID</TableHead>
              <TableHead>名称</TableHead>
              <TableHead>模型</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>协议</TableHead>
              <TableHead>价格摘要</TableHead>
              <TableHead>号池</TableHead>
              <TableHead>优先级/权重</TableHead>
              <TableHead>健康</TableHead>
              <TableHead>状态</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          {loading ? (
            <TableSkeleton cols={12} />
          ) : (
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={12} className="py-10 text-center text-muted-foreground">
                    暂无渠道数据
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row, index) => (
                  <TableRow key={row.id ?? index} data-state={row.id != null && selectedIds.has(row.id) ? 'selected' : undefined}>
                    <TableCell>
                      <Checkbox
                        checked={row.id != null && selectedIds.has(row.id)}
                        onCheckedChange={() => row.id != null && toggleSelect(row.id)}
                      />
                    </TableCell>
                    <TableCell className="text-muted-foreground">{row.id ?? '-'}</TableCell>
                    <TableCell className="max-w-56">
                      <div className="font-medium">{row.name ?? '未命名渠道'}</div>
                      {row.description ? (
                        <div className="line-clamp-1 text-xs text-muted-foreground">{row.description}</div>
                      ) : null}
                    </TableCell>
                    <TableCell className="max-w-48 break-all text-xs">{row.model ?? row.routing_model ?? '-'}</TableCell>
                    <TableCell>{row.type ?? '-'}</TableCell>
                    <TableCell>{row.protocol ?? 'openai'}</TableCell>
                    <TableCell className="max-w-80 align-top text-xs">
                      <div className="flex flex-col gap-1">
                        <div>
                          <span className="text-muted-foreground">售价：</span>
                          <span>{formatBilling(row)}</span>
                        </div>
                        <div>
                          <span className="text-muted-foreground">进价：</span>
                          <span>{formatBillingCost(row)}</span>
                        </div>
                        <div>
                          <span className="text-muted-foreground">分组价：</span>
                          <span>{formatGroupPricing(row)}</span>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>{row.key_pool_id ? `#${row.key_pool_id}` : '—'}</TableCell>
                    <TableCell className="text-xs">P{row.priority ?? 0} / W{row.weight ?? 1}</TableCell>
                    <TableCell>
                      {row.id ? <ChannelHealthBadge channelId={row.id} /> : <Badge variant="secondary">N/A</Badge>}
                    </TableCell>
                    <TableCell>
                      <Badge variant={row.is_active === false ? 'secondary' : 'default'}>
                        {row.is_active === false ? '停用' : '启用'}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-2">
                        <Button size="sm" variant="outline" onClick={() => openEdit(row)}>编辑</Button>
                        <Button size="sm" variant="outline" onClick={() => openCopy(row)}>
                          <CopyIcon data-icon="inline-start" />
                          复制
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => setLogChannel(row)}>日志</Button>
                        <Button size="sm" variant="outline" onClick={() => toggleChannel(row)}>
                          {row.is_active === false ? '启用' : '停用'}
                        </Button>
                        <Button size="sm" onClick={() => setPendingDeleteChannel(row)}>删除</Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          )}
        </Table>
      </Card>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="text-sm text-muted-foreground">
          共 {total} 个渠道，每页 {channelPageSize} 个
        </div>
        <TablePagination current={page} total={total} pageSize={channelPageSize} onChange={setPage} />
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="w-[min(calc(100vw-2rem),1280px)] max-w-none sm:max-w-[1280px]">
          <DialogHeader>
            <DialogTitle>{form.id ? '编辑渠道' : '新增渠道'}</DialogTitle>
            <DialogDescription>覆盖上游接入所需的核心字段。</DialogDescription>
          </DialogHeader>

          <Tabs defaultValue="basic">
            <TabsList className="w-full">
              <TabsTrigger value="basic">基本信息</TabsTrigger>
              <TabsTrigger value="auth">认证 &amp; 号池</TabsTrigger>
              <TabsTrigger value="billing">计费</TabsTrigger>
              <TabsTrigger value="scripts">脚本 &amp; 轮询</TabsTrigger>
            </TabsList>

            {/* ── 基本信息 ── */}
            <TabsContent value="basic" className="mt-5 max-h-[62vh] overflow-y-auto pr-1">
              <div className="grid gap-5 md:grid-cols-2">
                <div className="space-y-2">
                  <label className="text-sm font-medium">路由名称</label>
                  <Input value={form.name} onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))} />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">标准模型名</label>
                  <Input value={form.model} onChange={(event) => setForm((current) => ({ ...current, model: event.target.value }))} />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">接口类型</label>
                  <NativeSelect value={form.type} onChange={(event) => setForm((current) => ({ ...current, type: event.target.value }))}>
                    <option value="llm">llm</option>
                    <option value="image">image</option>
                    <option value="video">video</option>
                    <option value="audio">audio</option>
                    <option value="music">music</option>
                  </NativeSelect>
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">协议</label>
                  <NativeSelect value={form.protocol} onChange={(event) => setForm((current) => ({ ...current, protocol: event.target.value }))}>
                    <option value="openai">openai</option>
                    <option value="claude">claude</option>
                    <option value="gemini">gemini</option>
                    <option value="responses">responses</option>
                  </NativeSelect>
                </div>
                <div className="space-y-2 md:col-span-2">
                  <label className="text-sm font-medium">上游 URL</label>
                  <Input value={form.base_url} onChange={(event) => setForm((current) => ({ ...current, base_url: event.target.value }))} placeholder="https://api.example.com/v1/chat/completions" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">请求方法</label>
                  <NativeSelect value={form.method} onChange={(event) => setForm((current) => ({ ...current, method: event.target.value }))}>
                    <option value="POST">POST</option>
                    <option value="GET">GET</option>
                  </NativeSelect>
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">超时（ms）</label>
                  <Input value={form.timeout_ms} onChange={(event) => setForm((current) => ({ ...current, timeout_ms: event.target.value }))} />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">图标 URL</label>
                  <div className="flex gap-2">
                    <Input value={form.icon_url} onChange={(event) => setForm((current) => ({ ...current, icon_url: event.target.value }))} placeholder="https://…/icon.png" />
                    <input
                      ref={iconUploadRef}
                      type="file"
                      accept="image/*"
                      className="hidden"
                      onChange={(event) => {
                        void uploadChannelIcon(event.target.files?.[0])
                        event.target.value = ''
                      }}
                    />
                    <Button type="button" variant="outline" size="sm" onClick={() => iconUploadRef.current?.click()} disabled={uploadingIcon}>
                      {uploadingIcon ? '上传中...' : '上传'}
                    </Button>
                  </div>
                  {form.icon_url ? (
                    <div className="flex h-16 w-16 items-center justify-center overflow-hidden rounded-xl border bg-muted/20 p-1">
                      <img src={form.icon_url} alt="渠道图标预览" className="max-h-full max-w-full rounded-md object-contain" />
                    </div>
                  ) : null}
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">自定义展示名</label>
                  <Input value={form.display_name} onChange={(event) => setForm((current) => ({ ...current, display_name: event.target.value }))} placeholder="留空则用户端显示标准模型名，相同展示名的渠道归为同一模型" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">描述</label>
                  <Input value={form.description} onChange={(event) => setForm((current) => ({ ...current, description: event.target.value }))} placeholder="可选，显示在渠道名称下方" />
                </div>
                <div className="space-y-2 md:col-span-2">
                  <label className="text-sm font-medium">渠道分组标签</label>
                  <div className="flex flex-wrap gap-2">
                    {['高质', '低价', '备用'].map((tag) => (
                      <button
                        key={tag}
                        type="button"
                        onClick={() => setForm((current) => ({
                          ...current,
                          groups: current.groups.includes(tag)
                            ? current.groups.filter((g) => g !== tag)
                            : [...current.groups, tag],
                        }))}
                        className={`px-3 py-1 rounded-full text-xs border transition-colors ${
                          form.groups.includes(tag)
                            ? 'bg-primary text-primary-foreground border-primary'
                            : 'border-input hover:bg-accent'
                        }`}
                      >
                        {tag}
                      </button>
                    ))}
                    {form.groups.filter((g) => !['高质', '低价', '备用'].includes(g)).map((tag) => (
                      <button
                        key={tag}
                        type="button"
                        onClick={() => setForm((current) => ({ ...current, groups: current.groups.filter((g) => g !== tag) }))}
                        className="px-3 py-1 rounded-full text-xs border bg-secondary text-secondary-foreground"
                      >
                        {tag} ×
                      </button>
                    ))}
                    <div className="flex gap-1">
                      <Input
                        placeholder="自定义标签"
                        className="h-7 w-24 text-xs"
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') {
                            e.preventDefault()
                            const val = (e.target as HTMLInputElement).value.trim()
                            if (val && !form.groups.includes(val)) {
                              setForm((current) => ({ ...current, groups: [...current.groups, val] }));(e.target as HTMLInputElement).value = ''
                            }
                          }
                        }}
                      />
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-2 md:col-span-2 pt-1">
                  <input
                    id="channel-active"
                    type="checkbox"
                    checked={form.is_active}
                    onChange={(event) => setForm((current) => ({ ...current, is_active: event.target.checked }))}
                    className="h-4 w-4 rounded border-input"
                  />
                  <label htmlFor="channel-active" className="cursor-pointer text-sm font-medium">渠道启用</label>
                </div>
              </div>
            </TabsContent>

            {/* ── 认证 & 号池 ── */}
            <TabsContent value="auth" className="mt-5 max-h-[62vh] overflow-y-auto pr-1">
              <div className="grid gap-5 md:grid-cols-2">
                <div className="space-y-2">
                  <label className="text-sm font-medium">认证方式</label>
                  <NativeSelect value={form.auth_type} onChange={(event) => setForm((current) => ({ ...current, auth_type: event.target.value }))}>
                    <option value="bearer">bearer</option>
                    <option value="query_param">query_param</option>
                    <option value="basic">basic</option>
                    <option value="sigv4">sigv4</option>
                  </NativeSelect>
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">Query Param 名</label>
                  <Input value={form.auth_param_name} onChange={(event) => setForm((current) => ({ ...current, auth_param_name: event.target.value }))} placeholder="如 key（query_param 认证用）" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">AWS Region</label>
                  <Input value={form.auth_region} onChange={(event) => setForm((current) => ({ ...current, auth_region: event.target.value }))} placeholder="us-east-1（sigv4 认证用）" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">AWS Service</label>
                  <Input value={form.auth_service} onChange={(event) => setForm((current) => ({ ...current, auth_service: event.target.value }))} placeholder="execute-api（sigv4 认证用）" />
                </div>

                <div className="border-t pt-4 md:col-span-2" />

                <div className="flex items-center gap-2">
                  <input
                    id="channel-passthrough-headers"
                    type="checkbox"
                    checked={form.passthrough_headers}
                    onChange={(event) => setForm((current) => ({ ...current, passthrough_headers: event.target.checked }))}
                    className="h-4 w-4 rounded border-input"
                  />
                  <label htmlFor="channel-passthrough-headers" className="cursor-pointer text-sm font-medium">透传请求头（passthrough_headers）</label>
                  <span className="text-xs text-muted-foreground">将客户端 User-Agent、Anthropic-Version 等头原样转发给上游</span>
                </div>
                <div className="flex items-center gap-2">
                  <input
                    id="channel-passthrough-body"
                    type="checkbox"
                    checked={form.passthrough_body}
                    onChange={(event) => setForm((current) => ({ ...current, passthrough_body: event.target.checked }))}
                    className="h-4 w-4 rounded border-input"
                  />
                  <label htmlFor="channel-passthrough-body" className="cursor-pointer text-sm font-medium">透传请求体（passthrough_body）</label>
                  <span className="text-xs text-muted-foreground">跳过协议转换和脚本，原样转发原始请求体（适用于签名校验场景）</span>
                </div>

                <div className="border-t pt-4 md:col-span-2" />

                <div className="space-y-2">
                  <label className="text-sm font-medium">号池绑定</label>
                  <NativeSelect value={form.key_pool_id} onChange={(event) => setForm((current) => ({ ...current, key_pool_id: event.target.value }))}>
                    <option value="">不启用</option>
                    {poolOptions.map((pool) => (
                      <option key={pool.id} value={String(pool.id)}>
                        #{pool.id} {pool.name}
                      </option>
                    ))}
                  </NativeSelect>
                </div>
                <div className="space-y-2">{/* placeholder for grid alignment */}</div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">优先级</label>
                  <Input value={form.priority} onChange={(event) => setForm((current) => ({ ...current, priority: event.target.value }))} placeholder="数值越大越优先" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">权重</label>
                  <Input value={form.weight} onChange={(event) => setForm((current) => ({ ...current, weight: event.target.value }))} placeholder="加权随机，越大被选中概率越高" />
                </div>

                <div className="border-t pt-4 md:col-span-2" />

                <div className="space-y-2 md:col-span-2">
                  <label className="text-sm font-medium">请求头（JSON）</label>
                  <p className="text-xs text-muted-foreground">固定注入到每次上游请求的 HTTP 头，如 Authorization。</p>
                  <Textarea
                    value={form.headers_text}
                    onChange={(event) => setForm((current) => ({ ...current, headers_text: event.target.value }))}
                    rows={6}
                    className="font-mono text-xs"
                  />
                </div>
              </div>
            </TabsContent>

            {/* ── 计费 ── */}
            <TabsContent value="billing" className="mt-5 max-h-[62vh] overflow-y-auto pr-1">
              <div className="grid gap-5 md:grid-cols-2">
                <div className="space-y-2 md:col-span-2">
                  <label className="text-sm font-medium">计费类型</label>
                  <NativeSelect value={form.billing_type} onChange={(event) => setForm((current) => ({ ...current, billing_type: event.target.value }))}>
                    <option value="token">token — 按 token 数计费</option>
                    <option value="image">image — 按图片张数计费</option>
                    <option value="video">video — 按视频秒数计费</option>
                    <option value="audio">audio — 按音频秒数计费</option>
                    <option value="count">count — 按调用次数计费</option>
                    <option value="custom">custom — 自定义脚本计费</option>
                  </NativeSelect>
                </div>

                {form.billing_type === 'token' && (
                  <>
                    <div className="space-y-2 md:col-span-2">
                      <label className="text-sm font-medium">利润倍率</label>
                      <p className="text-xs text-muted-foreground">售价 = 成本 × 倍率。例如填 1.2 表示利润 20%。</p>
                      <Input type="number" step="0.01" value={form.billing_markup} onChange={(e) => setForm((c) => ({ ...c, billing_markup: e.target.value }))} placeholder="如 1.2" />
                    </div>
                    <div className="space-y-1 md:col-span-2">
                      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">成本价格</p>
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">输入成本（CNY / 百万 token）</label>
                      <Input type="number" value={form.billing_input_cost} onChange={(e) => setForm((c) => ({ ...c, billing_input_cost: e.target.value }))} placeholder="如 0.612" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">输出成本（CNY / 百万 token）</label>
                      <Input type="number" value={form.billing_output_cost} onChange={(e) => setForm((c) => ({ ...c, billing_output_cost: e.target.value }))} placeholder="如 4.9" />
                    </div>
                    <div className="space-y-1 md:col-span-2">
                      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">缓存写入成本（留空按协议默认倍率）</p>
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">缓存写入成本（CNY / 百万 token）</label>
                      <Input type="number" value={form.billing_cache_create_cost} onChange={(e) => setForm((c) => ({ ...c, billing_cache_create_cost: e.target.value }))} placeholder="留空按协议默认" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">缓存读取成本（CNY / 百万 token）</label>
                      <Input type="number" value={form.billing_cache_read_cost} onChange={(e) => setForm((c) => ({ ...c, billing_cache_read_cost: e.target.value }))} placeholder="留空按协议默认" />
                    </div>
                    <div className="flex items-center gap-2 md:col-span-2">
                      <input
                        id="input-from-response"
                        type="checkbox"
                        checked={form.billing_input_from_response}
                        onChange={(e) => setForm((c) => ({ ...c, billing_input_from_response: e.target.checked }))}
                        className="h-4 w-4 rounded border-input"
                      />
                      <label htmlFor="input-from-response" className="cursor-pointer text-sm font-medium">
                        从响应中获取实际输入 token 数（input_from_response）
                      </label>
                    </div>
                  </>
                )}

                {form.billing_type === 'image' && (
                  <>
                    <div className="space-y-2 md:col-span-2">
                      <label className="text-sm font-medium">利润倍率</label>
                      <p className="text-xs text-muted-foreground">售价 = 成本 × 倍率。例如填 1.2 表示利润 20%。</p>
                      <Input type="number" step="0.01" value={form.billing_markup} onChange={(e) => setForm((c) => ({ ...c, billing_markup: e.target.value }))} placeholder="如 1.2" />
                    </div>
                    <div className="space-y-1 md:col-span-2">
                      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">按档位成本（size_costs）</p>
                      <p className="text-xs text-muted-foreground">填写后按 1k/2k/3k/4k 档位优先计费；留空则使用基础成本。售价自动按倍率计算。</p>
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">1k 进价（CNY / 张）</label>
                      <Input type="number" value={form.billing_size_cost_1k} onChange={(e) => setForm((c) => ({ ...c, billing_size_cost_1k: e.target.value }))} placeholder="如 2.5" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">2k 进价（CNY / 张）</label>
                      <Input type="number" value={form.billing_size_cost_2k} onChange={(e) => setForm((c) => ({ ...c, billing_size_cost_2k: e.target.value }))} placeholder="如 4.2" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">3k 进价（CNY / 张）</label>
                      <Input type="number" value={form.billing_size_cost_3k} onChange={(e) => setForm((c) => ({ ...c, billing_size_cost_3k: e.target.value }))} placeholder="如 6.2" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">4k 进价（CNY / 张）</label>
                      <Input type="number" value={form.billing_size_cost_4k} onChange={(e) => setForm((c) => ({ ...c, billing_size_cost_4k: e.target.value }))} placeholder="如 7.8" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">兜底尺寸进价（CNY）</label>
                      <Input type="number" value={form.billing_default_size_cost} onChange={(e) => setForm((c) => ({ ...c, billing_default_size_cost: e.target.value }))} placeholder="size 不在表中时使用" />
                    </div>
                    <div className="space-y-1 md:col-span-2">
                      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">基础成本（档位留空时生效）</p>
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">基础进价（CNY）</label>
                      <Input type="number" value={form.billing_base_cost} onChange={(e) => setForm((c) => ({ ...c, billing_base_cost: e.target.value }))} placeholder="如 4.2" />
                    </div>
                  </>
                )}

                {(form.billing_type === 'video' || form.billing_type === 'audio') && (
                  <>
                    <div className="space-y-2 md:col-span-2">
                      <label className="text-sm font-medium">利润倍率</label>
                      <p className="text-xs text-muted-foreground">售价 = 成本 × 倍率。例如填 1.2 表示利润 20%。</p>
                      <Input type="number" step="0.01" value={form.billing_markup} onChange={(e) => setForm((c) => ({ ...c, billing_markup: e.target.value }))} placeholder="如 1.2" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">进价（CNY / 秒）</label>
                      <Input type="number" value={form.billing_cost_per_second} onChange={(e) => setForm((c) => ({ ...c, billing_cost_per_second: e.target.value }))} placeholder="如 0.008" />
                    </div>
                  </>
                )}

                {form.billing_type === 'count' && (
                  <>
                    <div className="space-y-2 md:col-span-2">
                      <label className="text-sm font-medium">利润倍率</label>
                      <p className="text-xs text-muted-foreground">售价 = 成本 × 倍率。例如填 1.2 表示利润 20%。</p>
                      <Input type="number" step="0.01" value={form.billing_markup} onChange={(e) => setForm((c) => ({ ...c, billing_markup: e.target.value }))} placeholder="如 1.2" />
                    </div>
                    <div className="space-y-2">
                      <label className="text-sm font-medium">进价（CNY / 次）</label>
                      <Input type="number" value={form.billing_cost_per_call} onChange={(e) => setForm((c) => ({ ...c, billing_cost_per_call: e.target.value }))} placeholder="如 0.0008" />
                    </div>
                  </>
                )}

                {form.billing_type !== 'custom' ? (
                  <div className="space-y-3 md:col-span-2">
                    <div className="flex items-center justify-between gap-3">
                      <div className="space-y-1">
                        <label className="text-sm font-medium">分组定价（pricing_groups）</label>
                        <p className="text-xs text-muted-foreground">按用户分组覆盖默认售价，不影响上游进价。组名必须与用户 group 完全一致，例如 vip、premium。</p>
                      </div>
                      <Button type="button" size="sm" variant="outline" onClick={addPricingGroup}>
                        <PlusIcon data-icon="inline-start" />
                        新增分组
                      </Button>
                    </div>

                    {form.pricing_groups.length === 0 ? (
                      <div className="rounded-lg border border-dashed px-4 py-5 text-xs text-muted-foreground">
                        暂无分组定价。需要 VIP 或其他用户组差异化售价时，点击“新增分组”进行配置。
                      </div>
                    ) : (
                      <div className="flex flex-col gap-4">
                        {form.pricing_groups.map((group, index) => (
                          <Card key={`${group.group || 'group'}-${index}`} size="sm">
                            <CardHeader>
                              <div className="flex items-start justify-between gap-3">
                                <div className="space-y-1">
                                  <CardTitle className="text-base">分组覆盖 #{index + 1}</CardTitle>
                                  <CardDescription>仅覆盖用户侧售价字段，保存后会写入 billing_config.pricing_groups。</CardDescription>
                                </div>
                                <Button type="button" size="sm" variant="outline" onClick={() => removePricingGroup(index)}>
                                  移除
                                </Button>
                              </div>
                            </CardHeader>
                            <CardContent className="grid gap-5 md:grid-cols-2">
                              <div className="space-y-2 md:col-span-2">
                                <label className="text-sm font-medium">用户分组名</label>
                                <Input
                                  value={group.group}
                                  onChange={(event) => updatePricingGroup(index, { group: event.target.value })}
                                  placeholder="如 vip"
                                />
                              </div>

                              {form.billing_type === 'token' ? (
                                <>
                                  <div className="space-y-2">
                                    <label className="text-sm font-medium">输入售价（CNY / 百万 token）</label>
                                    <Input
                                      type="number"
                                      value={group.input_price_per_1m_tokens}
                                      onChange={(event) => updatePricingGroup(index, { input_price_per_1m_tokens: event.target.value })}
                                      placeholder="如 8"
                                    />
                                  </div>
                                  <div className="space-y-2">
                                    <label className="text-sm font-medium">输出售价（CNY / 百万 token）</label>
                                    <Input
                                      type="number"
                                      value={group.output_price_per_1m_tokens}
                                      onChange={(event) => updatePricingGroup(index, { output_price_per_1m_tokens: event.target.value })}
                                      placeholder="如 32"
                                    />
                                  </div>
                                </>
                              ) : null}

                              {form.billing_type === 'image' ? (
                                <>
                                  <div className="space-y-2">
                                    <label className="text-sm font-medium">基础售价（CNY）</label>
                                    <Input
                                      type="number"
                                      value={group.base_price}
                                      onChange={(event) => updatePricingGroup(index, { base_price: event.target.value })}
                                      placeholder="如 5"
                                    />
                                  </div>
                                  <div className="space-y-2">
                                    <label className="text-sm font-medium">兜底尺寸售价（CNY）</label>
                                    <Input
                                      type="number"
                                      value={group.default_size_price}
                                      onChange={(event) => updatePricingGroup(index, { default_size_price: event.target.value })}
                                      placeholder="如 5"
                                    />
                                  </div>
                                  <div className="space-y-2">
                                    <label className="text-sm font-medium">1k 售价（CNY / 张）</label>
                                    <Input
                                      type="number"
                                      value={group.size_price_1k}
                                      onChange={(event) => updatePricingGroup(index, { size_price_1k: event.target.value })}
                                      placeholder="如 3"
                                    />
                                  </div>
                                  <div className="space-y-2">
                                    <label className="text-sm font-medium">2k 售价（CNY / 张）</label>
                                    <Input
                                      type="number"
                                      value={group.size_price_2k}
                                      onChange={(event) => updatePricingGroup(index, { size_price_2k: event.target.value })}
                                      placeholder="如 5"
                                    />
                                  </div>
                                  <div className="space-y-2">
                                    <label className="text-sm font-medium">3k 售价（CNY / 张）</label>
                                    <Input
                                      type="number"
                                      value={group.size_price_3k}
                                      onChange={(event) => updatePricingGroup(index, { size_price_3k: event.target.value })}
                                      placeholder="如 7"
                                    />
                                  </div>
                                  <div className="space-y-2">
                                    <label className="text-sm font-medium">4k 售价（CNY / 张）</label>
                                    <Input
                                      type="number"
                                      value={group.size_price_4k}
                                      onChange={(event) => updatePricingGroup(index, { size_price_4k: event.target.value })}
                                      placeholder="如 9"
                                    />
                                  </div>
                                </>
                              ) : null}

                              {(form.billing_type === 'video' || form.billing_type === 'audio') ? (
                                <div className="space-y-2 md:col-span-2">
                                  <label className="text-sm font-medium">售价（CNY / 秒）</label>
                                  <Input
                                    type="number"
                                    value={group.price_per_second}
                                    onChange={(event) => updatePricingGroup(index, { price_per_second: event.target.value })}
                                    placeholder="如 0.006"
                                  />
                                </div>
                              ) : null}

                              {form.billing_type === 'count' ? (
                                <div className="space-y-2 md:col-span-2">
                                  <label className="text-sm font-medium">售价（CNY / 次）</label>
                                  <Input
                                    type="number"
                                    value={group.price_per_call}
                                    onChange={(event) => updatePricingGroup(index, { price_per_call: event.target.value })}
                                    placeholder="如 0.001"
                                  />
                                </div>
                              ) : null}
                            </CardContent>
                          </Card>
                        ))}
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="rounded-lg border border-dashed px-4 py-5 text-xs text-muted-foreground md:col-span-2">
                    `custom` 计费依赖脚本返回 credits 数值，当前不支持通过 pricing_groups 做可视化分组定价覆盖。
                  </div>
                )}

                <div className="space-y-2 md:col-span-2">
                  <label className="text-sm font-medium">高级配置（JSON）</label>
                  <p className="text-xs text-muted-foreground">用于配置 metric_paths、resolution_tiers 等高级参数；结构化价格字段和分组定价会在保存时覆盖同名键。</p>
                  <Textarea
                    value={form.billing_config_text}
                    onChange={(event) => setForm((current) => ({ ...current, billing_config_text: event.target.value }))}
                    rows={8}
                    className="font-mono text-xs"
                    placeholder={'{\n  "metric_paths": {\n    "input_tokens": "response.usage.prompt_tokens",\n    "output_tokens": "response.usage.completion_tokens"\n  },\n  "resolution_tiers": [\n    { "max_pixels": 1048576, "multiplier": 1.0 }\n  ]\n}'}
                  />
                  <p className="text-xs text-muted-foreground">分组定价已改为上方可视化编辑；这里建议只放 metric_paths、resolution_tiers 等非价格字段。</p>
                </div>

                <div className="space-y-2 md:col-span-2">
                  <label className="text-sm font-medium">自定义计费脚本</label>
                  <p className="text-xs text-muted-foreground">billing_type=custom 时生效，脚本需返回 credits 数值。</p>
                  <Textarea
                    value={form.billing_script}
                    onChange={(event) => setForm((current) => ({ ...current, billing_script: event.target.value }))}
                    rows={8}
                    className="font-mono text-xs"
                    placeholder="function calcBilling(request) { return 1000 }"
                  />
                </div>
              </div>
            </TabsContent>

            {/* ── 脚本 & 轮询 ── */}
            <TabsContent value="scripts" className="mt-5 max-h-[62vh] overflow-y-auto pr-1">
              <div className="grid gap-5">
                <div className="space-y-2">
                  <label className="text-sm font-medium">入参脚本</label>
                  <p className="text-xs text-muted-foreground">mapRequest(input) → 将平台请求映射为上游格式。</p>
                  <Textarea value={form.request_script} onChange={(event) => setForm((current) => ({ ...current, request_script: event.target.value }))} rows={7} className="font-mono text-xs" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">出参脚本</label>
                  <p className="text-xs text-muted-foreground">mapResponse(input) → 映射上游响应，或提取 upstream_task_id（异步）。</p>
                  <Textarea value={form.response_script} onChange={(event) => setForm((current) => ({ ...current, response_script: event.target.value }))} rows={7} className="font-mono text-xs" />
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">错误检测脚本</label>
                  <p className="text-xs text-muted-foreground">checkError(response) → 返回非空字符串表示错误，null/false 表示正常。</p>
                  <Textarea value={form.error_script} onChange={(event) => setForm((current) => ({ ...current, error_script: event.target.value }))} rows={5} className="font-mono text-xs" />
                </div>

                <div className="border-t pt-2">
                  <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">轮询配置（异步任务用）</p>
                </div>

                <div className="space-y-2">
                  <label className="text-sm font-medium">轮询 URL</label>
                  <Input value={form.query_url} onChange={(event) => setForm((current) => ({ ...current, query_url: event.target.value }))} placeholder="如 https://api.example.com/v1/tasks/{id}" />
                </div>
                <div className="grid grid-cols-2 gap-5">
                  <div className="space-y-2">
                    <label className="text-sm font-medium">轮询方法</label>
                    <NativeSelect value={form.query_method} onChange={(event) => setForm((current) => ({ ...current, query_method: event.target.value }))}>
                      <option value="GET">GET</option>
                      <option value="POST">POST</option>
                    </NativeSelect>
                  </div>
                  <div className="space-y-2">
                    <label className="text-sm font-medium">轮询超时（ms）</label>
                    <Input value={form.query_timeout_ms} onChange={(event) => setForm((current) => ({ ...current, query_timeout_ms: event.target.value }))} />
                  </div>
                </div>
                <div className="space-y-2">
                  <label className="text-sm font-medium">轮询脚本</label>
                  <p className="text-xs text-muted-foreground">mapResponse(input) → 将轮询响应映射为标准格式。</p>
                  <Textarea value={form.query_script} onChange={(event) => setForm((current) => ({ ...current, query_script: event.target.value }))} rows={7} className="font-mono text-xs" />
                </div>
              </div>
            </TabsContent>
          </Tabs>

          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>取消</Button>
            <Button onClick={saveChannel} disabled={!form.name.trim() || !form.model.trim() || !form.base_url.trim()}>
              <SaveIcon data-icon="inline-start" />
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={pendingDeleteChannel !== undefined} onOpenChange={() => setPendingDeleteChannel(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确认删除渠道"{pendingDeleteChannel?.name ?? pendingDeleteChannel?.model ?? pendingDeleteChannel?.id}"吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={executeDeleteChannel}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* 批量设权重 Dialog */}
      <Dialog open={batchRateOpen} onOpenChange={setBatchRateOpen}>
        <DialogContent className="w-[min(calc(100vw-2rem),560px)] max-w-none sm:max-w-[560px]">
          <DialogHeader>
            <DialogTitle>批量设置权重</DialogTitle>
            <DialogDescription>将选中 {selectedIds.size} 个渠道的权重设为同一值。</DialogDescription>
          </DialogHeader>
          <Input type="number" min="1" step="1" value={batchRate} onChange={(e) => setBatchRate(e.target.value)} placeholder="权重（正整数）" />
          <DialogFooter>
            <Button variant="outline" onClick={() => setBatchRateOpen(false)}>取消</Button>
            <Button onClick={batchSetRate} disabled={batchMutating}>确认</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 变更日志 Dialog */}
      <Dialog open={logChannel !== null} onOpenChange={() => setLogChannel(null)}>
        <DialogContent className="w-[min(calc(100vw-2rem),960px)] max-w-none sm:max-w-[960px]">
          <DialogHeader>
            <DialogTitle>渠道变更日志</DialogTitle>
            <DialogDescription>{logChannel?.name ?? logChannel?.model ?? `#${logChannel?.id}`}</DialogDescription>
          </DialogHeader>
          {logChannel?.id ? <ChannelLogPanel channelId={logChannel.id} /> : null}
        </DialogContent>
      </Dialog>
    </>
  )
}

function ChannelHealthBadge({ channelId }: { channelId: number }) {
  const { data } = useAsync(async () => {
    return adminApi.getChannelHealth(channelId)
  }, null as import('@/lib/api/admin').AdminChannelHealth | null, [channelId])

  const [open, setOpen] = useState(false)

  if (!data || data.total === 0) return <Badge variant="secondary" className="text-xs">无数据</Badge>

  const rate = data.success_rate ?? 0
  const isHealthy = rate >= 95
  const latency = data.p50_ms != null ? `${data.p50_ms.toFixed(0)}ms` : ''

  return (
    <>
      <button
        type="button"
        onClick={(e) => { e.stopPropagation(); setOpen(true) }}
        className="focus:outline-none"
        title="点击查看健康详情"
      >
        <Badge variant={isHealthy ? 'default' : 'destructive'} className="cursor-pointer text-xs hover:opacity-80">
          {rate.toFixed(0)}%{latency ? ` P50 ${latency}` : ''}
        </Badge>
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="w-[min(calc(100vw-2rem),800px)] max-w-none sm:max-w-[800px]">
          <DialogHeader>
            <DialogTitle>渠道 #{channelId} 健康状态（近 24h）</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid grid-cols-1 gap-3 text-center sm:grid-cols-3">
              <div className="rounded-lg border p-3">
                <p className="text-xs text-muted-foreground">成功率</p>
                <p className={`text-lg font-semibold ${isHealthy ? 'text-emerald-600' : 'text-destructive'}`}>
                  {rate.toFixed(1)}%
                </p>
                <p className="text-xs text-muted-foreground">{data.ok}/{data.total}</p>
              </div>
              <div className="rounded-lg border p-3">
                <p className="text-xs text-muted-foreground">P50 延迟</p>
                <p className="text-lg font-semibold">
                  {data.p50_ms != null ? `${data.p50_ms.toFixed(0)}ms` : '-'}
                </p>
              </div>
              <div className="rounded-lg border p-3">
                <p className="text-xs text-muted-foreground">P99 延迟</p>
                <p className="text-lg font-semibold">
                  {data.p99_ms != null ? `${data.p99_ms.toFixed(0)}ms` : '-'}
                </p>
              </div>
            </div>
            {data.top_errors && data.top_errors.length > 0 ? (
              <div>
                <p className="mb-2 text-sm font-medium">失败原因 TOP{data.top_errors.length}</p>
                <div className="space-y-1.5">
                  {data.top_errors.map((e, i) => (
                    <div key={i} className="flex items-start justify-between gap-3 rounded-md bg-muted/40 px-3 py-2 text-xs">
                      <span className="min-w-0 flex-1 truncate font-mono text-destructive/80" title={e.msg}>{e.msg || '(空)'}</span>
                      <span className="shrink-0 font-semibold text-muted-foreground">{e.count} 次</span>
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">近 24h 无失败记录</p>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

function ChannelLogPanel({ channelId }: { channelId: number }) {
  const { data: logs, loading } = useAsync(async () => {
    const res = await adminApi.listChannelLogs(channelId)
    return (Array.isArray(res) ? res : (res as { logs?: AdminChannelLog[] }).logs ?? []) as AdminChannelLog[]
  }, [] as AdminChannelLog[], [channelId])

  if (loading) return <div className="py-6 text-sm text-muted-foreground text-center">加载中…</div>

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-40">时间</TableHead>
          <TableHead>操作人</TableHead>
          <TableHead>字段</TableHead>
          <TableHead>前值</TableHead>
          <TableHead>后值</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {logs.length === 0 ? (
          <TableRow>
            <TableCell colSpan={5} className="py-6 text-center text-sm text-muted-foreground">暂无变更记录</TableCell>
          </TableRow>
        ) : logs.map((log, i) => (
          <TableRow key={log.id ?? i}>
            <TableCell className="text-sm text-muted-foreground">
              {log.created_at ? new Date(log.created_at).toLocaleString('zh-CN') : '-'}
            </TableCell>
            <TableCell className="text-sm">{log.admin_id ? `#${log.admin_id}` : '-'}</TableCell>
            <TableCell className="font-mono text-xs">{log.field ?? '-'}</TableCell>
            <TableCell className="font-mono text-xs text-muted-foreground truncate max-w-xs">{log.old_val ?? '-'}</TableCell>
            <TableCell className="font-mono text-xs truncate max-w-xs">{log.new_val ?? '-'}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
