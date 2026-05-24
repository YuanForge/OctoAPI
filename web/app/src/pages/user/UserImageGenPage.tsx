import { useEffect, useRef, useState } from 'react'
import { toast } from 'sonner'

import { PageHeader } from '@/components/shared/PageHeader'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { buildUserInvokeHeaders, canInvokeWithSelectedKey } from '@/lib/api/request-auth'
import { userApi, type ApiKeyRecord, type UserChannel, type UserTask } from '@/lib/api/user'

type ImageGenerateResponse = {
  task_id?: number | string
  status?: string | number
  msg?: string
  error_msg?: string
  url?: unknown
  urls?: unknown
  items?: unknown[]
  data?: Array<{ url?: unknown }>
  result?: Record<string, unknown>
}

const imageBase64Signatures: Array<[string, string]> = [
  ['iVBORw0KGgo', 'image/png'],
  ['/9j/', 'image/jpeg'],
  ['R0lGOD', 'image/gif'],
  ['UklGR', 'image/webp'],
  ['PHN2Zy', 'image/svg+xml'],
  ['PD94bWw', 'image/svg+xml'],
]

function detectBase64ImageMime(value: string) {
  return imageBase64Signatures.find(([signature]) => value.startsWith(signature))?.[1] ?? 'image/png'
}

function isLikelyBase64Image(value: string) {
  if (value.length < 64 || value.length % 4 === 1) return false
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(value)) return false
  return imageBase64Signatures.some(([signature]) => value.startsWith(signature))
}

function normalizeImageSrc(value: unknown) {
  if (typeof value !== 'string') return ''
  const trimmed = value.trim()
  if (!trimmed) return ''
  if (/^[a-z][a-z\d+.-]*:/i.test(trimmed) || trimmed.startsWith('/') || trimmed.startsWith('./') || trimmed.startsWith('../')) {
    return trimmed
  }

  const compact = trimmed.replace(/\s/g, '')
  if (!isLikelyBase64Image(compact)) return trimmed
  return `data:${detectBase64ImageMime(compact)};base64,${compact}`
}

function openImageUrl(url: string) {
  if (url.startsWith('data:')) {
    const [header, base64] = url.split(',')
    const mime = header.replace('data:', '').replace(';base64', '')
    const bytes = atob(base64)
    const arr = new Uint8Array(bytes.length)
    for (let i = 0; i < bytes.length; i++) arr[i] = bytes.charCodeAt(i)
    const blob = new Blob([arr], { type: mime })
    const blobUrl = URL.createObjectURL(blob)
    const win = window.open(blobUrl, '_blank')
    if (win) win.addEventListener('unload', () => URL.revokeObjectURL(blobUrl))
  } else {
    window.open(url, '_blank', 'noopener,noreferrer')
  }
}

function collectImageSources(...values: unknown[]) {
  const sources: string[] = []

  const append = (value: unknown) => {
    if (Array.isArray(value)) {
      value.forEach((item) => {
        if (typeof item === 'string') {
          append(item)
        } else if (item && typeof item === 'object') {
          append((item as { url?: unknown }).url)
        }
      })
      return
    }

    const source = normalizeImageSrc(value)
    if (source) sources.push(source)
  }

  values.forEach(append)
  return Array.from(new Set(sources))
}

