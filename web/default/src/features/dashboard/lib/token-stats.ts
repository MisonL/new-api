import { z } from 'zod'

const tokenStatsResponse = z.object({
  success: z.literal(true),
  data: z.object({
    input_tokens: z.number().int().nonnegative(),
    cache_read_tokens: z.number().int().nonnegative(),
    cache_write_tokens: z.number().int().nonnegative(),
    output_tokens: z.number().int().nonnegative(),
    cache_rate: z.number().nonnegative().nullable(),
  }),
})

export function parseDashboardTokenStats(payload: unknown) {
  return tokenStatsResponse.parse(payload).data
}
