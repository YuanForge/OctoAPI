import { createHttpClient } from '@/lib/api/http'
import { uploadAuthedImage, type UploadImageCategory } from '@/lib/api/upload'

const http = createHttpClient('admin')

export type AdminLoginResponse = {
  token: string
}

export type AdminStatsResponse = {
  // flat fields (fallback)
  total_users?: number
  users?: number
  total_requests?: number
  requests?: number
  total_revenue?: number
  revenue?: number
  // structured fields (preferred)
  channels?: number
  active_channels?: number
  today?: {
    revenue?: number
    cost?: number
    profit?: number
    count?: number
  }
  total?: {
    revenue?: number
    cost?: number
    profit?: number
  }
}

export type AdminChannel = {
  id?: number
  name?: string
  model?: string
  routing_model?: string
  display_name?: string
  model_provider?: string
  type?: string
  protocol?: string
  base_url?: string
  method?: string
  query_url?: string
  query_method?: string
  timeout_ms?: number
  query_timeout_ms?: number
  billing_type?: string
  headers?: Record<string, unknown>
  billing_config?: Record<string, unknown>
  billing_script?: string
  request_script?: string
  response_script?: string
  query_script?: string
  error_script?: string
  key_pool_id?: number
  auth_type?: string
  auth_param_name?: string
  auth_region?: string
  auth_service?: string
  passthrough_headers?: boolean
  passthrough_body?: boolean
  weight?: number
  priority?: number
  icon_url?: string
  description?: string
  is_active?: boolean
  groups?: string[]
}

export type AdminUser = {
  id?: number
  username?: string
  email?: string
  role?: string
  group?: string
  balance_credits?: number
  balance?: number
  is_active?: boolean
  frozen_reason?: string
  rebate_ratio?: number | null
  created_at?: string
  invite_count?: number
  total_spent?: number
}

export type AdminReferralUser = {
  id?: number
  username?: string
  email?: string
  created_at?: string
}

export type AdminUserReferrals = {
  inviter?: AdminReferralUser | null
  inviter_id?: number | null
  invitees?: AdminReferralUser[]
  invitee_count?: number
}

export type AdminTransaction = {
  id?: number
  user_id?: number
  created_at?: string
  type?: string
  amount?: number
  credits?: number
  model_credit_charged?: number
  cost?: number
  profit?: number
  channel_id?: number
  corr_id?: string
  llm_log_id?: number
  task_id?: number
  remark?: string
  description?: string
}

export type AdminTransactionSummary = {
  revenue?: number
  cost?: number
  profit?: number
  transaction_count?: number
}

export type AdminTask = {
  id?: number
  user_id?: number
  channel_id?: number
  type?: string
  status?: string
  error_msg?: string
  credits_charged?: number
  upstream_task_id?: string
  corr_id?: string
  request?: Record<string, unknown>
  upstream_request?: Record<string, unknown>
  upstream_response?: Record<string, unknown>
  result?: Record<string, unknown>
  created_at?: string
  updated_at?: string
}

export type AdminCleanupTarget = 'tasks' | 'llm_logs'

export type AdminCleanupBase = {
  target: AdminCleanupTarget
  target_label: string
  retention_days: number
  cutoff: string
  statuses: string[]
}

export type AdminCleanupPreview = AdminCleanupBase & {
  count: number
}

export type AdminCleanupRunResult = AdminCleanupBase & {
  ok: boolean
  matched: number
  deleted: number
  remaining: number
  has_more: boolean
}

export type AdminLog = {
  id?: number
  user_id?: number
  username?: string
  channel_id?: number
  api_key_id?: number
  upstream_api_key?: string
  model?: string
  created_at?: string
  updated_at?: string
  corr_id?: string
  credits_charged?: number
  cost_charged?: number
  status?: string
  is_stream?: boolean
  transport?: string
  error_msg?: string
  upstream_status?: number
  upstream_method?: string
  upstream_url?: string
  usage?: {
    prompt_tokens?: number
    completion_tokens?: number
    total_tokens?: number
    cache_creation_tokens?: number
    cache_read_tokens?: number
    estimated?: boolean
  }
  client_request?: Record<string, unknown>
  client_response?: Record<string, unknown>
  upstream_headers?: Record<string, unknown>
  upstream_request?: Record<string, unknown>
  upstream_response?: Record<string, unknown>
}

