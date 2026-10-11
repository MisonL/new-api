/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';

import enTranslation from './locales/en.json';
import frTranslation from './locales/fr.json';
import zhCNTranslation from './locales/zh-CN.json';
import zhTWTranslation from './locales/zh-TW.json';
import ruTranslation from './locales/ru.json';
import jaTranslation from './locales/ja.json';
import viTranslation from './locales/vi.json';
import { supportedLanguages } from './language';

i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    load: 'currentOnly',
    supportedLngs: supportedLanguages,
    resources: {
      en: enTranslation,
      'zh-CN': zhCNTranslation,
      'zh-TW': zhTWTranslation,
      fr: frTranslation,
      ru: ruTranslation,
      ja: jaTranslation,
      vi: viTranslation,
    },
    fallbackLng: 'zh-CN',
    nsSeparator: false,
    // An empty translation means "not translated yet", not "render nothing".
    // By default i18next only falls back to fallbackLng when the key is
    // absent, so a key present with value "" resolves to an empty string and
    // the UI renders a blank label, button or aria-label. i18next-cli extract
    // writes exactly such empty entries for every missing key, so without this
    // the blank values spread on each extraction.
    returnEmptyString: false,
    interpolation: {
      escapeValue: false,
    },
  });

window.__i18n = i18n;

export default i18n;
