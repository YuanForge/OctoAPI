import { useEffect, useState } from 'react'
import { PlusIcon, Pencil1Icon, TrashIcon } from '@radix-ui/react-icons'
import { Loader2, RefreshCwIcon, ServerIcon } from 'lucide-react'

import { PageHeader } from '@/components/shared/PageHeader'
import { TableEmpty } from '@/components/shared/TableEmpty'
import { TableSkeleton } from '@/components/shared/TableSkeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
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
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { adminApi, type AdminUpstreamPlatform } from '@/lib/api/admin'
import { useAsync } from '@/hooks/use-async'
import { toast } from 'sonner'

type PlatformForm = {
  name: string
  platform_type: string
  base_url: string
  api_key: string
  system_token: string
  upstream_user_id: string
  balance_alert_threshold: string
  note: string
  is_active: boolean
}

const defaultForm: PlatformForm = {
  name: 'zzshu',
  platform_type: 'newapi',
  base_url: 'https://us.zzshu.cc',
  api_key: '',
  system_token: '',
  upstream_user_id: '',
  balance_alert_threshold: '',
  note: '',
  is_active: true,
}

function formatBalance(p: AdminUpstreamPlatform) {
  const currency = p.balance_currency || 'CNY'
  if (p.balance_synced_at && p.balance_amount != null) {
    if (currency === 'CNY') return `¥${p.balance_amount.toFixed(4)}`
    return `${currency} ${p.balance_amount.toFixed(4)}`
  }
  if (p.balance != null && p.balance > 0) return `¥${(p.balance / 1_000_000).toFixed(4)}`
  return '-'
}

function platformLabel(type?: string) {
  if (type === 'newapi') return 'New API'
  if (type === 'sub2api') return 'Sub2API'
  return 'OpenAI'
}

function supportsBalance(type?: string) {
  return type === 'newapi' || type === 'sub2api'
}

function formatAlertThreshold(p: AdminUpstreamPlatform) {
  const threshold = p.balance_alert_threshold ?? 0
  if (!supportsBalance(p.platform_type) || threshold <= 0) return '关闭'
  const currency = p.balance_currency || 'CNY'
  return `${currency} <= ${threshold.toFixed(4)}`
}