export type AdminVendor = {
  id?: number
  name?: string
  username?: string
  email?: string
  invite_code?: string
  is_active?: boolean
  enabled?: boolean
  commission_ratio?: number
  fee_ratio?: number
  balance?: number
  balance_credits?: number
  created_at?: string
}

export type AdminCard = {
  id?: number
  code?: string
  credits?: number
  status?: string
  note?: string
  batch_id?: string
  vendor_id?: number | null
  used_by?: number
  used_at?: string
  created_at?: string
}

export type AdminCardBatch = {
  id?: number
  batch_id?: string
  note?: string
  credits?: number
  count?: number
  used?: number
  created_at?: string
}

export type AdminWithdrawal = {
  id?: number
  username?: string
  created_at?: string
  amount?: number
  payment_type?: string
  payment_qr?: string
  status?: string
  review_stage?: string
  cs_reviewer_id?: number
  cs_reviewed_at?: string
  finance_reviewer_id?: number
  finance_reviewed_at?: string
  admin_remark?: string
  proof_url?: string
  proof_note?: string
}

export type AdminKeyPool = {
  id?: number
  name?: string
  channel_id?: number
  is_active?: boolean
  vendor_submittable?: boolean
}

export type AdminPoolKey = {
  id?: number
  pool_id?: number
  vendor_id?: number | null
  value?: string
  base_url_override?: string
  priority?: number
  is_active?: boolean
  last_used_at?: string | null
  fail_rate?: number
  total_calls?: number
  balance?: number | null
}

export type AdminKeyPoolSyncResult = {
  pool_id?: number
  platform_id?: number
  group?: string
  listed?: number
  imported?: number
  reactivated?: number
  skipped?: number
  created_upstream?: number
  skipped_by_lock?: boolean
}

export type AdminOcpcPlatform = {
  id?: number
  platform?: string
  name?: string
  enabled?: boolean
  baidu_page_url?: string
  baidu_token?: string
  baidu_reg_type?: number
  baidu_order_type?: number
  e360_key?: string
  e360_secret?: string
  e360_jzqs?: string
  e360_so_type?: string
  e360_reg_event?: string
  e360_order_event?: string
}

export type AdminTrendPoint = {
  label: string
  value: number
}

export type AdminTopEntry = {
  id: string
  name: string
  value: number
}

export type AdminTopStats = {
  users: AdminTopEntry[]
  models: AdminTopEntry[]
  channels: AdminTopEntry[]
}

export type AdminChannelHealth = {
  total: number
  ok: number
  success_rate: number
  p50_ms: number
  p99_ms: number
  top_errors: { msg: string; count: number }[]
}

export type AdminChannelLog = {
  id?: number
  channel_id?: number
  admin_id?: number
  field?: string
  old_val?: string
  new_val?: string
  created_at?: string
}

export type AdminAuditLog = {
  id?: number
  admin_id?: number
  admin_email?: string
  action?: string
  resource_type?: string
  resource_id?: number
  summary?: string
  detail?: Record<string, unknown>
  ip?: string
  ua?: string
  created_at?: string
}

export type AdminNotification = {
  id?: number
  title?: string
  content?: string
  target_type?: string
  target_value?: string
  status?: string
  created_by?: number
  send_at?: string
  sent_at?: string
  created_at?: string
}

export type AdminAlert = {
  id?: number
  type?: string
  resource_type?: string
  resource_id?: number
  message?: string
  status?: string
  acked_by?: number
  acked_at?: string
  resolved_at?: string
  detail?: Record<string, unknown>
  created_at?: string
}

export type AdminExportTask = {
  id?: number
  name?: string
  type?: string
  params?: Record<string, unknown>
  status?: string
  progress?: number
  file_url?: string
  file_size?: number
  error_msg?: string
  expires_at?: string
  created_at?: string
}

export type AdminUpstreamPlatform = {
  id?: number
  name?: string
  platform_type?: string
  base_url?: string
  upstream_user_id?: string
  upstream_group?: string
  balance?: number
  balance_amount?: number
  balance_currency?: string
  balance_synced_at?: string
  balance_alert_threshold?: number
  balance_alert_notified?: boolean
  balance_alert_notified_at?: string
  is_active?: boolean
  has_api_key?: boolean
  has_system_token?: boolean
  note?: string
  created_at?: string
}

