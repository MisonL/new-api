import { describe, expect, test } from 'bun:test'
import {
  getDynamicPricingSummary,
  isDynamicPricingModel,
} from '../src/features/pricing/lib/dynamic-price'
import type { PricingModel } from '../src/features/pricing/types'

// Guard against a regression that would make the pricing page show a price the
// server does not charge. The parser is regex based and the server expression
// grammar keeps growing (service_tier, header, ...), so every shape the server
// emits must at least keep input/output prices parseable.

function model(billingExpr: string): PricingModel {
  return {
    id: 1,
    model_name: 'probe',
    quota_type: 0,
    model_ratio: 0.3,
    completion_ratio: 4,
    enable_groups: ['default'],
    billing_mode: 'tiered_expr',
    billing_expr: billingExpr,
  }
}

const options = { tokenUnit: 'M' as const }

describe('dynamic pricing expression parsing', () => {
  test('nested service_tier branches still yield the input and output prices', () => {
    // Mirrors MiniMaxM3BillingExpr from setting/billing_setting/tiered_billing.go.
    const expr =
      'len <= 512000 ? (param("service_tier") == "priority" ? tier("priority_standard", p * 0.45 + c * 1.8 + cr * 0.09) : tier("standard", p * 0.3 + c * 1.2 + cr * 0.06)) : (param("service_tier") == "priority" ? tier("priority_long_context", p * 0.9 + c * 3.6 + cr * 0.18) : tier("long_context", p * 0.6 + c * 2.4 + cr * 0.12))'

    const summary = getDynamicPricingSummary(model(expr), options)
    expect(summary).not.toBeNull()
    if (!summary) throw new Error('unreachable')

    expect(summary.isSpecialExpression).toBe(false)
    expect(summary.tierCount).toBe(4)
    // First tier in the expression is the priority standard tier.
    expect(summary.tier?.inputPrice).toBeCloseTo(0.45, 10)
    expect(summary.tier?.outputPrice).toBeCloseTo(1.8, 10)
    expect(summary.primaryEntries.map((entry) => entry.value)).toEqual([
      0.45, 1.8,
    ])
  })

  test('a tier whose coefficients cannot be read yields no price entries', () => {
    const summary = getDynamicPricingSummary(
      model('tier("standard", p + c)'),
      options
    )
    expect(summary).not.toBeNull()
    if (!summary) throw new Error('unreachable')

    // The tier text does not carry a coefficient for any priced variable, so
    // the page must say the expression could not be parsed rather than print a
    // price of $0 for a model the server does charge for.
    expect(summary.tierCount).toBe(1)
    expect(summary.primaryEntries).toEqual([])
    expect(summary.isSpecialExpression).toBe(false)
  })

  test('a plain long-context expression keeps both tiers', () => {
    const expr =
      'len <= 200000 ? tier("standard", p * 2 + c * 12 + cr * 0.2) : tier("long_context", p * 4 + c * 18 + cr * 0.4)'
    const summary = getDynamicPricingSummary(model(expr), options)
    expect(summary?.tierCount).toBe(2)
    expect(summary?.tier?.inputPrice).toBeCloseTo(2, 10)
    expect(summary?.tier?.outputPrice).toBeCloseTo(12, 10)
  })
})

describe('isDynamicPricingModel', () => {
  test('requires both the mode flag and a non-empty expression', () => {
    expect(isDynamicPricingModel(model('tier("a", p * 1)'))).toBe(true)
    expect(
      isDynamicPricingModel({ ...model('tier("a", p * 1)'), billing_expr: '' })
    ).toBe(false)
    expect(
      isDynamicPricingModel({ ...model('tier("a", p * 1)'), billing_mode: '' })
    ).toBe(false)
  })
})