export function AdminUpstreamPage() {
  const { data: platforms, loading, error, reload } = useAsync(async () => {
    const res = await adminApi.listUpstreamPlatforms()
    return res.platforms ?? []
  }, [] as AdminUpstreamPlatform[], [])

  const [mutError, setMutError] = useState('')
  const [editing, setEditing] = useState<AdminUpstreamPlatform | null>(null)
  const [form, setForm] = useState<PlatformForm>(defaultForm)
  const [syncingId, setSyncingId] = useState<number | null>(null)

  useEffect(() => {
    const timer = window.setInterval(() => reload(), 10_000)
    return () => window.clearInterval(timer)
  }, [reload])

  function openCreate() {
    setEditing({})
    setForm(defaultForm)
    setMutError('')
  }

  function openEdit(p: AdminUpstreamPlatform) {
    setEditing(p)
    setForm({
      name: p.name ?? '',
      platform_type: p.platform_type ?? 'openai',
      base_url: p.base_url ?? '',
      api_key: '',
      system_token: '',
      upstream_user_id: p.upstream_user_id ?? '',
      balance_alert_threshold: p.balance_alert_threshold && p.balance_alert_threshold > 0 ? String(p.balance_alert_threshold) : '',
      note: p.note ?? '',
      is_active: p.is_active ?? true,
    })
    setMutError('')
  }

  async function handleSave() {
    setMutError('')
    const threshold = Number.parseFloat(form.balance_alert_threshold)
    const payload = {
      name: form.name.trim(),
      platform_type: form.platform_type,
      base_url: form.base_url.trim(),
      upstream_user_id: form.upstream_user_id.trim(),
      balance_alert_threshold: Number.isFinite(threshold) && threshold > 0 ? threshold : 0,
      note: form.note.trim(),
      is_active: form.is_active,
      ...(form.api_key.trim() ? { api_key: form.api_key.trim() } : {}),
      ...(form.system_token.trim() ? { system_token: form.system_token.trim() } : {}),
    }
    try {
      if (editing?.id) {
        await adminApi.updateUpstreamPlatform(editing.id, payload)
      } else {
        await adminApi.createUpstreamPlatform(payload)
      }
      setEditing(null)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function handleDelete(id: number) {
    setMutError('')
    try {
      await adminApi.deleteUpstreamPlatform(id)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function syncBalance(p: AdminUpstreamPlatform) {
    if (!p.id) return
    setSyncingId(p.id)
    try {
      await adminApi.syncUpstreamBalance(p.id)
      toast.success('余额已同步')
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      toast.error(getApiErrorMessage(err))
    } finally {
      setSyncingId(null)
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Upstream"
        title="上游平台"
        description="维护上游账户和余额检测凭据；后台每 10 秒自动同步余额并按阈值推送 Lark 告警。"
        actions={
          <Button size="sm" onClick={openCreate}>
            <PlusIcon className="mr-1 size-3.5" />添加平台
          </Button>
        }
      />
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{String(error)}</AlertDescription>
        </Alert>
      ) : null}
      {mutError ? (
        <Alert variant="destructive">
          <AlertDescription>{mutError}</AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-16">ID</TableHead>
              <TableHead>名称</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>Base URL</TableHead>
              <TableHead className="w-36 text-right">可用余额</TableHead>
              <TableHead className="w-40">低余额告警</TableHead>
              <TableHead>余额凭据</TableHead>
              <TableHead className="w-24">状态</TableHead>
              <TableHead className="w-40">同步时间</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          {loading && platforms.length === 0 ? <TableSkeleton cols={10} /> : (
            <TableBody>
              {platforms.length === 0 ? (
                <TableEmpty cols={10} Icon={ServerIcon} title="暂无上游平台" description="点击右上角添加。" />
              ) : platforms.map((p) => (
                <TableRow key={p.id}>
                  <TableCell>{p.id}</TableCell>
                  <TableCell>
                    <div className="font-medium">{p.name}</div>
                    <div className="text-xs text-muted-foreground">{p.note || '-'}</div>
                  </TableCell>
                  <TableCell><Badge variant="outline">{platformLabel(p.platform_type)}</Badge></TableCell>
                  <TableCell className="max-w-[320px] truncate font-mono text-xs text-muted-foreground">{p.base_url}</TableCell>
                  <TableCell className="text-right font-mono">{formatBalance(p)}</TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1.5">
                      <Badge variant={p.balance_alert_threshold && p.balance_alert_threshold > 0 ? 'outline' : 'secondary'}>
                        {formatAlertThreshold(p)}
                      </Badge>
                      {p.balance_alert_notified ? <Badge variant="destructive">已通知</Badge> : null}
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1.5">
                      <Badge variant={p.has_api_key ? 'default' : 'secondary'}>api key</Badge>
                      <Badge variant={p.has_system_token ? 'default' : 'secondary'}>token</Badge>
                      {p.upstream_user_id ? <Badge variant="outline">user</Badge> : null}
                    </div>
                  </TableCell>
                  <TableCell>
                    {p.is_active
                      ? <Badge className="bg-emerald-600 text-white">正常</Badge>
                      : <Badge variant="secondary">停用</Badge>}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {p.balance_synced_at ? new Date(p.balance_synced_at).toLocaleString('zh-CN') : '未同步'}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      {supportsBalance(p.platform_type) ? (
                        <Button size="sm" variant="outline" disabled={syncingId === p.id} onClick={() => syncBalance(p)}>
                          {syncingId === p.id ? <Loader2 className="size-3.5 animate-spin" /> : <RefreshCwIcon className="size-3.5" />}
                          立即同步
                        </Button>
                      ) : null}
                      <Button size="sm" variant="outline" onClick={() => openEdit(p)}>
                        <Pencil1Icon className="size-3.5" />编辑
                      </Button>
                      <Button size="sm" variant="destructive" onClick={() => p.id && handleDelete(p.id)}>
                        <TrashIcon className="size-3.5" />删除
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          )}
        </Table>
      </Card>

      <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>{editing?.id ? '编辑上游平台' : '添加上游平台'}</DialogTitle>
            <DialogDescription>这里保存余额检测所需的账户信息；渠道成本同步在渠道编辑弹窗里操作。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-4 md:grid-cols-2">
            <div className="space-y-1.5">
              <Label>名称</Label>
              <Input value={form.name} onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))} placeholder="zzshu" />
            </div>
            <div className="space-y-1.5">
              <Label>平台类型</Label>
              <NativeSelect value={form.platform_type} onChange={(e) => setForm((f) => ({ ...f, platform_type: e.target.value }))}>
                <option value="newapi">zzshu / New API</option>
                <option value="sub2api">modelboxs / Sub2API</option>
                <option value="openai">OpenAI 兼容</option>
              </NativeSelect>
            </div>
            <div className="space-y-1.5 md:col-span-2">
              <Label>Base URL</Label>
              <Input value={form.base_url} onChange={(e) => setForm((f) => ({ ...f, base_url: e.target.value }))} placeholder="https://us.zzshu.cc" />
            </div>
            <div className="space-y-1.5">
              <Label>余额 API Key（可选）</Label>
              <Input type="password" value={form.api_key} onChange={(e) => setForm((f) => ({ ...f, api_key: e.target.value }))} placeholder={editing?.id ? '留空不修改' : 'sk-...'} />
            </div>
            <div className="space-y-1.5">
              <Label>{form.platform_type === 'sub2api' ? '控制台 JWT（可选）' : '系统访问令牌（可选）'}</Label>
              <Input type="password" value={form.system_token} onChange={(e) => setForm((f) => ({ ...f, system_token: e.target.value }))} placeholder={editing?.id ? '留空不修改' : form.platform_type === 'sub2api' ? 'eyJ...' : 'token'} />
            </div>
            <div className="space-y-1.5">
              <Label>上游用户 ID（New API 余额可选）</Label>
              <Input value={form.upstream_user_id} onChange={(e) => setForm((f) => ({ ...f, upstream_user_id: e.target.value }))} placeholder="New-Api-User" />
            </div>
            <div className="space-y-1.5">
              <Label>低余额告警阈值</Label>
              <Input
                type="number"
                min={0}
                step="0.0001"
                value={form.balance_alert_threshold}
                onChange={(e) => setForm((f) => ({ ...f, balance_alert_threshold: e.target.value }))}
                placeholder="0 = 关闭"
              />
            </div>
            <div className="space-y-1.5 md:col-span-2">
              <Label>备注</Label>
              <Input value={form.note} onChange={(e) => setForm((f) => ({ ...f, note: e.target.value }))} />
            </div>
            <label className="flex items-center gap-2 text-sm md:col-span-2">
              <Checkbox checked={form.is_active} onCheckedChange={(v) => setForm((f) => ({ ...f, is_active: v === true }))} />
              启用
            </label>
            {mutError ? <p className="text-sm text-destructive md:col-span-2">{mutError}</p> : null}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditing(null)}>取消</Button>
            <Button onClick={handleSave}>保存</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
