import * as z from 'zod'

export const quotaSchema = z.object({
  QuotaForNewUser: z.coerce.number().min(0),
  PreConsumedQuota: z.coerce.number().min(0),
  QuotaForInviter: z.coerce.number().min(0),
  QuotaForInvitee: z.coerce.number().min(0),
  TopUpLink: z.string().url().optional().or(z.literal('')),
  general_setting: z.object({
    docs_link: z.string().url().optional().or(z.literal('')),
  }),
  quota_setting: z.object({
    enable_free_model_pre_consume: z.boolean(),
  }),
})

export type QuotaFormValues = z.infer<typeof quotaSchema>
