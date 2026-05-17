import { useEffect, useRef, useState } from 'react'

import { MessageContent } from '@/components/shared/MessageContent'
import { PageHeader } from '@/components/shared/PageHeader'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { buildUserInvokeHeaders, canInvokeWithSelectedKey } from '@/lib/api/request-auth'
import { userApi, type ApiKeyRecord, type UserChannel, type ChatConversation, type ConversationMessage } from '@/lib/api/user'

type Message = {
  role: 'user' | 'assistant'
  content: string
}

export function UserPlaygroundPage() {
  const [apiKeys, setApiKeys] = useState<ApiKeyRecord[]>([])
  const [channels, setChannels] = useState<UserChannel[]>([])
  const [selectedKeyId, setSelectedKeyId] = useState<number | undefined>()
  const [selectedChannelId, setSelectedChannelId] = useState<number | undefined>()
  const [error, setError] = useState('')
  const [systemPrompt, setSystemPrompt] = useState('')
  const [maxTokens, setMaxTokens] = useState<string>('')
  const [temperature, setTemperature] = useState(0.7)
  const [topP, setTopP] = useState(1)
  const [useTemp, setUseTemp] = useState(false)
  const [useTopP, setUseTopP] = useState(false)
  const [inputText, setInputText] = useState('')
  const [messages, setMessages] = useState<Message[]>([])
  const [streaming, setStreaming] = useState(false)
  const [streamingText, setStreamingText] = useState('')
  const [conversations, setConversations] = useState<ChatConversation[]>([])
  const [currentConvId, setCurrentConvId] = useState<number | undefined>()
  const scrollRef = useRef<HTMLDivElement | null>(null)

  async function loadConversations() {
    try {
      const res = await userApi.listConversations()
      setConversations(res.items ?? [])
    } catch { /* ignore */ }
  }

  useEffect(() => {
    async function load() {
      try {
        const [keysRes, channelsRes] = await Promise.all([
          userApi.listApiKeys(),
          userApi.listChannels(),
        ])
        const nextKeys = Array.isArray(keysRes) ? keysRes : keysRes.api_keys ?? keysRes.keys ?? []
        const nextChannels = (Array.isArray(channelsRes) ? channelsRes : channelsRes.channels ?? []).filter(
          (item) => item.type === 'llm'
        )
        setApiKeys(nextKeys)
        setChannels(nextChannels)
        if (nextKeys.length > 0) setSelectedKeyId(nextKeys[0].id)
        if (nextChannels.length > 0) setSelectedChannelId(nextChannels[0].id)
      } catch {
        setError('读取 API 密钥或模型列表失败')
      }
    }

    void load()
    void loadConversations()
  }, [])

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' })
  }, [messages, streamingText])

  function canInvoke() {
    return canInvokeWithSelectedKey(apiKeys, selectedKeyId)
  }

  function currentChannel() {
    return channels.find((item) => item.id === selectedChannelId) ?? channels[0]
  }

  async function saveCurrentConversation() {
    if (messages.length === 0) return
    const title = messages.find((m) => m.role === 'user')?.content.slice(0, 40) ?? '对话'
    const model = currentChannel()?.routing_model || currentChannel()?.name || ''
    try {
      const saved = await userApi.saveConversation({
        id: currentConvId,
        title,
        model,
        messages: messages as ConversationMessage[],
      })
      setCurrentConvId(saved.id)
      await loadConversations()
    } catch { /* ignore */ }
  }

  async function deleteConversation(id: number) {
    try {
      await userApi.deleteConversation(id)
      setConversations((prev) => prev.filter((c) => c.id !== id))
      if (currentConvId === id) {
        setMessages([])
        setCurrentConvId(undefined)
      }
    } catch { /* ignore */ }
  }

  async function sendMessage() {
    if (!inputText.trim() || streaming) return
    const authHeaders = buildUserInvokeHeaders(apiKeys, selectedKeyId)
    if (!authHeaders) {
      setError('请选择可用的 API 密钥')
      return
    }
    if (!selectedChannelId && channels.length === 0) {
      setError('当前没有可用的文本模型渠道')
      return
    }

    const userMessage: Message = { role: 'user', content: inputText.trim() }
    const nextMessages = [...messages, userMessage]
    setError('')
    setMessages(nextMessages)
    setInputText('')
    setStreaming(true)
    setStreamingText('')

    const body: Record<string, unknown> = {
      model:
        currentChannel()?.routing_model ||
        currentChannel()?.name ||
        'gpt-3.5-turbo',
      messages: [
        ...(systemPrompt.trim()
          ? [{ role: 'system', content: systemPrompt.trim() }]
          : []),
        ...nextMessages,
      ],
      stream: true,
    }
    const parsedMaxTokens = Number(maxTokens)
    if (Number.isFinite(parsedMaxTokens) && parsedMaxTokens > 0) {
      body.max_tokens = parsedMaxTokens
    }
    if (useTemp) body.temperature = temperature
    if (useTopP) body.top_p = topP

    try {
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...authHeaders,
        },
        body: JSON.stringify(body),
      })

      if (!response.ok || !response.body) {
        throw new Error((await response.text()) || `请求失败 (${response.status})`)
      }

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let accum = ''

      while (true) {
        const { value, done } = await reader.read()
        if (done) break
        const chunk = decoder.decode(value)
        const lines = chunk.split('\n')
        for (const line of lines) {
          if (!line.startsWith('data: ')) continue
          const data = line.slice(6).trim()
          if (data === '[DONE]') continue
          try {
            const parsed = JSON.parse(data)
            const delta = parsed.choices?.[0]?.delta?.content || ''
            accum += delta
            setStreamingText(accum)
          } catch {
            // skip malformed chunks
          }
        }
      }

      setMessages((current) => [...current, { role: 'assistant', content: accum }])
      setStreamingText('')
    } catch (error) {
      setMessages((current) => [
        ...current,
        { role: 'assistant', content: `请求失败：${error instanceof Error ? error.message : '未知错误'}` },
      ])
      setError(error instanceof Error ? error.message : '请求失败')
    } finally {
      setStreaming(false)
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Playground"
        title="文本对话"
        description="已经接上真实 `/v1/chat/completions`，可直接用已有 API Key 做对话验证。"
        actions={<Button onClick={() => { void saveCurrentConversation().then(() => { setMessages([]); setCurrentConvId(undefined) }) }}>新对话</Button>}
      />
      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}
      <div className="grid gap-4 xl:grid-cols-[320px_1fr] 2xl:grid-cols-[320px_1fr_240px]">
        <Card>
          <CardContent className="flex flex-col gap-4 p-6">
            <div className="flex flex-col gap-2">
              <Label>API 密钥</Label>
              <NativeSelect
                value={selectedKeyId}
                onChange={(event) => setSelectedKeyId(Number(event.target.value))}
              >
                {apiKeys.map((key) => (
                  <option key={key.id} value={key.id}>
                    {key.name || key.masked_key || key.key}
                  </option>
                ))}
              </NativeSelect>
              <p className="text-xs text-muted-foreground">
                已登录用户可直接使用已创建的密钥进行调用。
              </p>
            </div>
            <div className="flex flex-col gap-2">
              <Label>模型</Label>
              <NativeSelect
                value={selectedChannelId}
                onChange={(event) => setSelectedChannelId(Number(event.target.value))}
              >
                {channels.map((channel) => (
                  <option key={channel.id} value={channel.id}>
                    {channel.name} {channel.routing_model ? `· ${channel.routing_model}` : ''}
                  </option>
                ))}
              </NativeSelect>
              {channels.length === 0 ? (
                <p className="text-xs text-muted-foreground">当前没有可用的文本模型渠道。</p>
              ) : null}
            </div>
            <div className="flex flex-col gap-2">
              <Label>系统提示词</Label>
              <Textarea
                value={systemPrompt}
                onChange={(event) => setSystemPrompt(event.target.value)}
                placeholder="例如：你是一个专业的 AI 助手"
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label>Max Tokens <span className="text-muted-foreground font-normal">(选填)</span></Label>
              <Input
                type="number"
                inputMode="numeric"
                min={1}
                max={128000}
                value={maxTokens}
                onChange={(event) => setMaxTokens(event.target.value)}
                placeholder="不填则不限制"
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label className="flex items-center justify-between">
                <span>Temperature <span className="text-muted-foreground font-normal">(选填)</span></span>
                <input
                  type="checkbox"
                  checked={useTemp}
                  onChange={(event) => setUseTemp(event.target.checked)}
                />
              </Label>
              {useTemp ? (
                <div className="flex items-center gap-3">
                  <input
                    type="range"
                    min={0}
                    max={2}
                    step={0.01}
                    value={temperature}
                    onChange={(event) => setTemperature(Number(event.target.value))}
                    className="flex-1 accent-primary"
                  />
                  <span className="w-12 text-right font-mono text-xs tabular-nums">{temperature.toFixed(2)}</span>
                </div>
              ) : null}
            </div>
            <div className="flex flex-col gap-2">
              <Label className="flex items-center justify-between">
                <span>Top P <span className="text-muted-foreground font-normal">(选填)</span></span>
                <input
                  type="checkbox"
                  checked={useTopP}
                  onChange={(event) => setUseTopP(event.target.checked)}
                />
              </Label>
              {useTopP ? (
                <div className="flex items-center gap-3">
                  <input
                    type="range"
                    min={0}
                    max={1}
                    step={0.01}
                    value={topP}
                    onChange={(event) => setTopP(Number(event.target.value))}
                    className="flex-1 accent-primary"
                  />
                  <span className="w-12 text-right font-mono text-xs tabular-nums">{topP.toFixed(2)}</span>
                </div>
              ) : null}
            </div>
          </CardContent>
        </Card>
        <Card className="flex min-h-[70vh] flex-col overflow-hidden">
          <CardContent className="flex min-h-0 flex-1 flex-col p-0">
            <div ref={scrollRef} className="flex-1 flex flex-col gap-4 overflow-auto p-6">
              {messages.length === 0 && !streaming ? (
                <div className="flex min-h-[300px] items-center justify-center text-sm text-muted-foreground">
                  开始一段对话吧。
                </div>
              ) : null}
              {messages.map((message, index) => (
                <div
                  key={`${message.role}-${index}`}
                  className={`flex ${message.role === 'user' ? 'justify-end' : 'justify-start'}`}
                >
                  <div
                    className={`max-w-[80%] rounded-2xl px-4 py-3 text-sm leading-7 ${
                      message.role === 'user'
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-muted text-foreground'
                    }`}
                  >
                    <MessageContent content={message.content} role={message.role} />
                  </div>
                </div>
              ))}
              {streaming && streamingText ? (
                <div className="flex justify-start">
                  <div className="max-w-[80%] rounded-2xl bg-muted px-4 py-3 text-sm leading-7">
                    <MessageContent content={streamingText} role="assistant" />
                  </div>
                </div>
              ) : null}
            </div>
            <div className="border-t border-border/70 p-4">
              <div className="flex gap-3">
                <Textarea
                  className="min-h-24 flex-1"
                  value={inputText}
                  onChange={(event) => setInputText(event.target.value)}
                  placeholder="输入消息，Enter 发送"
                />
                <div className="flex flex-col gap-1 items-end">
                  <Button
                    onClick={sendMessage}
                    disabled={streaming || !inputText.trim() || !canInvoke() || channels.length === 0}
                  >
                    {streaming ? '生成中...' : '发送'}
                  </Button>
                  {apiKeys.length > 0 && !canInvoke() && (
                    <p className="text-xs text-destructive">所选密钥不可用，请重新选择</p>
                  )}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
        <Card className="hidden 2xl:flex flex-col overflow-hidden">
          <div className="flex items-center justify-between border-b px-4 py-3 shrink-0">
            <span className="text-sm font-semibold">历史对话</span>
            <button type="button" onClick={() => void loadConversations()} className="text-xs text-muted-foreground hover:text-foreground">刷新</button>
          </div>
          <div className="flex-1 overflow-y-auto divide-y divide-border/60">
            {conversations.length === 0 ? (
              <p className="px-4 py-10 text-center text-xs text-muted-foreground">暂无历史对话</p>
            ) : (
              conversations.map((conv) => (
                <div
                  key={conv.id}
                  className={`group flex cursor-pointer items-center gap-1.5 px-3 py-2 hover:bg-muted/50 ${currentConvId === conv.id ? 'bg-muted/70' : ''}`}
                  onClick={() => { setMessages(conv.messages as Message[]); setCurrentConvId(conv.id); setStreamingText('') }}
                >
                  <p className="flex-1 truncate text-xs leading-snug">{conv.title}</p>
                  <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); void deleteConversation(conv.id) }}
                    className="shrink-0 text-sm text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100"
                  >
                    ×
                  </button>
                </div>
              ))
            )}
          </div>
        </Card>
      </div>
    </>
  )
}
