import type { PaymentMethod, TopupInfo } from '../types'

interface PaymentMethodDisplay {
  type: string
  name?: string
}

export interface SubscriptionPaymentOptions {
  enableStripe: boolean
  enableCreem: boolean
  enableOnlineTopUp: boolean
  epayMethods: PaymentMethod[]
}

export function getEpayMethods(
  payMethods: PaymentMethod[] = []
): PaymentMethod[] {
  return payMethods.filter(
    (method) =>
      Boolean(method?.type) &&
      method.type !== 'stripe' &&
      method.type !== 'creem'
  )
}

export function getSubscriptionPaymentOptions(
  topupInfo: Pick<
    TopupInfo,
    | 'enable_online_topup'
    | 'enable_stripe_topup'
    | 'enable_creem_topup'
    | 'pay_methods'
  > | null
): SubscriptionPaymentOptions {
  return {
    enableStripe: Boolean(topupInfo?.enable_stripe_topup),
    enableCreem: Boolean(topupInfo?.enable_creem_topup),
    enableOnlineTopUp: Boolean(topupInfo?.enable_online_topup),
    epayMethods: getEpayMethods(topupInfo?.pay_methods),
  }
}

export function getBillingPreferenceLabel(
  preference: string,
  t: (key: string) => string
): string {
  switch (preference) {
    case 'subscription_first':
      return t('Subscription First')
    case 'wallet_first':
      return t('Wallet First')
    case 'subscription_only':
      return t('Subscription Only')
    case 'wallet_only':
      return t('Wallet Only')
    default:
      return preference
  }
}

export function getSelectedPaymentMethodLabel(
  selectedMethod: string,
  methods: PaymentMethodDisplay[],
  t: (key: string) => string
): string {
  return (
    methods.find((method) => method.type === selectedMethod)?.name ||
    selectedMethod ||
    t('Select payment method')
  )
}