export type AdminUpstreamChannelSyncResult = {
  bound?: number
  created?: number
  updated?: number
  skipped?: number
  price_synced?: number
  price_unavailable?: number
}

export type AdminUpstreamChannelBindingCandidate = {
  channel_id: number
  name?: string
  model?: string
  display_name?: string
  base_url?: string
  protocol?: string
  is_active?: boolean
  existing_platform_id?: number
  match_reasons?: string[]
  price_available?: boolean
  price_will_update?: boolean
}

export type AdminUpstreamChannelBindingPreview = {
  candidates?: AdminUpstreamChannelBindingCandidate[]
  total?: number
  bindable?: number
  price_available?: number
  price_unavailable?: number
}

export type AdminChannelUpstreamCostPreview = {
  platform?: AdminUpstreamPlatform
  model?: string
  upstream_model?: string
  found?: boolean
  base_url_match?: boolean
  price_available?: boolean
  price_unavailable?: boolean
  billing_type?: string
  protocol?: string
  billing_config?: Record<string, unknown>
}

export type AdminChannelUpstreamCostResult = AdminChannelUpstreamCostPreview & {
  updated?: number
  price_synced?: number
  channel?: AdminChannel
}

export type AdminRole = {
  id?: number
  name?: string
  label?: string
  permissions?: string[]
  is_builtin?: boolean
  created_at?: string
}

export type AdminAdminUser = {
  id?: number
  username?: string
  email?: string | null
  role_ids?: number[]
  role_names?: string[]
}

export type AdminMe = {
  user_id?: number
  username?: string
  email?: string
  role?: string
  permissions?: string[]
}

export type AdminCoupon = {
  id?: number
  code?: string
  type?: string
  title?: string
  discount_type?: string
  discount_value?: number
  min_amount?: number
  max_discount?: number
  total_count?: number
  used_count?: number
  per_user_limit?: number
  valid_from?: string
  valid_until?: string
  created_at?: string
}

export type AdminPaymentOrder = {
  id?: number
  user_id?: number
  user_email?: string
  out_trade_no?: string
  amount?: number
  credits?: number
  status?: string
  trade_no?: string
  pay_flat?: number
  pay_channel?: string
  pay_from?: string
  created_at?: string
  paid_at?: string
}

export type AdminRiskLabel = {
  id?: number
  user_id?: number
  label?: string
  reason?: string
  created_by?: number
  created_at?: string
}

export type AdminAPIKey = {
  id?: number
  user_id?: number
  user_email?: string
  name?: string
  key_type?: string
  is_active?: boolean
  last_used_at?: string
  created_at?: string
}

export type AdminUserPortrait = {
  daily_spend: { day: string; amount: number }[]
  top_models: { model: string; calls: number }[]
  api_keys: AdminAPIKey[]
  risk_labels: AdminRiskLabel[]
}

export const adminAuthApi = {
  login: (payload: { username: string; password: string }) =>
    http.post<AdminLoginResponse>('/auth/login', payload),
}

