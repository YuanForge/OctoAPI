import { useEffect, useRef, useState } from 'react'
import { ListIcon, RefreshCwIcon, Trash2Icon } from 'lucide-react'

import { DateRangeFilter } from '@/components/shared/DateRangeFilter'
import { PageHeader } from '@/components/shared/PageHeader'
import { TableEmpty } from '@/components/shared/TableEmpty'
import { TableSkeleton } from '@/components/shared/TableSkeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { adminApi, type AdminCleanupPreview, type AdminCleanupRunResult, type AdminCleanupTarget, type AdminMe, type AdminTask } from '@/lib/api/admin'
import { getApiErrorMessage } from '@/lib/api/http'
import { useAsync } from '@/hooks/use-async'

function statusBadge(s: string | undefined) {
  if (s === 'pending') return <Badge className="bg-yellow-500 hover:bg-yellow-500 text-white">排队中</Badge>
  if (s === 'processing') return <Badge className="bg-blue-500 hover:bg-blue-500 text-white">处理中</Badge>
  if (s === 'done') return <Badge className="bg-emerald-600 hover:bg-emerald-600 text-white">成功</Badge>
  if (s === 'failed') return <Badge variant="destructive">失败</Badge>
  return <Badge variant="outline">{s ?? '-'}</Badge>
}

function JsonBlock({ title, value }: { title: string; value: unknown }) {
  if (!value || (typeof value === 'object' && Object.keys(value as object).length === 0)) return null
  return (
    <div className="mb-4">
      <p className="mb-1 text-sm font-semibold">{title}</p>
      <pre className="overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all">
        {JSON.stringify(value, null, 2)}
      </pre>
    </div>
  )
}

function cleanupTargetLabel(target: AdminCleanupTarget) {
  return target === 'tasks' ? '历史任务' : 'LLM 调用日志'
}

function cleanupStatusText(status: string) {
  const labels: Record<string, string> = {
    done: '成功',
    failed: '失败',
    ok: '成功',
    error: '错误',
    refunded: '已退款',
  }
  return labels[status] ?? status
}

function formatDateTime(value: string | undefined) {
  if (!value) return '-'
  return new Date(value).toLocaleString('zh-CN')
}

