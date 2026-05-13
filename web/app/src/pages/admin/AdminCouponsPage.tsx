import { useState } from 'react'
import { PlusIcon, Trash2Icon } from 'lucide-react'

import { PageHeader } from '@/components/shared/PageHeader'
import { TableEmpty } from '@/components/shared/TableEmpty'
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
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { TicketIcon } from 'lucide-react'
import { adminApi, type AdminCoupon } from '@/lib/api/admin'
import { useAsync } from '@/hooks/use-async'

function discountLabel(c: AdminCoupon) {
  if (c.discount_type === 'percent') return `${((c.discount_value ?? 0) / 100)}% 折扣`
  return `减 ¥${((c.discount_value ?? 0) / 100).toFixed(2)}`
}

export function AdminCouponsPage() {
  const [page, setPage] = useState(1)
  const pageSize = 20

  const { data, loading, error, reload } = useAsync(async () => {
    const res = await adminApi.listCoupons({ page, size: pageSize })
    return { coupons: res.coupons ?? [], total: res.total ?? 0 }
  }, { coupons: [] as AdminCoupon[], total: 0 }, [page])

  const [mutError, setMutError] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [usesOpen, setUsesOpen] = useState(false)
  const [selectedCoupon, setSelectedCoupon] = useState<AdminCoupon | null>(null)
  const [uses, setUses] = useState<{ id?: number; coupon_id?: number; user_id?: number; discount?: number; created_at?: string }[]>([])
  const [usesLoading, setUsesLoading] = useState(false)
  const [pendingVoidId, setPendingVoidId] = useState<number | null>(null)

  async function openUses(coupon: AdminCoupon) {
    if (!coupon.id) return
    setSelectedCoupon(coupon)
    setUsesOpen(true)
    setUsesLoading(true)
    try {
      const res = await adminApi.listCouponUses(coupon.id)
      setUses(res.uses ?? [])
    } catch { setUses([]) } finally { setUsesLoading(false) }
  }
  const [form, setForm] = useState({
    code: '',
    title: '',
    discount_type: 'fixed',
    discount_value: '',
    min_amount: '',
    max_discount: '',
    total_count: '',
    per_user_limit: '1',
    valid_from: '',
    valid_until: '',
  })

  async function handleCreate() {
    setMutError('')
    try {
      await adminApi.createCoupon({
        ...form,
        discount_value: form.discount_value ? parseFloat(form.discount_value) * 100 : undefined,
        min_amount: form.min_amount ? parseFloat(form.min_amount) * 100 : undefined,
        max_discount: form.max_discount ? parseFloat(form.max_discount) * 100 : undefined,
        total_count: form.total_count ? parseInt(form.total_count, 10) : undefined,
        per_user_limit: parseInt(form.per_user_limit, 10),
      })
      setCreateOpen(false)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    }
  }

  async function handleVoid(id: number) {
    setPendingVoidId(id)
  }

  async function executeVoid() {
    if (pendingVoidId == null) return
    setMutError('')
    try {
      await adminApi.voidCoupon(pendingVoidId)
      reload()
    } catch (err) {
      const { getApiErrorMessage } = await import('@/lib/api/http')
      setMutError(getApiErrorMessage(err))
    } finally {
      setPendingVoidId(null)
    }
  }

  const totalPages = Math.ceil(data.total / pageSize)

  return (
    <>
      <PageHeader
        eyebrow="Coupons"
        title="优惠券管理"
        description="创建和管理优惠券，支持固定金额减免和百分比折扣。"
        actions={
          <Button size="sm" onClick={() => { setCreateOpen(true); setMutError('') }}>
            <PlusIcon className="mr-1 size-3.5" />创建优惠券
          </Button>
        }
      />
      {error || mutError ? (
        <Alert variant="destructive">
          <AlertDescription>{String(error ?? mutError)}</AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-16">ID</TableHead>
              <TableHead>兑换码</TableHead>
              <TableHead>名称</TableHead>
              <TableHead className="w-28">优惠</TableHead>
              <TableHead className="w-28">最低消费</TableHead>
              <TableHead className="w-20 text-right">已用/总量</TableHead>
              <TableHead className="w-40">有效期</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          {loading ? <TableSkeleton cols={8} /> : (
            <TableBody>
              {data.coupons.length === 0 ? (
                <TableEmpty cols={8} Icon={TicketIcon} title="暂无优惠券" description="点击右上角创建。" />
              ) : data.coupons.map((c) => {
                const isVoided = c.valid_until != null && new Date(c.valid_until) < new Date()
                return (
                <TableRow key={c.id} className={isVoided ? 'opacity-50' : ''}>
                  <TableCell>{c.id}</TableCell>
                  <TableCell className="font-mono text-sm">{c.code}</TableCell>
                  <TableCell className="font-medium">{c.title}</TableCell>
                  <TableCell>
                    {isVoided
                      ? <Badge variant="secondary">已作废</Badge>
                      : <Badge variant="outline">{discountLabel(c)}</Badge>}
                  </TableCell>
                  <TableCell>{c.min_amount ? `¥${((c.min_amount ?? 0) / 100).toFixed(2)}` : '-'}</TableCell>
                  <TableCell className="text-right text-sm">
                    {c.used_count ?? 0} / {c.total_count ?? '∞'}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {c.valid_from ? new Date(c.valid_from).toLocaleDateString('zh-CN') : '—'}
                    {' ~ '}
                    {c.valid_until ? new Date(c.valid_until).toLocaleDateString('zh-CN') : '无限期'}
                  </TableCell>
                  <TableCell className="text-right">
                    {c.id != null ? (
                      <div className="flex justify-end gap-2">
                        <Button size="sm" variant="outline" onClick={() => openUses(c)}>使用记录</Button>
                        {!isVoided && (
                          <Button size="sm" variant="destructive" onClick={() => handleVoid(c.id!)}>
                            <Trash2Icon className="mr-1 size-3.5" />作废
                          </Button>
                        )}
                      </div>
                    ) : null}
                  </TableCell>
                </TableRow>
                )
              })}
            </TableBody>
          )}
        </Table>
        {totalPages > 1 ? (
          <CardContent className="flex items-center justify-between border-t py-3">
            <span className="text-sm text-muted-foreground">共 {data.total} 张</span>
            <div className="flex gap-2">
              <Button size="sm" variant="outline" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一页</Button>
              <span className="text-sm text-muted-foreground">{page} / {totalPages}</span>
              <Button size="sm" variant="outline" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>下一页</Button>
            </div>
          </CardContent>
        ) : null}
      </Card>

      {/* 使用记录 */}
      <Dialog open={usesOpen} onOpenChange={setUsesOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{selectedCoupon?.code ?? ''} 使用记录</DialogTitle>
          </DialogHeader>
          {usesLoading ? (
            <p className="py-4 text-center text-sm text-muted-foreground">加载中…</p>
          ) : uses.length === 0 ? (
            <p className="py-4 text-center text-sm text-muted-foreground">暂无使用记录</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户 ID</TableHead>
                  <TableHead className="text-right">优惠金额（¥）</TableHead>
                  <TableHead className="w-40">时间</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {uses.map((u, i) => (
                  <TableRow key={u.id ?? i}>
                    <TableCell>{u.user_id}</TableCell>
                    <TableCell className="text-right font-mono">¥{(((u.discount ?? 0)) / 100).toFixed(2)}</TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {u.created_at ? new Date(u.created_at).toLocaleString('zh-CN') : '-'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setUsesOpen(false)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>创建优惠券</DialogTitle>
          </DialogHeader>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <Label>兑换码</Label>
              <Input value={form.code} onChange={(e) => setForm(f => ({ ...f, code: e.target.value }))} placeholder="留空自动生成" />
            </div>
            <div className="space-y-1.5">
              <Label>名称</Label>
              <Input value={form.title} onChange={(e) => setForm(f => ({ ...f, title: e.target.value }))} placeholder="如：新用户专享" />
            </div>
            <div className="space-y-1.5">
              <Label>优惠类型</Label>
              <Select value={form.discount_type} onValueChange={(v) => setForm(f => ({ ...f, discount_type: v }))}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="fixed">固定减免（¥）</SelectItem>
                  <SelectItem value="percent">百分比折扣（%）</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1.5">
              <Label>{form.discount_type === 'fixed' ? '减免金额（¥）' : '折扣百分比'}</Label>
              <Input value={form.discount_value} onChange={(e) => setForm(f => ({ ...f, discount_value: e.target.value }))} placeholder={form.discount_type === 'fixed' ? '如：10' : '如：20（表示80折）'} />
            </div>
            <div className="space-y-1.5">
              <Label>最低消费（¥）</Label>
              <Input value={form.min_amount} onChange={(e) => setForm(f => ({ ...f, min_amount: e.target.value }))} placeholder="0 = 无限制" />
            </div>
            <div className="space-y-1.5">
              <Label>最大优惠（¥）</Label>
              <Input value={form.max_discount} onChange={(e) => setForm(f => ({ ...f, max_discount: e.target.value }))} placeholder="留空不限" />
            </div>
            <div className="space-y-1.5">
              <Label>发放总量</Label>
              <Input value={form.total_count} onChange={(e) => setForm(f => ({ ...f, total_count: e.target.value }))} placeholder="留空不限" />
            </div>
            <div className="space-y-1.5">
              <Label>每人限用次数</Label>
              <Input value={form.per_user_limit} onChange={(e) => setForm(f => ({ ...f, per_user_limit: e.target.value }))} />
            </div>
            <div className="space-y-1.5">
              <Label>有效期开始</Label>
              <Input type="datetime-local" value={form.valid_from} onChange={(e) => setForm(f => ({ ...f, valid_from: e.target.value }))} />
            </div>
            <div className="space-y-1.5">
              <Label>有效期结束</Label>
              <Input type="datetime-local" value={form.valid_until} onChange={(e) => setForm(f => ({ ...f, valid_until: e.target.value }))} />
            </div>
          </div>
          {mutError ? <p className="text-sm text-destructive">{mutError}</p> : null}
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button>
            <Button onClick={handleCreate}>创建</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={pendingVoidId != null} onOpenChange={() => setPendingVoidId(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认作废</AlertDialogTitle>
            <AlertDialogDescription>
              确认作废此优惠券吗？作废后持有该优惠码的用户将无法使用，此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={executeVoid}>确认作废</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