export function UserImageGenPage() {
  const [apiKeys, setApiKeys] = useState<ApiKeyRecord[]>([])
  const [channels, setChannels] = useState<UserChannel[]>([])
  const [selectedKeyId, setSelectedKeyId] = useState<number | undefined>()
  const [selectedChannelId, setSelectedChannelId] = useState<number | undefined>()
  const [error, setError] = useState('')
  const [prompt, setPrompt] = useState('')
  const [size, setSize] = useState('1k')
  const [aspectRatio, setAspectRatio] = useState('1:1')
  const [referenceImages, setReferenceImages] = useState('')
  const [taskId, setTaskId] = useState('')
  const [taskStatus, setTaskStatus] = useState<'idle' | 'polling' | 'done' | 'failed'>('idle')
  const [taskError, setTaskError] = useState('')
  const [images, setImages] = useState<string[]>([])
  const [running, setRunning] = useState(false)
  const [uploadingReference, setUploadingReference] = useState(false)
  const referenceUploadRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    async function load() {
      try {
        const [keysRes, channelsRes] = await Promise.all([
          userApi.listApiKeys(),
          userApi.listChannels(),
        ])
        const nextKeys = Array.isArray(keysRes) ? keysRes : keysRes.api_keys ?? keysRes.keys ?? []
        const nextChannels = (Array.isArray(channelsRes) ? channelsRes : channelsRes.channels ?? []).filter(
          (item) => item.type === 'image' || item.billing_type === 'image'
        )
        setApiKeys(nextKeys)
        setChannels(nextChannels)
        if (nextKeys.length > 0) setSelectedKeyId(nextKeys[0].id)
        if (nextChannels.length > 0) setSelectedChannelId(nextChannels[0].id)
      } catch {
        setError('读取图片渠道或 API 密钥失败')
      }
    }

    void load()
  }, [])

  const [historyTasks, setHistoryTasks] = useState<UserTask[]>([])

  async function loadHistory() {
    try {
      const res = await userApi.listTasks({ type: 'image', status: 'done', size: 20 })
      const tasks = Array.isArray(res) ? res : (res.tasks ?? res.items ?? [])
      setHistoryTasks(tasks)
    } catch { /* ignore */ }
  }

  async function clearHistory() {
    try {
      await userApi.clearTaskHistory('image')
      setHistoryTasks([])
    } catch (err) {
      const message = err instanceof Error ? err.message : '清空历史失败'
      setError(message)
      toast.error(message)
    }
  }

  useEffect(() => { void loadHistory() }, [])

  function canInvoke() {
    return canInvokeWithSelectedKey(apiKeys, selectedKeyId)
  }

  // 异步任务轮询：task_id 存在且处于 polling 状态时每 3s 查询一次
  useEffect(() => {
    if (!taskId || taskStatus !== 'polling') return
    const authHeaders = buildUserInvokeHeaders(apiKeys, selectedKeyId)
    if (!authHeaders) return
    let cancelled = false

    const tick = async () => {
      try {
        const resp = await fetch(`/v1/tasks/${taskId}`, {
          headers: authHeaders,
        })
        if (!resp.ok) return
        const data = await resp.json() as ImageGenerateResponse
        if (cancelled) return
        const st = data.status
        if (st === 'done' || st === 2) {
          const result = data.result ?? {}
          const urlList = collectImageSources(result.data, result.urls, result.url, data.urls, data.url, data.items)
          setImages(urlList)
          setTaskStatus('done')
          setRunning(false)
          void loadHistory()
        } else if (st === 'failed' || st === 3) {
          setTaskError(data.error_msg ?? data.msg ?? '生成失败')
          setTaskStatus('failed')
          setRunning(false)
        }
      } catch {
        // 忽略单次轮询失败
      }
    }

    const timer = setInterval(() => { void tick() }, 3000)
    void tick()
    return () => { cancelled = true; clearInterval(timer) }
  }, [taskId, taskStatus, apiKeys, selectedKeyId])

  function currentChannel() {
    return channels.find((item) => item.id === selectedChannelId) ?? channels[0]
  }

  function referenceUrls() {
    return referenceImages
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
  }

  async function uploadReferenceFiles(fileList: FileList | null) {
    const files = Array.from(fileList ?? [])
    if (files.length === 0) {
      return
    }

    setUploadingReference(true)
    setError('')
    try {
      const uploadedUrls: string[] = []
      for (const file of files) {
        const response = await userApi.uploadImage(file, 'reference')
        if (response.url) {
          uploadedUrls.push(response.url)
        }
      }
      if (uploadedUrls.length === 0) {
        throw new Error('上传失败，未返回图片地址')
      }
      setReferenceImages((current) => {
        const merged = [...current.split('\n').map((line) => line.trim()).filter(Boolean), ...uploadedUrls]
        return merged.join('\n')
      })
      toast.success(`已上传 ${uploadedUrls.length} 张参考图`)
    } catch (err) {
      const message = err instanceof Error ? err.message : '参考图上传失败'
      setError(message)
      toast.error(message)
    } finally {
      setUploadingReference(false)
    }
  }

  async function generate() {
    if (!prompt.trim()) return
    const authHeaders = buildUserInvokeHeaders(apiKeys, selectedKeyId)
    if (!authHeaders) {
      setError('请选择可用的 API 密钥')
      return
    }
    if (!selectedChannelId && channels.length === 0) {
      setError('当前没有可用的图片模型渠道')
      return
    }
    setRunning(true)
    setImages([])
    setTaskId('')
    setTaskStatus('idle')
    setTaskError('')
    setError('')
    try {
      const endpoint = currentChannel()?.id
        ? `/v1/image?channel_id=${currentChannel()?.id}`
        : '/v1/image'
      const refUrls = referenceUrls()
      const body: Record<string, unknown> = {
        model: currentChannel()?.routing_model || currentChannel()?.name,
        prompt,
        size,
        aspect_ratio: aspectRatio,
      }
      if (refUrls.length > 0) body.refer_images = refUrls
      const response = await fetch(endpoint, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...authHeaders,
        },
        body: JSON.stringify(body),
      })
      if (!response.ok) {
        throw new Error((await response.text()) || `请求失败 (${response.status})`)
      }
      const data = await response.json() as ImageGenerateResponse
      const syncImages = collectImageSources(data.data, data.urls, data.url, data.items, data.result?.data, data.result?.urls, data.result?.url)
      if (syncImages.length > 0) {
        setImages(syncImages)
        setTaskStatus('done')
        setRunning(false)
      } else if (data.task_id) {
        setTaskId(String(data.task_id))
        setTaskStatus('polling')
      } else if (data.status === 'failed' || data.status === 3) {
        throw new Error(data.error_msg ?? data.msg ?? '生成失败')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '图片生成失败')
    } finally {
      setRunning(false)
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Image"
        title="图片生成"
        description="接入真实 `/v1/image` 接口，支持基础参数提交和任务返回。"
      />
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div className="grid gap-4 xl:grid-cols-[320px_1fr] 2xl:grid-cols-[320px_1fr_240px]">
        <Card>
          <CardContent className="flex flex-col gap-4 p-6">
            <div className="grid gap-1.5">
              <Label>API 密钥 <span className="text-destructive">*</span></Label>
              <NativeSelect value={selectedKeyId} onChange={(event) => setSelectedKeyId(Number(event.target.value))}>
                {apiKeys.map((key) => (
                  <option key={key.id} value={key.id}>{key.name || key.masked_key || key.key}</option>
                ))}
              </NativeSelect>
              <p className="text-xs text-muted-foreground">已登录用户可直接使用已创建的密钥进行调用。</p>
            </div>
            <div className="grid gap-1.5">
              <Label>模型 <span className="text-muted-foreground font-normal">(选填)</span></Label>
              <NativeSelect value={selectedChannelId} onChange={(event) => setSelectedChannelId(Number(event.target.value))}>
                {channels.map((channel) => (
                  <option key={channel.id} value={channel.id}>{channel.name}</option>
                ))}
              </NativeSelect>
              {channels.length === 0 ? (
                <p className="text-xs text-muted-foreground">当前没有可用的图片模型渠道。</p>
              ) : null}
            </div>
            <div className="grid gap-1.5">
              <Label>提示词 <span className="text-destructive">*</span></Label>
              <Textarea
                rows={5}
                value={prompt}
                onChange={(event) => setPrompt(event.target.value)}
                placeholder="描述你想生成的图片内容..."
              />
            </div>
            <div className="grid gap-1.5">
              <Label>分辨率档位</Label>
              <NativeSelect value={size} onChange={(event) => setSize(event.target.value)}>
                <option value="1k">1k (1024px)</option>
                <option value="2k">2k (2048px)</option>
                <option value="3k">3k (3072px)</option>
                <option value="4k">4k (4096px)</option>
              </NativeSelect>
            </div>
            <div className="grid gap-1.5">
              <Label>宽高比</Label>
              <NativeSelect value={aspectRatio} onChange={(event) => setAspectRatio(event.target.value)}>
                <option value="1:1">1:1 方图</option>
                <option value="16:9">16:9 横版</option>
                <option value="9:16">9:16 竖版</option>
                <option value="4:3">4:3</option>
                <option value="3:4">3:4</option>
                <option value="3:2">3:2</option>
                <option value="2:3">2:3</option>
                <option value="21:9">21:9 超宽</option>
              </NativeSelect>
            </div>
            <div className="grid gap-1.5">
              <div className="flex items-center justify-between gap-2">
                <Label>参考图 URL <span className="text-muted-foreground font-normal">(选填，每行一条)</span></Label>
                <div className="flex items-center gap-2">
                  <input
                    ref={referenceUploadRef}
                    type="file"
                    accept="image/*"
                    multiple
                    className="hidden"
                    onChange={(event) => {
                      void uploadReferenceFiles(event.target.files)
                      event.target.value = ''
                    }}
                  />
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => referenceUploadRef.current?.click()}
                    disabled={uploadingReference}
                  >
                    {uploadingReference ? '上传中...' : '本地上传'}
                  </Button>
                </div>
              </div>
              <Textarea
                rows={3}
                value={referenceImages}
                onChange={(event) => setReferenceImages(event.target.value)}
                placeholder={'https://example.com/ref1.png\nhttps://example.com/ref2.png'}
              />
              {referenceUrls().length > 0 ? (
                <div className="grid gap-3 sm:grid-cols-2">
                  {referenceUrls().map((url) => (
                    <div key={url} className="overflow-hidden rounded-xl border border-border/70 bg-muted/20">
                      <img src={url} alt="reference" className="h-36 w-full object-cover" />
                      <div className="truncate px-3 py-2 text-xs text-muted-foreground">{url}</div>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>
            <Button onClick={generate} disabled={running || !prompt.trim() || !canInvoke() || channels.length === 0}>
              {running ? '生成中...' : '生成图片'}
            </Button>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="flex flex-col gap-4 p-6">
            {taskStatus === 'polling' ? (
              <Alert>
                <AlertDescription>
                  生成中，任务 ID：<span className="font-mono">{taskId}</span>，完成后将自动展示。
                </AlertDescription>
              </Alert>
            ) : null}
            {taskStatus === 'failed' && taskError ? (
              <Alert variant="destructive">
                <AlertDescription>{taskError}</AlertDescription>
              </Alert>
            ) : null}
            {taskId && taskStatus !== 'polling' ? (
              <p className="text-xs text-muted-foreground">任务 ID：{taskId}</p>
            ) : null}
            {images.length > 0 ? (
              <div className="grid gap-4 md:grid-cols-2">
                {images.map((url, index) => (
                  <a key={`${index}-${url.slice(0, 64)}`} href="#" onClick={(e) => { e.preventDefault(); openImageUrl(url) }}>
                    <img className="rounded-xl border border-border/70 w-full" src={url} alt="generated" />
                  </a>
                ))}
              </div>
            ) : taskStatus === 'idle' ? (
              <p className="text-sm text-muted-foreground">提交后将在这里展示结果。</p>
            ) : null}
          </CardContent>
        </Card>
        <Card className="hidden 2xl:flex flex-col overflow-hidden">
          <div className="flex items-center justify-between border-b px-4 py-3 shrink-0">
            <span className="text-sm font-semibold">历史生成</span>
            <div className="flex items-center gap-2">
              {historyTasks.length > 0 && (
                <button type="button" onClick={() => void clearHistory()} className="text-xs text-muted-foreground hover:text-destructive">清空</button>
              )}
              <button type="button" onClick={() => void loadHistory()} className="text-xs text-muted-foreground hover:text-foreground">刷新</button>
            </div>
          </div>
          <div className="flex-1 overflow-y-auto p-2">
            {historyTasks.length === 0 ? (
              <p className="py-10 text-center text-xs text-muted-foreground">暂无历史记录</p>
            ) : (
              <div className="grid grid-cols-2 gap-1.5">
                {historyTasks.map((task) => {
                  const imgUrl = collectImageSources(task.url, task.result?.data, task.result?.urls, task.result?.url)[0] ?? ''
                  const prompt = (task.request?.prompt as string | undefined) ?? ''
                  const date = task.created_at ? new Date(task.created_at).toLocaleDateString('zh-CN') : ''
                  if (!imgUrl) return null
                  return (
                    <a
                      key={task.task_id ?? task.id}
                      href="#"
                      className="group relative block overflow-hidden rounded-lg border border-border/50"
                      onClick={(e) => { e.preventDefault(); openImageUrl(imgUrl) }}
                    >
                      <img src={imgUrl} alt={prompt} className="aspect-square w-full object-cover" loading="lazy" />
                      <div className="absolute inset-0 flex flex-col justify-end bg-gradient-to-t from-black/70 via-black/20 to-transparent p-1.5 opacity-0 transition-opacity group-hover:opacity-100">
                        {prompt ? <p className="line-clamp-3 text-[10px] leading-tight text-white">{prompt}</p> : null}
                        <p className="mt-0.5 text-[9px] text-white/60">{date}</p>
                      </div>
                    </a>
                  )
                })}
              </div>
            )}
          </div>
        </Card>
      </div>
    </>
  )
}
