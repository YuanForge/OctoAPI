import { useEffect, useMemo, useRef, useState } from 'react'
import { ListIcon } from 'lucide-react'

import { DateRangeFilter, formatDateTimeFilterValue } from '@/components/shared/DateRangeFilter'
import { PageHeader } from '@/components/shared/PageHeader'
import { TableEmpty } from '@/components/shared/TableEmpty'
import { TablePagination } from '@/components/shared/TablePagination'
import { TableSkeleton } from '@/components/shared/TableSkeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { useAsync } from '@/hooks/use-async'
import { userApi, type UserTask } from '@/lib/api/user'
import { copyToClipboard } from '@/lib/clipboard'

function resolveStatus(row: UserTask): string {
  if (typeof row.status === 'string') return row.status
  if (row.status === 0) return 'pending'
  if (row.status === 1) return 'processing'
  if (row.status === 2 || row.status === 200) return 'done'
  if (typeof row.status === 'number' && row.status < 0) return 'failed'
  return String(row.status ?? '-')
}

function statusBadge(s: string) {
  if (s === 'pending') return <Badge className="bg-yellow-500 text-white hover:bg-yellow-500">排队中</Badge>
  if (s === 'processing') return <Badge className="bg-blue-500 text-white hover:bg-blue-500">处理中</Badge>
  if (s === 'done') return <Badge className="bg-emerald-600 text-white hover:bg-emerald-600">已完成</Badge>
  if (s === 'failed') return <Badge variant="destructive">失败</Badge>
  return <Badge variant="outline">{s}</Badge>
}

function typeLabel(t: string | undefined) {
  return ({ image: '图片生成', video: '视频生成', audio: '音频生成', music: '音乐生成' } as Record<string, string>)[t ?? ''] ?? (t ?? '-')
}

function JsonBlock({ title, value }: { title: string; value: unknown }) {
  if (!value || (typeof value === 'object' && Object.keys(value as object).length === 0)) return null
  return (
    <div className="mb-4">
      <div className="mb-1 flex items-center justify-between">
        <p className="text-sm font-semibold">{title}</p>
        <Button
          size="sm"
          variant="ghost"
          className="h-6 px-2 text-xs"
          onClick={() => {
            void copyToClipboard(JSON.stringify(value, null, 2), { successMessage: '内容已复制' })
          }}
        >
          复制
        </Button>
      </div>
      <pre className="overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all">
        {JSON.stringify(value, null, 2)}
      </pre>
    </div>
  )
}

function splitLines(value: unknown) {
  if (typeof value !== 'string') return []
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
}

function normalizeStringList(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value
      .flatMap((item) => {
        if (typeof item === 'string') return [item.trim()]
        if (item && typeof item === 'object' && typeof (item as { url?: unknown }).url === 'string') {
          return [((item as { url?: string }).url ?? '').trim()]
        }
        return []
      })
      .filter(Boolean)
  }
  if (typeof value === 'string') {
    return splitLines(value)
  }
  return []
}

function firstString(...values: unknown[]) {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value.trim()
  }
  return ''
}

function collectResultVideoUrls(task: UserTask | null) {
  if (!task) return []
  const result = task.result ?? {}
  const urls = [
    task.url,
    result.url,
    ...(normalizeStringList(result.urls)),
    ...(normalizeStringList(result.videos)),
    ...(normalizeStringList(task.items)),
    ...(normalizeStringList(result.items)),
  ]
  return Array.from(new Set(urls.filter((item): item is string => Boolean(item))))
}

function collectReferenceImages(task: UserTask | null) {
  if (!task) return []
  return Array.from(new Set(normalizeStringList(task.request?.refer_images)))
}

function collectReferenceVideos(task: UserTask | null) {
  if (!task) return []
  return Array.from(new Set(normalizeStringList(task.request?.refer_videos)))
}

function MediaSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-lg border p-4">
      <p className="mb-3 text-sm font-semibold">{title}</p>
      {children}
    </div>
  )
}

