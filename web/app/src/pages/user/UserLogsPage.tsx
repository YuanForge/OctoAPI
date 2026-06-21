import { useState } from 'react'
import { FileClockIcon, Loader2, Search } from 'lucide-react'

import { useAsync } from '@/hooks/use-async'
import { DateRangeFilter, formatDateTimeFilterValue } from '@/components/shared/DateRangeFilter'
import { PageHeader } from '@/components/shared/PageHeader'
import { TableEmpty } from '@/components/shared/TableEmpty'
import { TablePagination } from '@/components/shared/TablePagination'
import { TableSkeleton } from '@/components/shared/TableSkeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { NativeSelect } from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { userApi, type UserLog } from '@/lib/api/user'
import { formatCredits, formatTokenPricePerMillion } from '@/lib/formatters/credits'

function renderStatus(status?: string) {
  if (status === 'ok') {
    return <Badge variant="secondary" className="bg-green-100 text-green-800">成功</Badge>
  }
  if (status === 'error') {
    return <Badge variant="destructive">失败</Badge>
  }
  if (status === 'refunded') {
    return <Badge variant="outline" className="border-orange-200 text-orange-600">已退款</Badge>
  }
  if (status === 'pending') {
    return <Badge variant="secondary">进行中</Badge>
  }
  return <Badge variant="outline">{status ?? '-'}</Badge>
}

function renderTokenPrice(value?: number | null) {
  if (value == null) {
    return <span className="text-muted-foreground/50">-</span>
  }
  return <span className="text-sm">{formatTokenPricePerMillion(value)}</span>
}