export function AdminTasksPage() {
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState({ task_id: '', user_id: '', type: '', status: '' })
  const [startAt, setStartAt] = useState('')
  const [endAt, setEndAt] = useState('')
  const [queryParams, setQueryParams] = useState<Record<string, unknown>>({ page: 1, size: 20 })

  const { data, loading, error, reload } = useAsync(async () => {
    const res = await adminApi.listTasks(queryParams)
    const tasks = Array.isArray(res) ? res : (res.tasks ?? res.items ?? [])
    const total = Array.isArray(res) ? tasks.length : (res as { total?: number }).total ?? tasks.length
    return { tasks, total }
  }, { tasks: [] as AdminTask[], total: 0 }, [queryParams])

  const { data: me } = useAsync<AdminMe | null>(
    () => adminApi.getAdminMe(),
    null,
    [],
  )
  const permissions = me?.permissions ?? []
  const canCleanup = permissions.includes('*') || permissions.includes('tasks:write')

  const pageSize = 20
  const totalPages = Math.ceil(data.total / pageSize)

  // 详情弹窗
  const [detail, setDetail] = useState<AdminTask | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const autoRefreshRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const [cleanupTarget, setCleanupTarget] = useState<AdminCleanupTarget>('tasks')
  const [cleanupRetentionDays, setCleanupRetentionDays] = useState('90')
  const [cleanupPreview, setCleanupPreview] = useState<AdminCleanupPreview | null>(null)
  const [cleanupResult, setCleanupResult] = useState<AdminCleanupRunResult | null>(null)
  const [cleanupError, setCleanupError] = useState('')
  const [cleanupLoading, setCleanupLoading] = useState(false)
  const [cleanupRunning, setCleanupRunning] = useState(false)
  const [cleanupDialogOpen, setCleanupDialogOpen] = useState(false)
  const [cleanupConfirm, setCleanupConfirm] = useState('')

  function stopAutoRefresh() {
    if (autoRefreshRef.current) { clearInterval(autoRefreshRef.current); autoRefreshRef.current = null }
  }
  useEffect(() => () => stopAutoRefresh(), [])

  async function openDetail(id: number) {
    setDetailLoading(true)
    stopAutoRefresh()
    try {
      const res = await adminApi.getAdminTask(id)
      const task: AdminTask = (res as { task?: AdminTask }).task ?? (res as AdminTask)
      setDetail(task)
      if (task.status === 'pending' || task.status === 'processing') {
        autoRefreshRef.current = setInterval(async () => {
          const r = await adminApi.getAdminTask(id)
          const t: AdminTask = (r as { task?: AdminTask }).task ?? (r as AdminTask)
          setDetail(t)
          if (t.status !== 'pending' && t.status !== 'processing') stopAutoRefresh()
        }, 3000)
      }
    } finally {
      setDetailLoading(false)
    }
  }

  function closeDetail() {
    stopAutoRefresh()
    setDetail(null)
  }

  function doSearch() {
    const params: Record<string, unknown> = { page: 1, size: pageSize }
    if (filters.task_id) params.task_id = filters.task_id
    if (filters.user_id) params.user_id = filters.user_id
    if (filters.type) params.type = filters.type
    if (filters.status) params.status = filters.status
    if (startAt) params.start_at = startAt.replace('T', ' ') + ':00'
    if (endAt) params.end_at = endAt.replace('T', ' ') + ':00'
    setPage(1)
    setQueryParams(params)
  }

  function resetFilters() {
    setFilters({ task_id: '', user_id: '', type: '', status: '' })
    setStartAt('')
    setEndAt('')
    setPage(1)
    setQueryParams({ page: 1, size: pageSize })
  }

  function changePage(next: number) {
    setPage(next)
    setQueryParams((prev) => ({ ...prev, page: next }))
  }

  function cleanupRetentionValue() {
    return Number.parseInt(cleanupRetentionDays, 10)
  }

  function cleanupRetentionValid() {
    const days = cleanupRetentionValue()
    return Number.isInteger(days) && days >= 30
  }

  function resetCleanupOutput() {
    setCleanupPreview(null)
    setCleanupResult(null)
    setCleanupError('')
  }

  const parsedCleanupRetention = cleanupRetentionValue()
  const previewIsCurrent = cleanupPreview?.target === cleanupTarget && cleanupPreview?.retention_days === parsedCleanupRetention

  async function previewCleanup() {
    if (!cleanupRetentionValid()) {
      setCleanupError('保留天数不能少于 30 天')
      return
    }
    setCleanupLoading(true)
    setCleanupError('')
    setCleanupResult(null)
    try {
      const res = await adminApi.previewCleanup({
        target: cleanupTarget,
        retention_days: parsedCleanupRetention,
      })
      setCleanupPreview(res)
    } catch (err) {
      setCleanupPreview(null)
      setCleanupError(getApiErrorMessage(err))
    } finally {
      setCleanupLoading(false)
    }
  }

  function openCleanupDialog() {
    if (!previewIsCurrent) {
      setCleanupError('请先按当前条件预估清理数量')
      return
    }
    setCleanupConfirm('')
    setCleanupDialogOpen(true)
  }

  async function runCleanup() {
    if (!previewIsCurrent || cleanupConfirm.trim() !== '确认清理') return
    setCleanupRunning(true)
    setCleanupError('')
    try {
      const res = await adminApi.runCleanup({
        target: cleanupTarget,
        retention_days: parsedCleanupRetention,
        confirm: cleanupConfirm,
      })
      setCleanupResult(res)
      setCleanupPreview({
        target: res.target,
        target_label: res.target_label,
        retention_days: res.retention_days,
        cutoff: res.cutoff,
        count: res.remaining,
        statuses: res.statuses,
      })
      setCleanupDialogOpen(false)
      setCleanupConfirm('')
      if (res.target === 'tasks') reload()
    } catch (err) {
      setCleanupError(getApiErrorMessage(err))
    } finally {
      setCleanupRunning(false)
    }
  }

  const upstreamURL = (req?: Record<string, unknown>) => req?._url as string | undefined
  const upstreamHeaders = (req?: Record<string, unknown>) => {
    if (!req) return undefined
    return req._headers as Record<string, unknown> | undefined
  }
  const upstreamBody = (req?: Record<string, unknown>) => {
    if (!req) return undefined
    const initial = req._initial_request
    if (initial && typeof initial === 'object') {
      return initial as Record<string, unknown>
    }
    const { _url, _headers, _poll_request, _method, method, query, ...rest } = req
    if (Object.keys(rest).length > 0) {
      return rest
    }
    return undefined
  }

  const pollBody = (req?: Record<string, unknown>) => {
    if (!req) return undefined
    if (req._poll_request && typeof req._poll_request === 'object') {
      return req._poll_request as Record<string, unknown>
    }
    const { method, query } = req
    if (method || query) {
      return { method, query }
    }
    return undefined
  }

  return (
    <>
      <PageHeader
        eyebrow="Task Center"
        title="异步任务排障"
        description="查看用户任务状态与完整请求/响应信息，辅助对接排障。"
        actions={
          error ? (
            <Button size="sm" variant="outline" onClick={reload}>
              重试
            </Button>
          ) : null
        }
      />
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {/* 快速统计卡片（根据当前查询结果计算） */}
      {!loading && data.tasks.length > 0 ? (() => {
        const total = data.tasks.length
        const done = data.tasks.filter(t => t.status === 'done').length
        const failed = data.tasks.filter(t => t.status === 'failed').length
        const failedTasks = data.tasks.filter(t => t.status === 'failed' && t.error_msg)
        // 统计前三失败原因
        const errMap: Record<string, number> = {}
        failedTasks.forEach(t => {
          const key = (t.error_msg ?? '').slice(0, 40)
          errMap[key] = (errMap[key] ?? 0) + 1
        })
        const topErrors = Object.entries(errMap).sort((a, b) => b[1] - a[1]).slice(0, 3)
        return (
          <div className="grid grid-cols-2 gap-4 xl:grid-cols-4">
            <Card>
              <CardContent className="py-4">
                <p className="text-xs text-muted-foreground">本页记录数</p>
                <p className="mt-1 text-2xl font-bold">{total}</p>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="py-4">
                <p className="text-xs text-muted-foreground">成功</p>
                <p className="mt-1 text-2xl font-bold text-emerald-600">{done}</p>
                <p className="text-xs text-muted-foreground">{total > 0 ? ((done / total) * 100).toFixed(1) : '0'}%</p>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="py-4">
                <p className="text-xs text-muted-foreground">失败</p>
                <p className="mt-1 text-2xl font-bold text-destructive">{failed}</p>
                <p className="text-xs text-muted-foreground">{total > 0 ? ((failed / total) * 100).toFixed(1) : '0'}%</p>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="py-4">
                <p className="text-xs text-muted-foreground">高频失败原因</p>
                {topErrors.length > 0 ? (
                  <ul className="mt-1 space-y-0.5">
                    {topErrors.map(([msg, cnt]) => (
                      <li key={msg} className="flex items-start gap-1 text-xs">
                        <span className="shrink-0 font-bold text-destructive">×{cnt}</span>
                        <span className="truncate text-muted-foreground" title={msg}>{msg || '(无详情)'}</span>
                      </li>
                    ))}
                  </ul>
                ) : <p className="mt-1 text-sm text-muted-foreground">-</p>}
              </CardContent>
            </Card>
          </div>
        )
      })() : null}

      {canCleanup ? (
        <Card>
          <CardContent className="space-y-4 py-4">
            <div className="flex flex-col gap-3 xl:flex-row xl:items-end xl:justify-between">
              <div>
                <div className="flex items-center gap-2">
                  <Trash2Icon className="size-4 text-muted-foreground" />
                  <h2 className="text-sm font-semibold">数据清理</h2>
                </div>
                <p className="mt-1 text-xs text-muted-foreground">
                  只清理已结束且早于保留期限的数据；进行中任务、账单流水不会被删除。
                </p>
              </div>
              <div className="flex flex-wrap items-end gap-3">
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">清理类型</label>
                  <Select
                    value={cleanupTarget}
                    onValueChange={(v) => {
                      setCleanupTarget(v as AdminCleanupTarget)
                      resetCleanupOutput()
                    }}
                  >
                    <SelectTrigger className="w-40"><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="tasks">历史任务</SelectItem>
                      <SelectItem value="llm_logs">LLM 调用日志</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-1">
                  <label className="text-xs text-muted-foreground">保留天数</label>
                  <Input
                    className="w-28"
                    min={30}
                    type="number"
                    value={cleanupRetentionDays}
                    onChange={(e) => {
                      setCleanupRetentionDays(e.target.value)
                      resetCleanupOutput()
                    }}
                  />
                </div>
                <Button variant="outline" onClick={previewCleanup} disabled={cleanupLoading || !cleanupRetentionValid()}>
                  <RefreshCwIcon className={`mr-1 size-3.5 ${cleanupLoading ? 'animate-spin' : ''}`} />
                  预估清理
                </Button>
                <Button
                  variant="destructive"
                  onClick={openCleanupDialog}
                  disabled={!previewIsCurrent || cleanupLoading || (cleanupPreview?.count ?? 0) <= 0}
                >
                  <Trash2Icon className="mr-1 size-3.5" />
                  执行清理
                </Button>
              </div>
            </div>

            {cleanupError ? (
              <Alert variant="destructive">
                <AlertDescription>{cleanupError}</AlertDescription>
              </Alert>
            ) : null}

            {cleanupPreview ? (
              <div className="grid gap-3 text-sm md:grid-cols-4">
                <div className="rounded-lg border bg-muted/20 p-3">
                  <p className="text-xs text-muted-foreground">预计可清理</p>
                  <p className="mt-1 text-2xl font-semibold">{cleanupPreview.count.toLocaleString()}</p>
                </div>
                <div className="rounded-lg border bg-muted/20 p-3">
                  <p className="text-xs text-muted-foreground">清理类型</p>
                  <p className="mt-2 font-medium">{cleanupPreview.target_label || cleanupTargetLabel(cleanupPreview.target)}</p>
                </div>
                <div className="rounded-lg border bg-muted/20 p-3">
                  <p className="text-xs text-muted-foreground">早于时间</p>
                  <p className="mt-2 font-medium">{formatDateTime(cleanupPreview.cutoff)}</p>
                </div>
                <div className="rounded-lg border bg-muted/20 p-3">
                  <p className="text-xs text-muted-foreground">允许状态</p>
                  <div className="mt-2 flex flex-wrap gap-1">
                    {cleanupPreview.statuses.map((s) => (
                      <Badge key={s} variant="outline">{cleanupStatusText(s)}</Badge>
                    ))}
                  </div>
                </div>
              </div>
            ) : null}

            {cleanupResult ? (
              <Alert variant={cleanupResult.has_more ? 'warning' : 'success'}>
                <AlertDescription>
                  已删除 {cleanupResult.deleted.toLocaleString()} 条，剩余 {cleanupResult.remaining.toLocaleString()} 条
                  {cleanupResult.has_more ? '；单次最多清理 50000 条，可再次执行。' : '。'}
                </AlertDescription>
              </Alert>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {/* 过滤栏 */}
      <Card>
        <CardContent className="flex flex-wrap items-end gap-3 py-4">
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">Task ID</label>
            <Input
              className="w-28"
              placeholder="Task ID"
              value={filters.task_id}
              onChange={(e) => setFilters((f) => ({ ...f, task_id: e.target.value }))}
              onKeyDown={(e) => e.key === 'Enter' && doSearch()}
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">用户 ID</label>
            <Input
              className="w-28"
              placeholder="用户 ID"
              value={filters.user_id}
              onChange={(e) => setFilters((f) => ({ ...f, user_id: e.target.value }))}
              onKeyDown={(e) => e.key === 'Enter' && doSearch()}
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">任务类型</label>
            <Select value={filters.type || '_all'} onValueChange={(v) => setFilters((f) => ({ ...f, type: v === '_all' ? '' : v }))}>
              <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="_all">全部类型</SelectItem>
                <SelectItem value="image">图片</SelectItem>
                <SelectItem value="video">视频</SelectItem>
                <SelectItem value="audio">音频</SelectItem>
                <SelectItem value="music">音乐</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">状态</label>
            <Select value={filters.status || '_all'} onValueChange={(v) => setFilters((f) => ({ ...f, status: v === '_all' ? '' : v }))}>
              <SelectTrigger className="w-28"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="_all">全部状态</SelectItem>
                <SelectItem value="pending">排队中</SelectItem>
                <SelectItem value="processing">处理中</SelectItem>
                <SelectItem value="done">成功</SelectItem>
                <SelectItem value="failed">失败</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <DateRangeFilter
            startAt={startAt}
            endAt={endAt}
            label="时间范围"
            onChange={({ startAt: s, endAt: e }) => { setStartAt(s); setEndAt(e) }}
          />
          <Button onClick={doSearch}>查询</Button>
          <Button variant="outline" onClick={resetFilters}>重置</Button>
        </CardContent>
      </Card>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-20">Task ID</TableHead>
              <TableHead className="w-20">用户 ID</TableHead>
              <TableHead className="w-20">类型</TableHead>
              <TableHead className="w-28">状态</TableHead>
              <TableHead className="w-20">渠道</TableHead>
              <TableHead className="w-32 text-right">扣费（cr）</TableHead>
              <TableHead>第三方任务 ID</TableHead>
              <TableHead className="w-48">错误信息</TableHead>
              <TableHead className="w-40">创建时间</TableHead>
              <TableHead className="w-40">结束时间</TableHead>
              <TableHead className="w-16 text-center">操作</TableHead>
            </TableRow>
          </TableHeader>
          {loading ? (
            <TableSkeleton cols={11} />
          ) : (
            <TableBody>
              {data.tasks.length === 0 ? (
                <TableEmpty
                  cols={11}
                  Icon={ListIcon}
                  title="还没有任务记录"
                  description="平台用户发起异步任务后会汇总在此处。"
                />
              ) : (
                data.tasks.map((row, index) => (
                  <TableRow key={row.id ?? index}>
                    <TableCell>{row.id ?? '-'}</TableCell>
                    <TableCell className="text-muted-foreground">{row.user_id ?? '-'}</TableCell>
                    <TableCell>{row.type ?? '-'}</TableCell>
                    <TableCell>{statusBadge(row.status)}</TableCell>
                    <TableCell className="text-muted-foreground">{row.channel_id ?? '-'}</TableCell>
                    <TableCell className="text-right font-mono text-sm">
                      {row.credits_charged != null ? row.credits_charged.toLocaleString() : '-'}
                    </TableCell>
                    <TableCell className="max-w-40 truncate font-mono text-xs text-muted-foreground" title={row.upstream_task_id}>
                      {row.upstream_task_id ?? '-'}
                    </TableCell>
                    <TableCell className="max-w-48 truncate text-xs text-destructive/80" title={row.error_msg}>
                      {row.error_msg ? row.error_msg.slice(0, 60) + (row.error_msg.length > 60 ? '…' : '') : '-'}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {row.created_at ? new Date(row.created_at).toLocaleString('zh-CN') : '-'}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {(row.status === 'done' || row.status === 'failed') && row.updated_at
                        ? new Date(row.updated_at).toLocaleString('zh-CN')
                        : '-'}
                    </TableCell>
                    <TableCell className="text-center">
                      {row.id != null ? (
                        <Button size="sm" variant="link" className="h-auto p-0" onClick={() => openDetail(row.id!)}>
                          详情
                        </Button>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          )}
        </Table>
        {totalPages > 1 ? (
          <CardContent className="flex items-center justify-between border-t py-3">
            <span className="text-sm text-muted-foreground">共 {data.total} 条记录</span>
            <div className="flex items-center gap-2">
              <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => changePage(page - 1)}>上一页</Button>
              <span className="text-sm text-muted-foreground">第 {page} / {totalPages} 页</span>
              <Button size="sm" variant="outline" disabled={page >= totalPages} onClick={() => changePage(page + 1)}>下一页</Button>
            </div>
          </CardContent>
        ) : null}
      </Card>

      {/* 详情弹窗 */}
      <Dialog open={Boolean(detail)} onOpenChange={closeDetail}>
        <DialogContent className="max-w-[872px] max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>任务详情 #{detail?.id}</DialogTitle>
          </DialogHeader>
          {detailLoading ? (
            <div className="space-y-3 py-2">
              <div className="h-24 w-full animate-pulse rounded-lg border bg-muted/40" />
              <div className="h-32 w-full animate-pulse rounded-lg border bg-muted/40" />
              <div className="h-16 w-full animate-pulse rounded-lg border bg-muted/40" />
            </div>
          ) : detail ? (
            <div className="space-y-1">
              <div className="grid grid-cols-2 gap-x-4 gap-y-1 rounded-lg border p-4 text-sm">
                <div><span className="text-muted-foreground">Task ID：</span><strong>{detail.id}</strong></div>
                <div><span className="text-muted-foreground">用户 ID：</span><strong>{detail.user_id ?? '-'}</strong></div>
                <div><span className="text-muted-foreground">任务类型：</span>{detail.type ?? '-'}</div>
                <div><span className="text-muted-foreground">状态：</span>{statusBadge(detail.status)}</div>
                <div><span className="text-muted-foreground">渠道 ID：</span>{detail.channel_id ?? '-'}</div>
                <div><span className="text-muted-foreground">扣费：</span>{detail.credits_charged?.toLocaleString() ?? '-'} cr</div>
                {detail.upstream_task_id ? (
                  <div className="col-span-2"><span className="text-muted-foreground">第三方任务 ID：</span><span className="font-mono text-xs">{detail.upstream_task_id}</span></div>
                ) : null}
                {detail.error_msg ? (
                  <div className="col-span-2"><span className="text-muted-foreground">错误信息：</span><span className="text-red-500">{detail.error_msg}</span></div>
                ) : null}
                <div className="col-span-2"><span className="text-muted-foreground">创建时间：</span>{detail.created_at ? new Date(detail.created_at).toLocaleString('zh-CN') : '-'}</div>
                {(detail.status === 'done' || detail.status === 'failed') && detail.updated_at ? (
                  <div className="col-span-2"><span className="text-muted-foreground">完成时间：</span>{new Date(detail.updated_at).toLocaleString('zh-CN')}</div>
                ) : null}
              </div>
              <div className="pt-2">
                <JsonBlock title="用户提交请求体" value={detail.request} />
                {detail.upstream_request && upstreamURL(detail.upstream_request) ? (
                  <div className="mb-4">
                    <p className="mb-1 text-sm font-semibold">上游请求 URL</p>
                    <pre className="overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs text-blue-600 whitespace-pre-wrap break-all">{upstreamURL(detail.upstream_request)}</pre>
                  </div>
                ) : null}
                <JsonBlock title="上游请求头" value={upstreamHeaders(detail.upstream_request)} />
                <JsonBlock title="首次发送给第三方的请求体" value={upstreamBody(detail.upstream_request)} />
                <JsonBlock title="轮询第三方任务请求体" value={pollBody(detail.upstream_request)} />
                <JsonBlock title="第三方原始响应体" value={detail.upstream_response} />
                <JsonBlock title="平台标准结果" value={detail.result} />
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <AlertDialog open={cleanupDialogOpen} onOpenChange={(open) => {
        if (!cleanupRunning) setCleanupDialogOpen(open)
      }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认清理数据</AlertDialogTitle>
            <AlertDialogDescription>
              将物理删除 {cleanupPreview?.target_label ?? cleanupTargetLabel(cleanupTarget)}
              中早于 {formatDateTime(cleanupPreview?.cutoff)} 的 {cleanupPreview?.count.toLocaleString() ?? 0} 条记录。
              此操作不会删除进行中数据和账单流水，但删除后无法恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="space-y-2">
            <label className="text-xs text-muted-foreground">输入“确认清理”后执行</label>
            <Input
              autoFocus
              value={cleanupConfirm}
              onChange={(e) => setCleanupConfirm(e.target.value)}
              placeholder="确认清理"
              disabled={cleanupRunning}
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={cleanupRunning}>取消</AlertDialogCancel>
            <Button
              variant="destructive"
              onClick={runCleanup}
              disabled={cleanupRunning || cleanupConfirm.trim() !== '确认清理'}
            >
              <Trash2Icon className="mr-1 size-3.5" />
              {cleanupRunning ? '清理中…' : '确认清理'}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