export function UserTasksPage() {
  const [page, setPage] = useState(1)
  const [filters, setFilters] = useState({ task_id: '', type: '', status: '' })
  const [startAt, setStartAt] = useState('')
  const [endAt, setEndAt] = useState('')
  const [queryParams, setQueryParams] = useState<Record<string, unknown>>({ page: 1, size: 20 })

  const { data, loading, error, reload } = useAsync(async () => {
    const res = await userApi.listTasks(queryParams)
    const tasks = Array.isArray(res) ? res : (res.tasks ?? res.items ?? [])
    const total = Array.isArray(res) ? tasks.length : (res as { total?: number }).total ?? tasks.length
    return { tasks, total }
  }, { tasks: [] as UserTask[], total: 0 }, [queryParams])

  const pageSize = 20

  const [detail, setDetail] = useState<UserTask | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [billing, setBilling] = useState<{ transactions?: Array<{ id?: number; type?: string; credits?: number; balance_after?: number; created_at?: string }>; net_charged?: number; refunded?: boolean } | null>(null)
  const autoRefreshRef = useRef<ReturnType<typeof setInterval> | null>(null)

  function stopAutoRefresh() {
    if (autoRefreshRef.current) {
      clearInterval(autoRefreshRef.current)
      autoRefreshRef.current = null
    }
  }

  useEffect(() => () => stopAutoRefresh(), [])

  async function openDetail(id: number) {
    setDetailLoading(true)
    setBilling(null)
    stopAutoRefresh()
    try {
      const res = await userApi.getTask(id)
      const task: UserTask = (res as { task?: UserTask }).task ?? (res as UserTask)
      setDetail(task)
      userApi.getTaskBilling(id).then((b) => setBilling(b)).catch(() => null)
      const st = resolveStatus(task)
      if (st === 'pending' || st === 'processing') {
        autoRefreshRef.current = setInterval(async () => {
          const r = await userApi.getTask(id)
          const t: UserTask = (r as { task?: UserTask }).task ?? (r as UserTask)
          setDetail(t)
          if (resolveStatus(t) !== 'pending' && resolveStatus(t) !== 'processing') stopAutoRefresh()
        }, 3000)
      }
    } finally {
      setDetailLoading(false)
    }
  }

  function closeDetail() {
    stopAutoRefresh()
    setDetail(null)
    setBilling(null)
  }

  function doSearch() {
    const params: Record<string, unknown> = { page: 1, size: pageSize }
    if (filters.task_id) params.task_id = filters.task_id
    if (filters.type) params.type = filters.type
    if (filters.status) params.status = filters.status
    if (startAt) params.start_at = formatDateTimeFilterValue(startAt)
    if (endAt) params.end_at = formatDateTimeFilterValue(endAt)
    setPage(1)
    setQueryParams(params)
  }

  function resetFilters() {
    setFilters({ task_id: '', type: '', status: '' })
    setStartAt('')
    setEndAt('')
    setPage(1)
    setQueryParams({ page: 1, size: pageSize })
  }

  function changePage(next: number) {
    setPage(next)
    setQueryParams((prev) => ({ ...prev, page: next }))
  }

  const promptText = useMemo(() => firstString(detail?.request?.prompt, detail?.request?.input), [detail])
  const referenceImages = useMemo(() => collectReferenceImages(detail), [detail])
  const referenceVideos = useMemo(() => collectReferenceVideos(detail), [detail])
  const resultVideoUrls = useMemo(() => collectResultVideoUrls(detail), [detail])

  return (
    <>
      <PageHeader
        eyebrow="Jobs"
        title="任务中心"
        description="查看异步任务的状态、计费和结果详情。"
        actions={error ? <Button size="sm" variant="outline" onClick={reload}>重试</Button> : null}
      />
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardContent className="flex flex-wrap items-end gap-3 py-4">
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">任务 ID</label>
            <Input
              className="w-28"
              placeholder="任务 ID"
              value={filters.task_id}
              onChange={(e) => setFilters((f) => ({ ...f, task_id: e.target.value }))}
              onKeyDown={(e) => e.key === 'Enter' && doSearch()}
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-muted-foreground">类型</label>
            <Select value={filters.type || '_all'} onValueChange={(v) => setFilters((f) => ({ ...f, type: v === '_all' ? '' : v }))}>
              <SelectTrigger className="w-32"><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value="_all">全部类型</SelectItem>
                <SelectItem value="image">图片生成</SelectItem>
                <SelectItem value="video">视频生成</SelectItem>
                <SelectItem value="audio">音频生成</SelectItem>
                <SelectItem value="music">音乐生成</SelectItem>
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
                <SelectItem value="done">已完成</SelectItem>
                <SelectItem value="failed">失败</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <DateRangeFilter
            startAt={startAt}
            endAt={endAt}
            label="时间范围"
            onChange={({ startAt: s, endAt: e }) => {
              setStartAt(s)
              setEndAt(e)
            }}
          />
          <Button onClick={doSearch}>查询</Button>
          <Button variant="outline" onClick={resetFilters}>重置</Button>
        </CardContent>
      </Card>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-20">ID</TableHead>
              <TableHead className="w-28">类型</TableHead>
              <TableHead className="w-36">请求时间</TableHead>
              <TableHead className="w-32 text-right">消耗积分</TableHead>
              <TableHead className="w-28">状态</TableHead>
              <TableHead>错误信息</TableHead>
              <TableHead className="w-16 text-center">操作</TableHead>
            </TableRow>
          </TableHeader>
          {loading ? (
            <TableSkeleton cols={7} />
          ) : (
            <TableBody>
              {data.tasks.length === 0 ? (
                <TableEmpty
                  cols={7}
                  Icon={ListIcon}
                  title="还没有任务记录"
                  description="发起图片、视频、音频或音乐生成后，任务会显示在这里。"
                />
              ) : (
                data.tasks.map((row, index) => {
                  const taskId = row.id ?? row.task_id
                  const st = resolveStatus(row)
                  const errMsg = row.error_msg ?? row.msg
                  return (
                    <TableRow key={taskId ?? index}>
                      <TableCell>{taskId ?? '-'}</TableCell>
                      <TableCell>
                        <Badge variant="secondary" className="text-xs">
                          {typeLabel(row.type ?? row.task_type)}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-sm text-muted-foreground">
                        {row.created_at ? new Date(row.created_at).toLocaleString('zh-CN') : '-'}
                      </TableCell>
                      <TableCell className="text-right font-mono text-sm">
                        {row.credits_charged != null ? (
                          <span className="font-semibold text-red-500">-{(row.credits_charged / 1e6).toFixed(4)}</span>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </TableCell>
                      <TableCell>{statusBadge(st)}</TableCell>
                      <TableCell className="max-w-48 truncate text-xs text-red-500" title={errMsg}>
                        {errMsg ?? <span className="text-muted-foreground">-</span>}
                      </TableCell>
                      <TableCell className="text-center">
                        {taskId != null ? (
                          <Button size="sm" variant="link" className="h-auto p-0" onClick={() => openDetail(taskId)}>
                            详情
                          </Button>
                        ) : null}
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          )}
        </Table>
        {data.total > 0 ? (
          <CardContent className="flex flex-wrap items-center justify-between gap-3 border-t py-3">
            <span className="text-sm text-muted-foreground">共 {data.total} 条记录</span>
            <TablePagination current={page} total={data.total} pageSize={pageSize} onChange={changePage} className="py-0" />
          </CardContent>
        ) : null}
      </Card>

      <Dialog open={Boolean(detail)} onOpenChange={closeDetail}>
        <DialogContent className="max-h-[80vh] max-w-[960px] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>任务详情 #{detail?.id ?? detail?.task_id}</DialogTitle>
          </DialogHeader>
          {detailLoading ? (
            <div className="space-y-3 py-2">
              <div className="h-24 w-full animate-pulse rounded-lg border bg-muted/40" />
              <div className="h-32 w-full animate-pulse rounded-lg border bg-muted/40" />
              <div className="h-16 w-full animate-pulse rounded-lg border bg-muted/40" />
            </div>
          ) : detail ? (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-x-4 gap-y-2 rounded-lg border p-4 text-sm">
                <div><span className="text-muted-foreground">任务 ID：</span><strong>{detail.id ?? detail.task_id}</strong></div>
                <div><span className="text-muted-foreground">类型：</span>{typeLabel(detail.type ?? detail.task_type)}</div>
                <div className="col-span-2"><span className="text-muted-foreground">状态：</span>{statusBadge(resolveStatus(detail))}</div>
                <div className="col-span-2">
                  <span className="text-muted-foreground">消耗积分：</span>
                  {detail.credits_charged != null
                    ? <span className="font-semibold text-red-500">-{(detail.credits_charged / 1e6).toFixed(6)}</span>
                    : '-'}
                </div>
                <div className="col-span-2">
                  <span className="text-muted-foreground">创建时间：</span>
                  {detail.created_at ? new Date(detail.created_at).toLocaleString('zh-CN') : '-'}
                </div>
                {detail.finished_at ? (
                  <div className="col-span-2">
                    <span className="text-muted-foreground">完成时间：</span>
                    {new Date(detail.finished_at).toLocaleString('zh-CN')}
                  </div>
                ) : null}
                {detail.upstream_task_id ? (
                  <div className="col-span-2">
                    <span className="text-muted-foreground">上游任务 ID：</span>
                    <span className="font-mono text-xs">{detail.upstream_task_id}</span>
                  </div>
                ) : null}
                {(detail.error_msg ?? detail.msg) ? (
                  <div className="col-span-2">
                    <span className="text-muted-foreground">备注：</span>
                    <span className="text-red-500">{detail.error_msg ?? detail.msg}</span>
                  </div>
                ) : null}
              </div>

              {promptText ? (
                <MediaSection title="提示词">
                  <div className="rounded-lg bg-muted/40 p-3 text-sm leading-6 whitespace-pre-wrap break-all">{promptText}</div>
                </MediaSection>
              ) : null}

              {referenceImages.length > 0 ? (
                <MediaSection title="参考图片">
                  <div className="grid gap-3 sm:grid-cols-2">
                    {referenceImages.map((url) => (
                      <div key={url} className="overflow-hidden rounded-xl border border-border/70 bg-muted/20">
                        <img src={url} alt="reference" className="h-40 w-full object-cover" />
                        <div className="truncate px-3 py-2 text-xs text-muted-foreground">{url}</div>
                      </div>
                    ))}
                  </div>
                </MediaSection>
              ) : null}

              {referenceVideos.length > 0 ? (
                <MediaSection title="参考视频">
                  <div className="grid gap-3">
                    {referenceVideos.map((url) => (
                      <div key={url} className="overflow-hidden rounded-xl border border-border/70 bg-muted/20">
                        <video src={url} controls className="aspect-video w-full bg-black" />
                        <div className="truncate px-3 py-2 text-xs text-muted-foreground">{url}</div>
                      </div>
                    ))}
                  </div>
                </MediaSection>
              ) : null}

              {resultVideoUrls.length > 0 ? (
                <MediaSection title="结果视频">
                  <div className="grid gap-3">
                    {resultVideoUrls.map((url) => (
                      <div key={url} className="overflow-hidden rounded-xl border border-border/70 bg-muted/20">
                        <video src={url} controls className="aspect-video w-full bg-black" />
                        <div className="flex items-center justify-between gap-2 px-3 py-2">
                          <div className="truncate text-xs text-muted-foreground">{url}</div>
                          <a href={url} target="_blank" rel="noopener noreferrer" className="shrink-0 text-xs text-primary underline">
                            打开
                          </a>
                        </div>
                      </div>
                    ))}
                  </div>
                </MediaSection>
              ) : null}

              <JsonBlock title="请求参数" value={detail.request} />
              <JsonBlock title="结果" value={detail.result} />

              {billing?.transactions && billing.transactions.length > 0 ? (
                <div className="rounded-lg border">
                  <div className="border-b px-3 py-2 text-xs font-semibold text-muted-foreground">账单明细</div>
                  <table className="w-full text-xs">
                    <thead>
                      <tr className="border-b text-muted-foreground">
                        <th className="px-3 py-1.5 text-left">类型</th>
                        <th className="px-3 py-1.5 text-right">积分</th>
                        <th className="px-3 py-1.5 text-right">余额后</th>
                        <th className="px-3 py-1.5 text-right">时间</th>
                      </tr>
                    </thead>
                    <tbody>
                      {billing.transactions.map((tx, i) => (
                        <tr key={tx.id ?? i} className="border-b last:border-0">
                          <td className="px-3 py-1.5">{tx.type ?? '-'}</td>
                          <td className="px-3 py-1.5 text-right font-mono">{tx.credits != null ? (tx.credits / 1e6).toFixed(6) : '-'}</td>
                          <td className="px-3 py-1.5 text-right font-mono">{tx.balance_after != null ? (tx.balance_after / 1e6).toFixed(4) : '-'}</td>
                          <td className="px-3 py-1.5 text-right text-muted-foreground">{tx.created_at ? new Date(tx.created_at).toLocaleString('zh-CN') : '-'}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              ) : null}
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </>
  )
}