export function UserLogsPage() {
  const [page, setPage] = useState(1)
  const pageSize = 20
  const [filters, setFilters] = useState({ model: '', status: '', startAt: '', endAt: '' })
  const [queryParams, setQueryParams] = useState<Record<string, string | number>>({ page: 1, page_size: pageSize })

  const { data, loading, error, reload } = useAsync(async () => {
    const res = await userApi.listLogs(queryParams)
    return {
      logs: (Array.isArray(res) ? res : res.items ?? res.logs ?? []) as UserLog[],
      total: (res && !Array.isArray(res) ? res.total : 0) as number,
    }
  }, { logs: [] as UserLog[], total: 0 }, [queryParams])

  const rows = data.logs
  const total = data.total

  const [drawerOpen, setDrawerOpen] = useState(false)
  const [currentLog, setCurrentLog] = useState<UserLog | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  async function openDetail(basicLog: UserLog) {
    setDrawerOpen(true)
    setDetailLoading(true)
    setCurrentLog({ ...basicLog })
    try {
      const res = await userApi.getLog(basicLog.id!)
      setCurrentLog({
        ...basicLog,
        ...res,
        credits_charged: basicLog.credits_charged ?? basicLog.cost_credits,
      })
    } catch (detailError) {
      console.error(detailError)
    } finally {
      setDetailLoading(false)
    }
  }

  function handleSearch() {
    const params: Record<string, string | number> = { page: 1, page_size: pageSize }
    if (filters.model) params.model = filters.model
    if (filters.status) params.status = filters.status
    if (filters.startAt) params.start_at = formatDateTimeFilterValue(filters.startAt)
    if (filters.endAt) params.end_at = formatDateTimeFilterValue(filters.endAt)
    setPage(1)
    setQueryParams(params)
  }

  function handleReset() {
    setFilters({ model: '', status: '', startAt: '', endAt: '' })
    setPage(1)
    setQueryParams({ page: 1, page_size: pageSize })
  }

  function changePage(next: number) {
    setPage(next)
    setQueryParams((current) => ({ ...current, page: next }))
  }

  return (
    <>
      <PageHeader
        eyebrow="Observability"
        title="调用日志"
        description="查看带有筛选、分页和详情的完整 API 调用记录。"
        actions={error ? <Button size="sm" variant="outline" onClick={reload}>重试</Button> : null}
      />
      {error ? (
        <Alert variant="destructive" className="mb-4">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <Card className="mb-4">
        <CardContent className="pt-6">
          <div className="flex flex-wrap items-center gap-3">
            <Input
              placeholder="模型名称"
              value={filters.model}
              onChange={(event) => setFilters({ ...filters, model: event.target.value })}
              className="w-[180px]"
              onKeyDown={(event) => event.key === 'Enter' && handleSearch()}
            />
            <NativeSelect
              value={filters.status}
              onChange={(event) => setFilters({ ...filters, status: event.target.value })}
              className="w-[140px]"
            >
              <option value="">全部状态</option>
              <option value="ok">成功 (ok)</option>
              <option value="error">失败 (error)</option>
              <option value="refunded">已退款 (refunded)</option>
              <option value="pending">进行中 (pending)</option>
            </NativeSelect>
            <DateRangeFilter
              startAt={filters.startAt}
              endAt={filters.endAt}
              onChange={({ startAt, endAt }) => setFilters({ ...filters, startAt, endAt })}
            />
            <Button onClick={handleSearch}>
              <Search className="mr-2 h-4 w-4" />
              查询
            </Button>
            <Button variant="outline" onClick={handleReset}>重置</Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>模型</TableHead>
              <TableHead>请求时间</TableHead>
              <TableHead className="text-right">输入 Tokens</TableHead>
              <TableHead className="text-right">输出 Tokens</TableHead>
              <TableHead className="text-right">输入价格</TableHead>
              <TableHead className="text-right">输出价格</TableHead>
              <TableHead className="text-right">消耗积分</TableHead>
              <TableHead className="text-center">状态</TableHead>
              <TableHead className="text-center">操作</TableHead>
            </TableRow>
          </TableHeader>
          {loading ? (
            <TableSkeleton cols={9} rows={8} />
          ) : (
            <TableBody>
              {rows.length === 0 ? (
                <TableEmpty
                  cols={9}
                  Icon={FileClockIcon}
                  title="还没有调用日志"
                  description="使用 API 密钥发起 LLM 请求后，调用记录会展示在这里。"
                />
              ) : (
                rows.map((row, index) => (
                  <TableRow key={row.id ?? index}>
                    <TableCell className="max-w-[200px] truncate font-medium" title={row.model}>
                      {row.model ?? '-'}
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {row.created_at ? new Date(row.created_at).toLocaleString('zh-CN') : '-'}
                    </TableCell>
                    <TableCell className="text-right">
                      {row.usage?.prompt_tokens != null ? (
                        <div className="text-sm">
                          {row.usage.prompt_tokens.toLocaleString()}
                          {row.usage.cache_read_tokens ? (
                            <div className="mt-1 text-[10px] leading-tight text-muted-foreground">
                              命中 {row.usage.cache_read_tokens.toLocaleString()}
                            </div>
                          ) : null}
                          {row.usage.cache_creation_tokens ? (
                            <div className="text-[10px] leading-tight text-muted-foreground">
                              写入 {row.usage.cache_creation_tokens.toLocaleString()}
                            </div>
                          ) : null}
                        </div>
                      ) : (
                        <span className="text-muted-foreground/50">-</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      {row.usage?.completion_tokens != null ? (
                        <div className="flex flex-col items-end gap-1">
                          <span className="text-sm">{row.usage.completion_tokens.toLocaleString()}</span>
                          {row.usage.estimated ? (
                            <Badge variant="outline" className="h-4 border-orange-200 px-1 py-0 text-[10px] text-orange-600">
                              估算
                            </Badge>
                          ) : null}
                        </div>
                      ) : (
                        <span className="text-muted-foreground/50">-</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">{renderTokenPrice(row.input_price_per_1m_tokens)}</TableCell>
                    <TableCell className="text-right">{renderTokenPrice(row.output_price_per_1m_tokens)}</TableCell>
                    <TableCell className="text-right">
                      {(row.credits_charged ?? row.cost_credits) ? (
                        <span className="font-semibold text-red-500">
                          -{formatCredits(row.credits_charged ?? row.cost_credits)}
                        </span>
                      ) : (
                        <span className="text-muted-foreground/50">-</span>
                      )}
                    </TableCell>
                    <TableCell className="text-center">{renderStatus(row.status)}</TableCell>
                    <TableCell className="text-center">
                      <Button variant="ghost" size="sm" onClick={() => openDetail(row)}>详情</Button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          )}
        </Table>
        {total > 0 ? (
          <div className="flex flex-wrap items-center justify-between gap-3 border-t px-4 py-4">
            <div className="text-sm text-muted-foreground">共 {total} 条数据</div>
            <TablePagination current={page} total={total} pageSize={pageSize} onChange={changePage} className="py-0" />
          </div>
        ) : null}
      </Card>

      <Sheet open={drawerOpen} onOpenChange={setDrawerOpen}>
        <SheetContent className="w-[min(96vw,1160px)] overflow-y-auto sm:max-w-[1160px]">
          <SheetHeader className="mb-6">
            <SheetTitle>日志详情</SheetTitle>
          </SheetHeader>
          {detailLoading ? (
            <div className="flex justify-center py-10">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
          ) : currentLog ? (
            <div className="space-y-6">
              <div className="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <div className="mb-1 text-muted-foreground">ID</div>
                  <div className="font-mono text-xs">{currentLog.id}</div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">状态</div>
                  <div>{renderStatus(currentLog.status)}</div>
                </div>
                <div className="col-span-2">
                  <div className="mb-1 text-muted-foreground">模型</div>
                  <div className="font-medium">{currentLog.model}</div>
                </div>
                <div className="col-span-2">
                  <div className="mb-1 text-muted-foreground">Corr ID</div>
                  <div className="break-all font-mono text-xs">{currentLog.corr_id}</div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">输入 Tokens</div>
                  <div>{currentLog.usage?.prompt_tokens ?? '-'}</div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">
                    输出 Tokens
                    {currentLog.usage?.estimated ? (
                      <Badge variant="outline" className="ml-1 h-4 py-0 text-[10px]">估算</Badge>
                    ) : null}
                  </div>
                  <div>{currentLog.usage?.completion_tokens ?? '-'}</div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">输入价格</div>
                  <div>{formatTokenPricePerMillion(currentLog.input_price_per_1m_tokens)}</div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">输出价格</div>
                  <div>{formatTokenPricePerMillion(currentLog.output_price_per_1m_tokens)}</div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">消耗积分</div>
                  <div className={(currentLog.credits_charged ?? currentLog.cost_credits) ? 'font-medium text-red-500' : ''}>
                    {(currentLog.credits_charged ?? currentLog.cost_credits)
                      ? `-${formatCredits(currentLog.credits_charged ?? currentLog.cost_credits)}`
                      : '-'}
                  </div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">流式</div>
                  <div>{currentLog.is_stream ? '是' : '否'}</div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">请求时间</div>
                  <div>{currentLog.created_at ? new Date(currentLog.created_at).toLocaleString('zh-CN') : '-'}</div>
                </div>
                <div>
                  <div className="mb-1 text-muted-foreground">完成时间</div>
                  <div>{currentLog.status !== 'pending' && currentLog.updated_at ? new Date(currentLog.updated_at).toLocaleString('zh-CN') : '-'}</div>
                </div>
              </div>

              {currentLog.error_msg ? (
                <div>
                  <div className="mb-2 text-sm font-semibold text-red-600">错误信息</div>
                  <div className="rounded-md bg-red-50 p-3 text-sm whitespace-pre-wrap text-red-900">
                    {currentLog.error_msg}
                  </div>
                </div>
              ) : null}

              {currentLog.client_request ? (
                <div>
                  <div className="mb-2 text-sm font-semibold">您的请求</div>
                  <pre className="max-h-[300px] overflow-x-auto break-all rounded-md bg-zinc-950 p-4 font-mono text-xs whitespace-pre-wrap text-zinc-50">
                    {JSON.stringify(currentLog.client_request, null, 2)}
                  </pre>
                </div>
              ) : null}

              {currentLog.upstream_headers ? (
                <div>
                  <div className="mb-2 text-sm font-semibold">上游请求头</div>
                  <pre className="max-h-[200px] overflow-x-auto break-all rounded-md bg-zinc-950 p-4 font-mono text-xs whitespace-pre-wrap text-zinc-50">
                    {JSON.stringify(currentLog.upstream_headers, null, 2)}
                  </pre>
                </div>
              ) : null}

              {currentLog.upstream_request ? (
                <div>
                  <div className="mb-2 text-sm font-semibold">上游请求体</div>
                  <pre className="max-h-[300px] overflow-x-auto break-all rounded-md bg-zinc-950 p-4 font-mono text-xs whitespace-pre-wrap text-zinc-50">
                    {JSON.stringify(currentLog.upstream_request, null, 2)}
                  </pre>
                </div>
              ) : null}

              {currentLog.upstream_response ? (
                <div>
                  <div className="mb-2 text-sm font-semibold">上游响应</div>
                  <pre className="max-h-[300px] overflow-x-auto break-all rounded-md bg-zinc-950 p-4 font-mono text-xs whitespace-pre-wrap text-zinc-50">
                    {JSON.stringify(currentLog.upstream_response, null, 2)}
                  </pre>
                </div>
              ) : null}

              {currentLog.client_response ? (
                <div>
                  <div className="mb-2 text-sm font-semibold">返回给您的响应</div>
                  <pre className="max-h-[300px] overflow-x-auto break-all rounded-md bg-zinc-950 p-4 font-mono text-xs whitespace-pre-wrap text-zinc-50">
                    {JSON.stringify(currentLog.client_response, null, 2)}
                  </pre>
                </div>
              ) : null}
            </div>
          ) : null}
        </SheetContent>
      </Sheet>
    </>
  )
}
