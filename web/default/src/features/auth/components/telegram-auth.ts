export interface TelegramAuthData {
  id: number
  first_name?: string
  last_name?: string
  username?: string
  photo_url?: string
  auth_date: number
  hash: string
  lang?: string
}

export function telegramAuthParams(
  data: TelegramAuthData
): Record<string, string> {
  const fields = [
    'id',
    'first_name',
    'last_name',
    'username',
    'photo_url',
    'auth_date',
    'hash',
    'lang',
  ] as const
  return Object.fromEntries(
    fields.flatMap((field) => {
      const value = data[field]
      return value === undefined || value === null || value === ''
        ? []
        : [[field, String(value)]]
    })
  )
}
