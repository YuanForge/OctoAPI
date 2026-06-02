import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'

import { enUS, zhCN } from '@/i18n/resources'

export const LANGUAGE_STORAGE_KEY = 'fanapi_language'

export const supportedLanguages = [
  { code: 'zh-CN', label: '简体中文', shortLabel: '中' },
  { code: 'en-US', label: 'English', shortLabel: 'EN' },
] as const

export type AppLanguage = (typeof supportedLanguages)[number]['code']

function getStoredLanguage(): AppLanguage {
  if (typeof window === 'undefined') return 'zh-CN'

  const stored = window.localStorage.getItem(LANGUAGE_STORAGE_KEY)
  if (supportedLanguages.some((language) => language.code === stored)) {
    return stored as AppLanguage
  }

  return 'zh-CN'
}

void i18n.use(initReactI18next).init({
  resources: {
    'zh-CN': {
      translation: zhCN,
    },
    'en-US': {
      translation: enUS,
    },
  },
  lng: getStoredLanguage(),
  fallbackLng: 'zh-CN',
  interpolation: {
    escapeValue: false,
  },
  returnNull: false,
})

i18n.on('languageChanged', (language) => {
  if (typeof window === 'undefined') return
  if (supportedLanguages.some((item) => item.code === language)) {
    window.localStorage.setItem(LANGUAGE_STORAGE_KEY, language)
  }
})

export { i18n }
