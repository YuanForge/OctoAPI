import { useState } from 'react'
import { RefreshCwIcon } from 'lucide-react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/shared/PageHeader'
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
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  adminApi,
  type AdminChannel,
  type AdminKeyPool,
  type AdminKeyPoolSyncResult,
  type AdminPoolKey,
} from '@/lib/api/admin'
import { useAsync } from '@/hooks/use-async'

export function AdminKeyPoolsPage() {
  const { data, loading, error: loadError, reload } = useAsync(async () => {
    const [poolResponse, channelResponse] = await Promise.all([
      adminApi.listKeyPools(),
      adminApi.listChannels(),
    ])
    const pools = Array.isArray(poolResponse) ? poolResponse : poolResponse.pools ?? []
    const channels = (Array.isArray(channelResponse)
      ? channelResponse
      : channelResponse.channels ?? channelResponse.items ?? []
    ).filter((item: AdminChannel) => item?.id)
    return { pools, channels }
  }, { pools: [] as AdminKeyPool[], channels: [] as AdminChannel[] })

  const pools = data.pools
  const channels = data.channels

  const [mutError, setMutError] = useState('')
  const [activePool, setActivePool] = useState<AdminKeyPool | null>(null)
  const [keys, setKeys] = useState<AdminPoolKey[]>([])
  const [keysLoading, setKeysLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [keyOpen, setKeyOpen] = useState(false)
  const [name, setName] = useState('')
  const [channelId, setChannelId] = useState(() => String(data.channels[0]?.id ?? ''))
  const [keyValue, setKeyValue] = useState('')
  const [keyBaseUrl, setKeyBaseUrl] = useState('')
  const [priority, setPriority] = useState('0')
  const [pendingDeletePool, setPendingDeletePool] = useState<AdminKeyPool | undefined>()
  const [importOpen, setImportOpen] = useState(false)
  const [importText, setImportText] = useState('')
  const [importing, setImporting] = useState(false)
  const [importResult, setImportResult] = useState<{ imported: number; skipped: number } | null>(null)
  const [bindingOpen, setBindingOpen] = useState(false)
  const [bindingPool, setBindingPool] = useState<AdminKeyPool | null>(null)
  const [boundChannels, setBoundChannels] = useState<AdminChannel[]>([])
  const [bindingLoading, setBindingLoading] = useState(false)
  const [syncingPoolId, setSyncingPoolId] = useState<number | null>(null)

  const error = loadError || mutError

  // Sync default channelId when channels first load
  if (channels.length > 0 && !channelId) {
    setChannelId(String(channels[0].id ?? ''))
  }

  async function openKeys(pool: AdminKeyPool) {
    setActivePool(pool)
    setKeyOpen(true)
    setKeysLoading(true)
    setMutError('')
    try {
      const response = await adminApi.listPoolKeys(pool.id as number)
      setKeys(Array.isArray(response) ? response : response.keys ?? [])
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    } finally {
      setKeysLoading(false)
    }
  }

  async function createPool() {
    setMutError('')
    try {
      await adminApi.createKeyPool({ channel_id: Number(channelId), name })
      setCreateOpen(false)
      setName('')
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function togglePool(pool: AdminKeyPool) {
    if (!pool.id) return
    setMutError('')
    try {
      await adminApi.toggleKeyPool(pool.id)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function toggleVendor(pool: AdminKeyPool) {
    if (!pool.id) return
    setMutError('')
    try {
      await adminApi.toggleVendorSubmittable(pool.id)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function executeDeletePool() {
    if (!pendingDeletePool?.id) return
    setMutError('')
    try {
      await adminApi.deleteKeyPool(pendingDeletePool.id)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    } finally {
      setPendingDeletePool(undefined)
    }
  }

  async function openBinding(pool: AdminKeyPool) {
    if (!pool.id) return
    setBindingPool(pool)
    setBindingOpen(true)
    setBindingLoading(true)
    setMutError('')
    try {
      const res = await adminApi.getKeyPoolChannels(pool.id)
      setBoundChannels(res.channels ?? [])
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    } finally {
      setBindingLoading(false)
    }
  }

  async function addKey() {
    if (!activePool?.id) return
    setMutError('')
    try {
      await adminApi.addPoolKey(activePool.id, {
        value: keyValue,
        priority: Number(priority),
        base_url_override: keyBaseUrl.trim() || undefined,
      })
      setKeyValue('')
      setKeyBaseUrl('')
      setPriority('0')
      await openKeys(activePool)
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function updateKey(row: AdminPoolKey) {
    if (!row.id) return
    setMutError('')
    try {
      await adminApi.updatePoolKey(row.id, {
        priority: row.priority ?? 0,
        is_active: row.is_active ?? true,
        base_url_override: row.base_url_override?.trim() ?? '',
      })
      if (activePool) await openKeys(activePool)
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function removeKey(row: AdminPoolKey) {
    if (!row.id) return
    setMutError('')
    try {
      await adminApi.removePoolKey(row.id)
      if (activePool) await openKeys(activePool)
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function clearKeyVendor(row: AdminPoolKey) {
    if (!row.id) return
    setMutError('')
    try {
      await adminApi.setPoolKeyVendor(row.id, null)
      if (activePool) await openKeys(activePool)
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  function updateDraftKey(id: number | undefined, patch: Partial<AdminPoolKey>) {
    if (!id) return
    setKeys((current) => current.map((row) => (row.id === id ? { ...row, ...patch } : row)))
  }

  async function importKeys() {
    if (!activePool?.id) return
    setMutError('')
    setImporting(true)
    setImportResult(null)
    try {
      const lines = importText.split(/[\n,]+/).map(s => s.trim()).filter(Boolean)
      const res = await adminApi.importPoolKeys(activePool.id, lines)
      setImportResult(res)
      setImportText('')
      await openKeys(activePool)
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    } finally {
      setImporting(false)
    }
  }

  function formatSyncResult(result: AdminKeyPoolSyncResult) {
    const parts = [
      `导入 ${result.imported ?? 0}`,
      `恢复 ${result.reactivated ?? 0}`,
      `跳过 ${result.skipped ?? 0}`,
    ]
    if (result.created_upstream) {
      parts.push(`新建上游 ${result.created_upstream}`)
    }
    if (result.skipped_by_lock) {
      parts.push('已有同步任务在执行')
    }
    return parts.join('，')
  }

  async function syncUpstreamKeys(pool: AdminKeyPool) {
    if (!pool.id) return
    setMutError('')
    setSyncingPoolId(pool.id)
    try {
      const result = await adminApi.syncKeyPoolFromUpstream(pool.id)
      toast.success(`上游 Key 已同步：${formatSyncResult(result)}`)
      reload()
      if (keyOpen && activePool?.id === pool.id) {
        await openKeys(pool)
      }
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      const msg = getApiErrorMessage(err)
      setMutError(msg)
      toast.error(msg)
    } finally {
      setSyncingPoolId(null)
    }
  }

  function channelLabel(channel: AdminChannel) {
    return `${channel.name ?? '未命名渠道'} · ${channel.type ?? 'unknown'} · #${channel.id}`
  }

  return (
    <>
      <PageHeader
        eyebrow="Key Pools"
        title="号池管理"
        description="管理号池、启停、切换号商上传，以及管理池内 Key。"
        actions={
          <>
            {error ? (
              <Button size="sm" variant="outline" onClick={reload}>
                重试
              </Button>
            ) : null}
            <Button onClick={() => setCreateOpen(true)}>新建号池</Button>
          </>
        }
      />
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <Card className="overflow-hidden">
        <Table className="min-w-[1000px]">
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>名称</TableHead>
              <TableHead>渠道 ID</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>号商上传</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          {loading ? (
            <TableSkeleton cols={6} />
          ) : (
            <TableBody>
              {pools.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-10 text-center text-muted-foreground">
                    暂无号池数据
                  </TableCell>
                </TableRow>
              ) : (
                pools.map((pool, index) => (
                  <TableRow key={pool.id ?? index}>
                    <TableCell>{pool.id ?? '-'}</TableCell>
                    <TableCell>{pool.name ?? '-'}</TableCell>
                    <TableCell>{pool.channel_id ?? '-'}</TableCell>
                    <TableCell>
                      <Badge variant={pool.is_active ? 'default' : 'secondary'}>
                        {pool.is_active ? '启用' : '停用'}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Badge variant={pool.vendor_submittable ? 'default' : 'secondary'}>
                        {pool.vendor_submittable ? '开放' : '关闭'}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex flex-wrap justify-end gap-2">
                        <Button size="sm" variant="outline" onClick={() => openKeys(pool)}>管理 Keys</Button>
                        <Button size="sm" variant="outline" onClick={() => openBinding(pool)}>绑定渠道</Button>
                        <Button size="sm" variant="outline" disabled={syncingPoolId === pool.id} onClick={() => syncUpstreamKeys(pool)}>
                          <RefreshCwIcon className={`mr-1 h-3.5 w-3.5 ${syncingPoolId === pool.id ? 'animate-spin' : ''}`} />
                          {syncingPoolId === pool.id ? '同步中' : '同步上游'}
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => togglePool(pool)}>
                          {pool.is_active ? '停用' : '启用'}
                        </Button>
                        <Button size="sm" variant="outline" onClick={() => toggleVendor(pool)}>切上传</Button>
                        <Button size="sm" onClick={() => setPendingDeletePool(pool)}>删除</Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          )}
        </Table>
      </Card>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="w-[min(calc(100vw-2rem),720px)] max-w-none sm:max-w-[720px]">
          <DialogHeader><DialogTitle>新建号池</DialogTitle></DialogHeader>
          <div className="flex flex-col gap-4">
            <NativeSelect value={channelId} onChange={(event) => setChannelId(event.target.value)}>
              <option value="">选择关联渠道</option>
              {channels.map((channel) => (
                <option key={channel.id} value={String(channel.id)}>
                  {channelLabel(channel)}
                </option>
              ))}
            </NativeSelect>
            <Input value={name} onChange={(event) => setName(event.target.value)} placeholder="号池名称" />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
            <Button onClick={createPool} disabled={!channelId || !name.trim()}>创建</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={keyOpen} onOpenChange={setKeyOpen}>
        <DialogContent className="max-h-[86vh] w-[min(calc(100vw-2rem),1280px)] max-w-none overflow-y-auto sm:max-w-[1280px]">
          <DialogHeader><DialogTitle>{activePool?.name ?? ''} - Key 管理</DialogTitle></DialogHeader>
          <div className="flex flex-wrap gap-3">
            <Input value={keyValue} onChange={(event) => setKeyValue(event.target.value)} placeholder="Key 值" />
            <Input className="min-w-[320px] flex-1" value={keyBaseUrl} onChange={(event) => setKeyBaseUrl(event.target.value)} placeholder="https://api.example.com/v1/images/generations" />
            <Input value={priority} onChange={(event) => setPriority(event.target.value)} placeholder="优先级" />
            <Button onClick={addKey}>添加 Key</Button>
            <Button variant="outline" onClick={() => { setImportOpen(true); setImportResult(null) }}>批量导入</Button>
            {activePool ? (
              <>
                <Button variant="outline" disabled={syncingPoolId === activePool.id} onClick={() => syncUpstreamKeys(activePool)}>
                  <RefreshCwIcon className={`mr-1 h-4 w-4 ${syncingPoolId === activePool.id ? 'animate-spin' : ''}`} />
                  {syncingPoolId === activePool.id ? '同步中' : '同步上游'}
                </Button>
              </>
            ) : null}
          </div>
          {importOpen ? (
            <div className="space-y-2 rounded-md border p-3">
              <p className="text-sm text-muted-foreground">每行或逗号分隔填写 Key 值，重复的自动跳过。</p>
              <Textarea
                rows={4}
                value={importText}
                onChange={e => setImportText(e.target.value)}
                placeholder="key1&#10;key2&#10;key3"
              />
              {importResult ? (
                <p className="text-xs text-muted-foreground">
                  已导入 <strong>{importResult.imported}</strong> 条，跳过 <strong>{importResult.skipped}</strong> 条。
                </p>
              ) : null}
              <div className="flex gap-2">
                <Button size="sm" onClick={importKeys} disabled={importing || !importText.trim()}>
                  {importing ? '导入中…' : '确认导入'}
                </Button>
                <Button size="sm" variant="outline" onClick={() => { setImportOpen(false); setImportText('') }}>取消</Button>
              </div>
            </div>
          ) : null}
          <Table className="min-w-[1280px]">
            <TableHeader>
              <TableRow>
                <TableHead>ID</TableHead>
                <TableHead>Key</TableHead>
                <TableHead>Base URL</TableHead>
                <TableHead>号商</TableHead>
                <TableHead>最近使用</TableHead>
                <TableHead>调用次数</TableHead>
                <TableHead>失败率</TableHead>
                <TableHead>优先级</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            {keysLoading ? (
              <TableSkeleton cols={10} rows={3} />
            ) : (
              <TableBody>
                {keys.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={10} className="py-6 text-center text-muted-foreground">
                      暂无 Key 数据
                    </TableCell>
                  </TableRow>
                ) : (
                  keys.map((row, index) => (
                    <TableRow key={row.id ?? index}>
                      <TableCell>{row.id ?? '-'}</TableCell>
                      <TableCell className="font-mono text-xs">{row.value ?? '-'}</TableCell>
                      <TableCell>
                        <Input
                          className="min-w-[280px] font-mono text-xs"
                          value={row.base_url_override ?? ''}
                          onChange={(event) =>
                            updateDraftKey(row.id, { base_url_override: event.target.value })
                          }
                          placeholder="使用渠道 Base URL"
                        />
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {row.vendor_id != null
                          ? <span className="font-medium text-foreground">号商 #{row.vendor_id}</span>
                          : <span className="text-muted-foreground/50">直营</span>}
                        {row.vendor_id != null && (
                          <Button
                            size="sm"
                            variant="ghost"
                            className="ml-1 h-5 px-1 text-xs text-muted-foreground"
                            onClick={() => clearKeyVendor(row)}
                          >
                            解绑
                          </Button>
                        )}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {row.last_used_at ? new Date(row.last_used_at).toLocaleString('zh-CN') : '从未'}
                      </TableCell>
                      <TableCell className="text-right">{row.total_calls ?? 0}</TableCell>
                      <TableCell>
                        {row.fail_rate != null
                          ? <span className={row.fail_rate > 0.1 ? 'text-destructive' : ''}>{(row.fail_rate * 100).toFixed(1)}%</span>
                          : '-'}
                      </TableCell>
                      <TableCell>
                        <Input
                          className="w-24"
                          value={String(row.priority ?? 0)}
                          onChange={(event) =>
                            updateDraftKey(row.id, { priority: Number(event.target.value || '0') })
                          }
                        />
                      </TableCell>
                      <TableCell>
                        <Label className="flex items-center gap-2 text-sm">
                          <input
                            type="checkbox"
                            checked={row.is_active !== false}
                            onChange={(event) =>
                              updateDraftKey(row.id, { is_active: event.target.checked })
                            }
                          />
                          {row.is_active === false ? '停用' : '启用'}
                        </Label>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          <Button size="sm" variant="outline" onClick={() => updateKey(row)}>保存</Button>
                          <Button size="sm" onClick={() => removeKey(row)}>删除</Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            )}
          </Table>
        </DialogContent>
      </Dialog>

      <AlertDialog open={pendingDeletePool !== undefined} onOpenChange={() => setPendingDeletePool(undefined)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确认删除号池 {pendingDeletePool?.name ?? pendingDeletePool?.id} 吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={executeDeletePool}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={bindingOpen} onOpenChange={setBindingOpen}>
        <DialogContent className="w-[min(calc(100vw-2rem),900px)] max-w-none sm:max-w-[900px]">
          <DialogHeader><DialogTitle>{bindingPool?.name ?? ''} - 绑定渠道</DialogTitle></DialogHeader>
          {bindingLoading ? (
            <p className="py-4 text-center text-sm text-muted-foreground">加载中…</p>
          ) : boundChannels.length === 0 ? (
            <p className="py-4 text-center text-sm text-muted-foreground">暂无渠道使用此号池</p>
          ) : (
            <Table className="min-w-[760px]">
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>渠道名称</TableHead>
                  <TableHead>模型</TableHead>
                  <TableHead>状态</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {boundChannels.map((ch, i) => (
                  <TableRow key={ch.id ?? i}>
                    <TableCell>{ch.id}</TableCell>
                    <TableCell>{ch.name ?? '-'}</TableCell>
                    <TableCell>{ch.model ?? '-'}</TableCell>
                    <TableCell>
                      <Badge variant={ch.is_active ? 'default' : 'secondary'}>
                        {ch.is_active ? '启用' : '停用'}
                      </Badge>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setBindingOpen(false)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
