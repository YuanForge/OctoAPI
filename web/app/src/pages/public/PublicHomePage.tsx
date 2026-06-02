import type { ComponentType, ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  ActivityIcon,
  ArrowRightIcon,
  BarChart3Icon,
  BlocksIcon,
  BookOpenIcon,
  CheckCircle2Icon,
  Code2Icon,
  DatabaseZapIcon,
  GaugeIcon,
  Globe2Icon,
  ImageIcon,
  KeyRoundIcon,
  Layers3Icon,
  LockKeyholeIcon,
  MessageSquareTextIcon,
  MusicIcon,
  NetworkIcon,
  PlayCircleIcon,
  RadioTowerIcon,
  ShieldCheckIcon,
  SparklesIcon,
  TerminalSquareIcon,
  VideoIcon,
  WalletCardsIcon,
  ZapIcon,
} from 'lucide-react'

import { AppLogo } from '@/components/shared/AppLogo'
import { LanguageSwitcher } from '@/components/shared/LanguageSwitcher'
import { ThemeToggle } from '@/components/shared/ThemeToggle'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { useSiteSettings } from '@/hooks/use-site-settings'
import { getRoleToken } from '@/lib/auth/storage'
import { cn } from '@/lib/utils'

type Feature = {
  titleKey: string
  descriptionKey: string
  Icon: ComponentType<{ className?: string }>
  tone: string
  span?: string
  preview: ReactNode
}

const modelPills = [
  'GPT',
  'Claude',
  'Gemini',
  'DeepSeek',
  'Qwen',
  'Suno',
  'Kling',
  'SD',
]

const stats = [
  { valueKey: 'home.statsUpstreamsValue', labelKey: 'home.statsUpstreams' },
  { valueKey: 'home.statsCapabilitiesValue', labelKey: 'home.statsCapabilities' },
  { valueKey: 'home.statsProtocolValue', labelKey: 'home.statsProtocol' },
  { valueKey: 'home.statsBillingValue', labelKey: 'home.statsBilling' },
]

const features: Feature[] = [
  {
    titleKey: 'home.featureGatewayTitle',
    descriptionKey: 'home.featureGatewayDesc',
    Icon: NetworkIcon,
    tone: 'text-sky-600 bg-sky-500/10 ring-sky-500/20',
    span: 'lg:col-span-2',
    preview: (
      <div className="grid grid-cols-4 gap-2">
        {modelPills.map((name) => (
          <span
            key={name}
            className="rounded-md border border-border/70 bg-background/70 px-2 py-2 text-center text-xs font-medium text-muted-foreground"
          >
            {name}
          </span>
        ))}
      </div>
    ),
  },
  {
    titleKey: 'home.featureKeysTitle',
    descriptionKey: 'home.featureKeysDesc',
    Icon: KeyRoundIcon,
    tone: 'text-emerald-600 bg-emerald-500/10 ring-emerald-500/20',
    preview: (
      <div className="space-y-2 text-xs">
        {['sk-live-****-4f2a', 'sk-team-****-91be', 'sk-app-****-0c77'].map((key, index) => (
          <div key={key} className="flex items-center justify-between rounded-md bg-background/70 px-3 py-2">
            <span className="font-mono text-muted-foreground">{key}</span>
            <span className={cn('size-2 rounded-full', index === 1 ? 'bg-amber-400' : 'bg-emerald-500')} />
          </div>
        ))}
      </div>
    ),
  },
  {
    titleKey: 'home.featureObserveTitle',
    descriptionKey: 'home.featureObserveDesc',
    Icon: ActivityIcon,
    tone: 'text-violet-600 bg-violet-500/10 ring-violet-500/20',
    preview: (
      <div className="flex h-20 items-end gap-2">
        {[34, 56, 42, 74, 50, 86, 62, 92].map((height, index) => (
          <span
            key={`${height}-${index}`}
            className="flex-1 rounded-t bg-primary/70"
            style={{ height: `${height}%`, opacity: 0.42 + index * 0.06 }}
          />
        ))}
      </div>
    ),
  },
  {
    titleKey: 'home.featureProtocolTitle',
    descriptionKey: 'home.featureProtocolDesc',
    Icon: Code2Icon,
    tone: 'text-amber-600 bg-amber-500/10 ring-amber-500/20',
    span: 'lg:col-span-2',
    preview: (
      <div className="rounded-lg bg-zinc-950 p-3 font-mono text-[11px] leading-5 text-zinc-100">
        <div><span className="text-emerald-300">POST</span> /v1/chat/completions</div>
        <div><span className="text-sky-300">POST</span> /v1/images/generations</div>
        <div><span className="text-violet-300">GET</span> /v1/tasks/&#123;task_id&#125;</div>
      </div>
    ),
  },
]

