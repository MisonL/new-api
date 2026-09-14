export interface VChartThemeManager {
  setCurrentTheme(theme: string): void
}

export declare const ThemeManager: VChartThemeManager

export declare const dataScheme: Array<{
  maxDomainLength?: number
  scheme: string[]
}>
