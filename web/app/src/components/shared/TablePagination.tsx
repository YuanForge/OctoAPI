import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ChevronsLeft, ChevronsRight } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'

interface TablePaginationProps {
  current: number
  total: number
  pageSize: number
  onChange: (page: number) => void
  className?: string
  jumpStep?: number
  maxActionsPerSecond?: number
}

function clampPage(page: number, maxPage: number) {
  if (!Number.isFinite(page)) return 1
  return Math.min(Math.max(Math.trunc(page), 1), maxPage)
}

export function TablePagination({
  current,
  total,
  pageSize,
  onChange,
  className,
  jumpStep = 5,
  maxActionsPerSecond = 10,
}: TablePaginationProps) {
  const maxPage = Math.ceil(total / pageSize) || 1
  const currentPage = clampPage(current, maxPage)
  const [rateLimited, setRateLimited] = useState(false)
  const pageInputRef = useRef<HTMLInputElement>(null)
  const actionTimesRef = useRef<number[]>([])
  const releaseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    return () => {
      if (releaseTimerRef.current) {
        clearTimeout(releaseTimerRef.current)
      }
    }
  }, [])

  function canRunAction() {
    const now = Date.now()
    const recent = actionTimesRef.current.filter((time) => now - time < 1000)

    if (recent.length >= maxActionsPerSecond) {
      const waitMs = Math.max(100, 1000 - (now - recent[0]))
      actionTimesRef.current = recent
      setRateLimited(true)
      if (releaseTimerRef.current) {
        clearTimeout(releaseTimerRef.current)
      }
      releaseTimerRef.current = setTimeout(() => setRateLimited(false), waitMs)
      return false
    }

    actionTimesRef.current = [...recent, now]
    return true
  }

  function changePage(next: number) {
    const target = clampPage(next, maxPage)
    if (target === currentPage || !canRunAction()) return
    onChange(target)
  }

  function submitPage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    changePage(Number(pageInputRef.current?.value))
  }

  const actionsDisabled = rateLimited || total <= 0

  return (
    <div className={cn('flex flex-wrap items-center justify-end gap-2 py-4', className)}>
      <div className="text-sm text-muted-foreground">
        第 {currentPage} 页 / 共 {maxPage} 页
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          onClick={() => changePage(currentPage - jumpStep)}
          disabled={actionsDisabled || currentPage <= 1}
        >
          <ChevronsLeft className="h-4 w-4" />
          前 {jumpStep} 页
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => changePage(currentPage - 1)}
          disabled={actionsDisabled || currentPage <= 1}
        >
          上一页
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => changePage(currentPage + 1)}
          disabled={actionsDisabled || currentPage >= maxPage}
        >
          下一页
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => changePage(currentPage + jumpStep)}
          disabled={actionsDisabled || currentPage >= maxPage}
        >
          后 {jumpStep} 页
          <ChevronsRight className="h-4 w-4" />
        </Button>
      </div>
      <form className="flex flex-wrap items-center gap-2" onSubmit={submitPage}>
        <span className="text-sm text-muted-foreground">跳至</span>
        <Input
          key={currentPage}
          ref={pageInputRef}
          type="number"
          min={1}
          max={maxPage}
          defaultValue={currentPage}
          className="w-20"
        />
        <span className="text-sm text-muted-foreground">页</span>
        <Button type="submit" variant="outline" size="sm" disabled={actionsDisabled}>
          查询
        </Button>
      </form>
      {rateLimited ? <div className="basis-full text-right text-xs text-muted-foreground">操作太快，请稍后再试</div> : null}
    </div>
  )
}