const workflow = [
  {
    titleKey: 'home.workflowKeyTitle',
    descriptionKey: 'home.workflowKeyDesc',
    Icon: KeyRoundIcon,
  },
  {
    titleKey: 'home.workflowModelTitle',
    descriptionKey: 'home.workflowModelDesc',
    Icon: BlocksIcon,
  },
  {
    titleKey: 'home.workflowMonitorTitle',
    descriptionKey: 'home.workflowMonitorDesc',
    Icon: BarChart3Icon,
  },
]

function PublicHeader({ siteName, logoUrl, signedIn }: { siteName: string; logoUrl: string; signedIn: boolean }) {
  const { t } = useTranslation()

  return (
    <header className="sticky top-0 z-40 border-b border-border/60 bg-background/88 backdrop-blur-xl">
      <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 sm:px-6">
        <Link to="/" aria-label={siteName}>
          <AppLogo siteName={siteName} logoUrl={logoUrl} label="AI Gateway" />
        </Link>
        <nav className="hidden items-center gap-7 text-sm font-medium text-muted-foreground md:flex">
          <a href="#features" className="transition hover:text-foreground">{t('common.features')}</a>
          <a href="#models" className="transition hover:text-foreground">{t('common.models')}</a>
          <a href="#workflow" className="transition hover:text-foreground">{t('common.workflow')}</a>
          <Link to="/docs" className="transition hover:text-foreground">{t('common.docs')}</Link>
        </nav>
        <div className="flex items-center gap-2">
          <LanguageSwitcher />
          <ThemeToggle />
          {signedIn ? (
            <Button asChild>
              <Link to="/dashboard">
                {t('common.dashboard')}
                <ArrowRightIcon data-icon="inline-end" />
              </Link>
            </Button>
          ) : (
            <>
              <Button asChild variant="ghost" className="hidden sm:inline-flex">
                <Link to="/login">{t('common.login')}</Link>
              </Button>
              <Button asChild>
                <Link to="/register">
                  {t('common.startUsing')}
                  <ArrowRightIcon data-icon="inline-end" />
                </Link>
              </Button>
            </>
          )}
        </div>
      </div>
    </header>
  )
}