export const adminApi = {
  getStats: () => http.get<AdminStatsResponse>('/admin/stats'),
  getStatsTrend: (days: 7 | 30, dim: 'revenue' | 'cost' | 'profit' | 'calls') =>
    http.get<{ points: AdminTrendPoint[]; dim: string; days: number }>('/admin/stats/trend', { params: { days, dim } }),
  getStatsTop: () => http.get<AdminTopStats>('/admin/stats/top'),
  listChannels: (params: Record<string, unknown> = {}) =>
    http.get<{ channels?: AdminChannel[]; items?: AdminChannel[]; total?: number; page?: number; size?: number } | AdminChannel[]>(
      '/admin/channels',
      { params }
    ),
  createChannel: (payload: Partial<AdminChannel>) =>
    http.post<AdminChannel>('/admin/channels', payload),
  updateChannel: (id: number, payload: Partial<AdminChannel>) =>
    http.put<AdminChannel>(`/admin/channels/${id}`, payload),
  toggleChannel: (id: number, isActive: boolean) =>
    http.patch<Record<string, unknown>>(`/admin/channels/${id}/active`, {
      is_active: isActive,
    }),
  deleteChannel: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/channels/${id}`),
  previewChannelUpstreamCost: (id: number, params: { platform_id: number; model?: string; group?: string; markup?: number }) =>
    http.get<AdminChannelUpstreamCostPreview>(`/admin/channels/${id}/upstream-cost`, { params }),
  syncChannelUpstreamCost: (id: number, payload: { platform_id: number; model?: string; group?: string; markup?: number }) =>
    http.post<AdminChannelUpstreamCostResult>(`/admin/channels/${id}/sync-upstream-cost`, payload),
  listUsers: (page = 1, size = 20, filters: Record<string, string> = {}) =>
    http.get<{ items?: AdminUser[]; users?: AdminUser[]; total?: number } | AdminUser[]>(
      '/admin/users',
      { params: { page, size, ...filters } }
    ),
  batchUpdateUsers: (payload: { action: 'freeze' | 'unfreeze' | 'set_group'; ids: number[]; group?: string; reason?: string }) =>
    http.post<{ message: string; count: number }>('/admin/users/batch', payload),
  rechargeUser: (id: number, amount: number) =>
    http.post<Record<string, unknown>>(`/admin/users/${id}/recharge`, { amount }),
  grantModelCredit: (id: number, payload: { model_name: string; credits: number }) =>
    http.post<Record<string, unknown>>(`/admin/users/${id}/model-credits`, payload),
  listModelCredits: (id: number) =>
    http.get<{ model_credits?: Array<{ id?: number; model_name?: string; credits?: number }> }>(`/admin/users/${id}/model-credits`),
  resetUserPassword: (id: number, password: string) =>
    http.put<Record<string, unknown>>(`/admin/users/${id}/password`, { password }),
  setUserGroup: (id: number, group: string) =>
    http.put<Record<string, unknown>>(`/admin/users/${id}/group`, { group }),
  setUserRole: (id: number, role: string) =>
    http.put<Record<string, unknown>>(`/admin/users/${id}/role`, { role }),
  freezeUser: (id: number, freeze: boolean, reason?: string) =>
    http.patch<Record<string, unknown>>(`/admin/users/${id}/freeze`, { freeze, reason }),
  createUser: (payload: { username: string; email: string; password: string; role?: string }) =>
    http.post<{ id?: number; username?: string; email?: string }>('/admin/users', payload),
  deleteUser: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/users/${id}`),
  listTransactions: (params: Record<string, unknown> = {}) =>
    http.get<{ items?: AdminTransaction[]; transactions?: AdminTransaction[]; total?: number; summary?: AdminTransactionSummary } | AdminTransaction[]>(
      '/admin/transactions',
      { params }
    ),
  listTasks: (params: Record<string, unknown> = {}) =>
    http.get<{ items?: AdminTask[]; tasks?: AdminTask[]; total?: number } | AdminTask[]>(
      '/admin/tasks',
      { params }
    ),
  getAdminTask: (id: number) =>
    http.get<{ task?: AdminTask } | AdminTask>(`/admin/tasks/${id}`),
  previewCleanup: (params: { target: AdminCleanupTarget; retention_days: number }) =>
    http.get<AdminCleanupPreview>('/admin/cleanup/preview', { params }),
  runCleanup: (payload: { target: AdminCleanupTarget; retention_days: number; confirm: string }) =>
    http.post<AdminCleanupRunResult>('/admin/cleanup/run', payload),
  listLogs: (params: Record<string, unknown> = {}) =>
    http.get<{ logs?: AdminLog[]; items?: AdminLog[]; total?: number }>('/admin/llm-logs', { params }),
  getLog: (id: number) =>
    http.get<AdminLog>(`/admin/llm-logs/${id}`),
  getSettings: () =>
    http.get<{ settings?: Record<string, string> } | Record<string, string>>(
      '/admin/settings'
    ),
  updateSettings: (payload: Record<string, string>) =>
    http.put<Record<string, unknown>>('/admin/settings', payload),
  getAdminMe: () =>
    http.get<AdminMe>('/admin/me'),
  listVendors: (params: Record<string, unknown> = {}) =>
    http.get<{ items?: AdminVendor[]; vendors?: AdminVendor[] } | AdminVendor[]>(
      '/admin/vendors',
      { params }
    ),
  updateVendor: (id: number, payload: { is_active?: boolean; commission_ratio?: number }) =>
    http.patch<Record<string, unknown>>(`/admin/vendors/${id}`, payload),
  listKeyPools: (channelId?: number) =>
    http.get<{ pools?: AdminKeyPool[] } | AdminKeyPool[]>('/admin/key-pools', {
      params: channelId ? { channel_id: channelId } : undefined,
    }),
  createKeyPool: (payload: { channel_id: number; name: string }) =>
    http.post<Record<string, unknown>>('/admin/key-pools', payload),
  deleteKeyPool: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/key-pools/${id}`),
  toggleKeyPool: (id: number) =>
    http.patch<Record<string, unknown>>(`/admin/key-pools/${id}/toggle`, {}),
  toggleVendorSubmittable: (id: number) =>
    http.patch<Record<string, unknown>>(`/admin/key-pools/${id}/vendor-toggle`, {}),
  listPoolKeys: (poolId: number) =>
    http.get<{ keys?: AdminPoolKey[] } | AdminPoolKey[]>(`/admin/key-pools/${poolId}/keys`),
  addPoolKey: (poolId: number, payload: { value: string; priority: number; base_url_override?: string }) =>
    http.post<Record<string, unknown>>(`/admin/key-pools/${poolId}/keys`, payload),
  importPoolKeys: (poolId: number, keys: string[]) =>
    http.post<{ imported: number; skipped: number }>(`/admin/key-pools/${poolId}/keys/import`, { keys }),
  syncKeyPoolFromUpstream: (poolId: number) =>
    http.post<AdminKeyPoolSyncResult>(`/admin/key-pools/${poolId}/sync-upstream`, {}),
  getKeyPoolChannels: (id: number) =>
    http.get<{ channels?: AdminChannel[] }>(`/admin/key-pools/${id}/channels`),
  removePoolKey: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/pool-keys/${id}`),
  updatePoolKey: (id: number, payload: { priority: number; is_active: boolean; base_url_override?: string }) =>
    http.patch<Record<string, unknown>>(`/admin/pool-keys/${id}`, payload),
  setPoolKeyVendor: (id: number, vendorId: number | null) =>
    http.patch<Record<string, unknown>>(`/admin/pool-keys/${id}/vendor`, { vendor_id: vendorId }),
  setUserRebateRatio: (id: number, ratio: number | null) =>
    http.put<Record<string, unknown>>(`/admin/users/${id}/rebate-ratio`, { rebate_ratio: ratio }),
  triggerOcpcUpload: () =>
    http.post<Record<string, unknown>>('/admin/ocpc/upload', {}),
  getOcpcSchedule: () =>
    http.get<{ schedule?: Record<string, string> }>('/admin/ocpc/schedule'),
  updateOcpcSchedule: (payload: { enabled: boolean; interval: number }) =>
    http.put<Record<string, unknown>>('/admin/ocpc/schedule', payload),
  listOcpcPlatforms: () =>
    http.get<{ list?: AdminOcpcPlatform[] } | AdminOcpcPlatform[]>('/admin/ocpc/platforms'),
  createOcpcPlatform: (payload: Record<string, unknown>) =>
    http.post<Record<string, unknown>>('/admin/ocpc/platforms', payload),
  updateOcpcPlatform: (id: number, payload: Record<string, unknown>) =>
    http.put<Record<string, unknown>>(`/admin/ocpc/platforms/${id}`, payload),
  deleteOcpcPlatform: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/ocpc/platforms/${id}`),
  toggleOcpcPlatform: (id: number) =>
    http.patch<Record<string, unknown>>(`/admin/ocpc/platforms/${id}/toggle`, {}),
  generateCards: (payload: { count: number; credits: number; note: string; vendor_id?: number | null }) =>
    http.post<{ cards?: AdminCard[] }>('/admin/cards/generate', payload),
  listCards: (params: Record<string, unknown> = {}) =>
    http.get<{ cards?: AdminCard[]; total?: number }>('/admin/cards', { params }),
  deleteCard: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/cards/${id}`),
  listWithdrawals: (params: Record<string, unknown> = {}) =>
    http.get<{ records?: AdminWithdrawal[]; total?: number }>('/admin/withdrawals', {
      params,
    }),
  getPendingWithdrawCount: () =>
    http.get<{ count?: number }>('/admin/withdrawals/pending-count'),
  approveWithdrawal: (id: number, remark = '') =>
    http.post<Record<string, unknown>>(`/admin/withdrawals/${id}/approve`, { remark }),
  csApproveWithdrawal: (id: number) =>
    http.post<Record<string, unknown>>(`/admin/withdrawals/${id}/cs-approve`, {}),
  rejectWithdrawal: (id: number, remark = '') =>
    http.post<Record<string, unknown>>(`/admin/withdrawals/${id}/reject`, { remark }),
  uploadWithdrawalProof: (id: number, proof_url: string, proof_note: string) =>
    http.post<Record<string, unknown>>(`/admin/withdrawals/${id}/proof`, { proof_url, proof_note }),
  // 渠道扩展
  batchUpdateChannels: (payload: { action: 'toggle_active' | 'set_rate'; ids: number[]; is_active?: boolean; rate_ratio?: number }) =>
    http.post<{ ok: boolean; count: number }>('/admin/channels/batch', payload),
  getChannelHealth: (id: number) =>
    http.get<AdminChannelHealth>(`/admin/channels/${id}/health`),
  listChannelLogs: (id: number) =>
    http.get<{ logs: AdminChannelLog[] }>(`/admin/channels/${id}/logs`),
  // 用户扩展
  getUserReferrals: (id: number) =>
    http.get<AdminUserReferrals>(`/admin/users/${id}/referrals`),
  getUserPortrait: (id: number) =>
    http.get<AdminUserPortrait>(`/admin/users/${id}/portrait`),
  getUserOperationLog: (id: number) =>
    http.get<{ transactions: unknown[]; audits: AdminAuditLog[] }>(`/admin/users/${id}/operation-log`),
  addRiskLabel: (userId: number, payload: { label: string; reason: string }) =>
    http.post<AdminRiskLabel>(`/admin/users/${userId}/risk-labels`, payload),
  deleteRiskLabel: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/risk-labels/${id}`),
  // API Key 总览
  listApiKeys: (params: Record<string, unknown> = {}) =>
    http.get<{ keys: AdminAPIKey[]; total: number }>('/admin/api-keys', { params }),
  revokeApiKey: (id: number) =>
    http.patch<Record<string, unknown>>(`/admin/api-keys/${id}/revoke`, {}),
  // 账单扩展
  getTransactionAggregate: (params: Record<string, unknown> = {}) =>
    http.get<{ rows: { key: string; revenue: number; cost: number; profit: number; calls: number }[]; dim: string }>('/admin/transactions/aggregate', { params }),
  adjustTransaction: (payload: { user_id: number; type: string; credits: number; reason: string }) =>
    http.post<{ ok: boolean; balance_after: number; transaction_id: number }>('/admin/transactions/adjust', payload),
  // 卡密批次
  listCardBatches: () =>
    http.get<{ batches: AdminCardBatch[] }>('/admin/cards/batches'),
  voidCard: (id: number) =>
    http.post<Record<string, unknown>>(`/admin/cards/${id}/void`, {}),
  voidCardBatch: (batchId: string) =>
    http.post<{ ok: boolean; voided: number }>(`/admin/cards/batches/${batchId}/void`, {}),
  // 审计日志
  listAuditLogs: (params: Record<string, unknown> = {}) =>
    http.get<{ logs: AdminAuditLog[]; total: number }>('/admin/audit', { params }),
  // 通知中心
  listNotifications: (params: Record<string, unknown> = {}) =>
    http.get<{ notifications: AdminNotification[]; total: number }>('/admin/notifications', { params }),
  createNotification: (payload: Partial<AdminNotification>) =>
    http.post<AdminNotification>('/admin/notifications', payload),
  sendNotification: (id: number) =>
    http.post<Record<string, unknown>>(`/admin/notifications/${id}/send`, {}),
  deleteNotification: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/notifications/${id}`),
  // 告警中心
  listAlerts: (params: Record<string, unknown> = {}) =>
    http.get<{ alerts: AdminAlert[]; total: number }>('/admin/alerts', { params }),
  ackAlert: (id: number) =>
    http.patch<Record<string, unknown>>(`/admin/alerts/${id}/ack`, {}),
  resolveAlert: (id: number) =>
    http.patch<Record<string, unknown>>(`/admin/alerts/${id}/resolve`, {}),
  // 数据导出中心
  listExportTasks: () =>
    http.get<{ tasks: AdminExportTask[] }>('/admin/exports'),
  createExportTask: (payload: { name: string; type: string; params: Record<string, unknown> }) =>
    http.post<AdminExportTask>('/admin/exports', payload),
  // 上游平台
  listUpstreamPlatforms: () =>
    http.get<{ platforms: AdminUpstreamPlatform[] }>('/admin/upstream-platforms'),
  createUpstreamPlatform: (payload: Partial<AdminUpstreamPlatform> & { api_key?: string; system_token?: string }) =>
    http.post<AdminUpstreamPlatform>('/admin/upstream-platforms', payload),
  updateUpstreamPlatform: (id: number, payload: Partial<AdminUpstreamPlatform> & { api_key?: string; system_token?: string }) =>
    http.put<Record<string, unknown>>(`/admin/upstream-platforms/${id}`, payload),
  deleteUpstreamPlatform: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/upstream-platforms/${id}`),
  getUpstreamModels: (id: number) =>
    http.get<{ models: string[]; items?: Array<{ id: string; billing_type?: string; protocol?: string; billing_config?: Record<string, unknown> }> }>(`/admin/upstream-platforms/${id}/models`),
  syncUpstreamBalance: (id: number) =>
    http.post<{ platform?: AdminUpstreamPlatform; balance?: number; currency?: string; used_amount?: number }>(`/admin/upstream-platforms/${id}/sync-balance`, {}),
  createUpstreamApiKey: (id: number, payload: { name?: string; group?: string; remain_quota?: number; unlimited_quota?: boolean; expired_time?: number; model_limits_enabled?: boolean; model_limits?: string; save_to_platform?: boolean }) =>
    http.post<{ api_key: string; saved?: boolean }>(`/admin/upstream-platforms/${id}/api-keys`, payload),
  syncUpstreamChannels: (id: number, models: string[], markup = 1) =>
    http.post<AdminUpstreamChannelSyncResult>(`/admin/upstream-platforms/${id}/sync-channels`, { models, markup }),
  batchCreateChannelsFromUpstream: (platformId: number, models: string[], markup = 1) =>
    http.post<AdminUpstreamChannelSyncResult>('/admin/channels/batch-from-upstream', { platform_id: platformId, models, markup }),
  previewUpstreamChannelBindings: (id: number, markup = 1) =>
    http.get<AdminUpstreamChannelBindingPreview>(`/admin/upstream-platforms/${id}/channel-bindings/preview`, { params: { markup } }),
  bindUpstreamChannels: (id: number, channelIds: number[], markup = 1, updatePrice = true) =>
    http.post<AdminUpstreamChannelSyncResult>(`/admin/upstream-platforms/${id}/bind-channels`, {
      channel_ids: channelIds,
      markup,
      update_price: updatePrice,
    }),
  // RBAC
  listRoles: () =>
    http.get<{ roles: AdminRole[] }>('/admin/roles'),
  createRole: (payload: { name: string; label: string; permissions: string[] }) =>
    http.post<AdminRole>('/admin/roles', payload),
  updateRole: (id: number, payload: { label?: string; permissions?: string[] }) =>
    http.put<Record<string, unknown>>(`/admin/roles/${id}`, payload),
  deleteRole: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/roles/${id}`),
  // 优惠券
  listCoupons: (params: Record<string, unknown> = {}) =>
    http.get<{ coupons: AdminCoupon[]; total: number }>('/admin/coupons', { params }),
  createCoupon: (payload: Partial<AdminCoupon>) =>
    http.post<AdminCoupon>('/admin/coupons', payload),
  voidCoupon: (id: number) =>
    http.delete<Record<string, unknown>>(`/admin/coupons/${id}`),
  listCouponUses: (id: number) =>
    http.get<{ uses: { id?: number; coupon_id?: number; user_id?: number; discount?: number; created_at?: string }[] }>(`/admin/coupons/${id}/uses`),
  // 客户充值明细
  listPaymentOrders: (params: Record<string, unknown> = {}) =>
    http.get<{ orders: AdminPaymentOrder[]; total: number }>('/admin/payments', { params }),
  // 系统设置操作日志
  listSettingLogs: () =>
    http.get<{ logs: AdminAuditLog[] }>('/admin/settings/logs'),
  verifyAdminPassword: (password: string) =>
    http.post<{ ok: boolean }>('/admin/verify-password', { password }),
  // 管理员账号 & 角色分配
  listAdminUsers: () =>
    http.get<{ admins: AdminAdminUser[] }>('/admin/admins'),
  setAdminRoles: (id: number, roleIds: number[]) =>
    http.put<{ ok: boolean }>(`/admin/admins/${id}/roles`, { role_ids: roleIds }),
  uploadImage: (file: File, category: UploadImageCategory) =>
    uploadAuthedImage('admin', file, category),
}