function ApiTerminal() {
  return (
    <div className="relative mx-auto w-full max-w-xl overflow-hidden rounded-2xl border border-white/12 bg-zinc-950/95 text-zinc-100 shadow-2xl shadow-slate-950/20">
      <div className="flex items-center justify-between border-b border-white/10 px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="size-2.5 rounded-full bg-red-400" />
          <span className="size-2.5 rounded-full bg-amber-300" />
          <span className="size-2.5 rounded-full bg-emerald-400" />
        </div>
        <span className="font-mono text-[10px] uppercase tracking-[0.18em] text-zinc-500">FanAPI Gateway</span>
      </div>
      <div className="grid border-b border-white/10 text-xs sm:grid-cols-3">
        {[
          ['Chat', '/v1/chat/completions'],
          ['Image', '/v1/images/generations'],
          ['Task', '/v1/tasks/{id}'],
        ].map(([label, endpoint], index) => (
          <div
            key={label}
            className={cn(
              'border-white/10 px-4 py-3 sm:border-r',
              index === 2 && 'sm:border-r-0',
              index === 0 && 'bg-white/[0.04]'
            )}
          >
            <div className="mb-1 font-medium text-zinc-200">{label}</div>
            <div className="truncate font-mono text-[11px] text-zinc-500">{endpoint}</div>
          </div>
        ))}
      </div>
      <div className="space-y-5 p-5 font-mono text-xs leading-6 sm:p-6">
        <div>
          <div className="mb-2 text-[10px] font-semibold uppercase tracking-[0.18em] text-zinc-500">Request</div>
          <pre className="whitespace-pre-wrap text-zinc-300">
<span className="text-emerald-300">curl</span> <span className="text-sky-300">-X POST</span> "{'{'}origin{'}'}/v1/chat/completions" \
  <span className="text-sky-300">-H</span> "Authorization: Bearer YOUR_API_KEY" \
  <span className="text-sky-300">-d</span> '{'{'}"model":"gpt-4o-mini","messages":[...]{'}'}'
          </pre>
        </div>
        <div className="rounded-xl border border-white/10 bg-white/[0.03] p-4">
          <div className="mb-2 text-[10px] font-semibold uppercase tracking-[0.18em] text-zinc-500">Response</div>
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-zinc-400">status</span>
              <Badge className="bg-emerald-500/15 text-emerald-300">200 OK</Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-zinc-400">latency</span>
              <span className="text-zinc-200">126 ms</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-zinc-400">credits</span>
              <span className="text-zinc-200">0.0048</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

function CapabilityStrip() {
  const { t } = useTranslation()
  const items = [
    { label: t('home.capabilityText'), Icon: MessageSquareTextIcon },
    { label: t('home.capabilityImage'), Icon: ImageIcon },
    { label: t('home.capabilityVideo'), Icon: VideoIcon },
    { label: t('home.capabilityMusic'), Icon: MusicIcon },
    { label: t('home.capabilityAsync'), Icon: RadioTowerIcon },
  ]

  return (
    <div className="flex flex-wrap gap-3">
      {items.map(({ label, Icon }) => (
        <span
          key={label}
          className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/70 px-4 py-2 text-sm font-medium shadow-sm backdrop-blur"
        >
          <Icon className="size-4 text-primary" />
          {label}
        </span>
      ))}
    </div>
  )
}

export function PublicHomePage() {
  const { t } = useTranslation()
  const { settings } = useSiteSettings()
  const signedIn = Boolean(getRoleToken('user'))
  const { siteName, logoUrl } = settings

  return (
    <div className="min-h-screen overflow-x-hidden bg-background text-foreground">
      <PublicHeader siteName={siteName} logoUrl={logoUrl} signedIn={signedIn} />

      <main>
        <section className="relative isolate overflow-hidden">
          <img
            src="/landing/gateway-hero.png"
            alt=""
            aria-hidden
            className="absolute inset-0 -z-20 h-full w-full object-cover opacity-55 dark:opacity-35"
          />
          <div className="absolute inset-0 -z-10 bg-[linear-gradient(90deg,var(--background)_0%,color-mix(in_oklab,var(--background)_88%,transparent)_46%,color-mix(in_oklab,var(--background)_54%,transparent)_100%)]" />
          <div className="absolute inset-0 -z-10 bg-[linear-gradient(180deg,color-mix(in_oklab,var(--background)_90%,transparent)_0%,transparent_42%,var(--background)_100%)]" />

          <div className="mx-auto flex max-w-7xl px-4 py-12 sm:px-6 sm:py-16 lg:min-h-[calc(100svh-9rem)] lg:items-center lg:py-20">
            <div className="max-w-3xl">
              <Badge className="mb-5 border-primary/20 bg-primary/10 px-3 py-1 text-primary" variant="outline">
                <SparklesIcon data-icon="inline-start" />
                {t('home.badge')}
              </Badge>
              <h1 className="text-4xl font-semibold leading-tight tracking-tight text-foreground sm:text-5xl lg:text-6xl">
                {siteName}
                <span className="block text-primary">{t('home.headline')}</span>
              </h1>
              <p className="mt-6 max-w-xl text-base leading-8 text-muted-foreground sm:text-lg">
                {t('home.subhead')}
              </p>
              <div className="mt-8 flex flex-wrap items-center gap-3">
                <Button asChild size="lg" className="h-11 px-5">
                  <Link to={signedIn ? '/dashboard' : '/register'}>
                    {signedIn ? t('home.primaryCtaSignedIn') : t('home.primaryCtaGuest')}
                    <ArrowRightIcon data-icon="inline-end" />
                  </Link>
                </Button>
                <Button asChild size="lg" variant="outline" className="h-11 px-5">
                  <Link to="/models">
                    {t('home.viewModels')}
                    <BlocksIcon data-icon="inline-end" />
                  </Link>
                </Button>
                <Button asChild size="lg" variant="ghost" className="h-11 px-5">
                  <Link to="/docs">
                    {t('home.readDocs')}
                    <BookOpenIcon data-icon="inline-end" />
                  </Link>
                </Button>
              </div>
              <div className="mt-10 hidden sm:block">
                <CapabilityStrip />
              </div>
            </div>
          </div>
        </section>

        <section className="border-y border-border/60 bg-card/40">
          <div className="mx-auto grid max-w-7xl grid-cols-2 gap-px bg-border/60 px-0 sm:grid-cols-4">
            {stats.map((item) => (
              <div key={item.labelKey} className="bg-background px-6 py-8 text-center">
                <div className="text-2xl font-semibold tracking-tight sm:text-3xl">{t(item.valueKey)}</div>
                <div className="mt-1 text-sm text-muted-foreground">{t(item.labelKey)}</div>
              </div>
            ))}
          </div>
        </section>

        <section className="px-4 py-16 sm:px-6 lg:py-20">
          <div className="mx-auto grid max-w-7xl gap-10 lg:grid-cols-[0.9fr_1.1fr] lg:items-center">
            <div className="max-w-xl">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-primary">{t('home.apiPreviewKicker')}</p>
              <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl">{t('home.apiPreviewTitle')}</h2>
              <p className="mt-4 text-base leading-7 text-muted-foreground">
                {t('home.apiPreviewDesc')}
              </p>
            </div>
            <ApiTerminal />
          </div>
        </section>

        <section id="features" className="px-4 py-20 sm:px-6 lg:py-24">
          <div className="mx-auto max-w-7xl">
            <div className="mb-10 max-w-2xl">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-primary">{t('home.coreKicker')}</p>
              <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl">{t('home.coreTitle')}</h2>
              <p className="mt-4 text-base leading-7 text-muted-foreground">
                {t('home.coreDesc')}
              </p>
            </div>
            <div className="grid gap-px overflow-hidden rounded-xl border border-border/70 bg-border/70 lg:grid-cols-3">
              {features.map(({ titleKey, descriptionKey, Icon, tone, span, preview }) => (
                <article key={titleKey} className={cn('bg-background p-6 transition hover:bg-muted/30', span)}>
                  <div className="mb-5 flex items-center gap-3">
                    <span className={cn('flex size-10 items-center justify-center rounded-lg ring-1', tone)}>
                      <Icon className="size-5" />
                    </span>
                    <h3 className="text-base font-semibold">{t(titleKey)}</h3>
                  </div>
                  <p className="min-h-14 text-sm leading-6 text-muted-foreground">{t(descriptionKey)}</p>
                  <div className="mt-6">{preview}</div>
                </article>
              ))}
            </div>
          </div>
        </section>

        <section id="models" className="border-y border-border/60 bg-muted/25 px-4 py-20 sm:px-6 lg:py-24">
          <div className="mx-auto grid max-w-7xl gap-10 lg:grid-cols-[0.9fr_1.1fr] lg:items-center">
            <div>
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-primary">{t('home.modelHubKicker')}</p>
              <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl">{t('home.modelHubTitle')}</h2>
              <p className="mt-4 text-base leading-7 text-muted-foreground">
                {t('home.modelHubDesc')}
              </p>
              <div className="mt-8 flex flex-wrap gap-3">
                <Button asChild>
                  <Link to="/models">
                    {t('home.browseModels')}
                    <ArrowRightIcon data-icon="inline-end" />
                  </Link>
                </Button>
                <Button asChild variant="outline">
                  <Link to="/docs">
                    {t('home.apiDocs')}
                    <TerminalSquareIcon data-icon="inline-end" />
                  </Link>
                </Button>
              </div>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              {[
                { titleKey: 'home.llmTitle', descKey: 'home.llmDesc', Icon: MessageSquareTextIcon },
                { titleKey: 'home.imageTitle', descKey: 'home.imageDesc', Icon: ImageIcon },
                { titleKey: 'home.videoTitle', descKey: 'home.videoDesc', Icon: VideoIcon },
                { titleKey: 'home.musicTitle', descKey: 'home.musicDesc', Icon: MusicIcon },
              ].map(({ titleKey, descKey, Icon }) => (
                <Card key={titleKey} className="rounded-lg border border-border/70 shadow-sm">
                  <CardContent className="p-5">
                    <Icon className="mb-4 size-5 text-primary" />
                    <h3 className="font-semibold">{t(titleKey)}</h3>
                    <p className="mt-2 text-sm leading-6 text-muted-foreground">{t(descKey)}</p>
                  </CardContent>
                </Card>
              ))}
            </div>
          </div>
        </section>

        <section id="workflow" className="px-4 py-20 sm:px-6 lg:py-24">
          <div className="mx-auto max-w-7xl">
            <div className="mb-12 text-center">
              <p className="mb-3 text-xs font-semibold uppercase tracking-[0.18em] text-primary">{t('home.workflowKicker')}</p>
              <h2 className="text-3xl font-semibold tracking-tight sm:text-4xl">{t('home.workflowTitle')}</h2>
            </div>
            <div className="grid gap-5 md:grid-cols-3">
              {workflow.map(({ titleKey, descriptionKey, Icon }, index) => (
                <div key={titleKey} className="relative rounded-xl border border-border/70 bg-card p-6">
                  <span className="absolute right-5 top-5 text-5xl font-semibold leading-none text-muted/80">
                    {index + 1}
                  </span>
                  <div className="mb-6 flex size-12 items-center justify-center rounded-lg bg-primary/10 text-primary">
                    <Icon className="size-6" />
                  </div>
                  <h3 className="text-lg font-semibold">{t(titleKey)}</h3>
                  <p className="mt-3 text-sm leading-6 text-muted-foreground">{t(descriptionKey)}</p>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className="bg-foreground px-4 py-16 text-background sm:px-6 lg:py-20">
          <div className="mx-auto grid max-w-7xl gap-8 lg:grid-cols-[1fr_auto] lg:items-center">
            <div>
              <div className="mb-4 flex flex-wrap gap-2">
                {[ShieldCheckIcon, GaugeIcon, WalletCardsIcon, DatabaseZapIcon, LockKeyholeIcon, Globe2Icon, Layers3Icon, PlayCircleIcon, ZapIcon, CheckCircle2Icon].map((Icon, index) => (
                  <span key={index} className="flex size-9 items-center justify-center rounded-lg bg-background/10 text-background/80">
                    <Icon className="size-4" />
                  </span>
                ))}
              </div>
              <h2 className="text-2xl font-semibold tracking-tight sm:text-3xl">{t('home.finalTitle')}</h2>
              <p className="mt-3 max-w-2xl text-sm leading-7 text-background/70">
                {t('home.finalDesc')}
              </p>
            </div>
            <div className="flex flex-wrap gap-3 lg:justify-end">
              <Button asChild variant="secondary" className="bg-background text-foreground hover:bg-background/90">
                <Link to={signedIn ? '/dashboard' : '/register'}>
                  {signedIn ? t('home.primaryCtaSignedIn') : t('home.registerAccount')}
                  <ArrowRightIcon data-icon="inline-end" />
                </Link>
              </Button>
              <Button asChild variant="outline" className="border-background/30 bg-transparent text-background hover:bg-background/10 hover:text-background">
                <Link to="/login">{t('common.login')}</Link>
              </Button>
            </div>
          </div>
        </section>
      </main>

      <footer className="border-t border-border/60 px-4 py-8 sm:px-6">
        <div className="mx-auto flex max-w-7xl flex-col gap-4 text-sm text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
          <span>{siteName} AI Gateway</span>
          <div className="flex gap-5">
            <Link to="/models" className="hover:text-foreground">{t('home.footerProduct')}</Link>
            <Link to="/docs" className="hover:text-foreground">{t('common.docs')}</Link>
            <Link to="/login" className="hover:text-foreground">{t('common.login')}</Link>
          </div>
        </div>
      </footer>
    </div>
  )
}
