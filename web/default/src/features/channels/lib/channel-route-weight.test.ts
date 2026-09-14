import assert from 'node:assert/strict'
import test from 'node:test'
import {
  adjustRouteWeights,
  formatRouteWeightPercent,
  parseRouteWeightPercent,
  ROUTE_WEIGHT_TOTAL_BPS,
} from './channel-route-weight'

test('route weight percentage conversion preserves basis points exactly', () => {
  assert.equal(parseRouteWeightPercent('0'), 0)
  assert.equal(parseRouteWeightPercent('12.34'), 1234)
  assert.equal(parseRouteWeightPercent('100.00'), ROUTE_WEIGHT_TOTAL_BPS)
  assert.equal(formatRouteWeightPercent(1), '0.01')
  assert.equal(formatRouteWeightPercent(1234), '12.34')
  assert.equal(formatRouteWeightPercent(10000), '100')
})

test('route weight percentage conversion rejects unsafe precision and range', () => {
  assert.equal(parseRouteWeightPercent(''), null)
  assert.equal(parseRouteWeightPercent('1.234'), null)
  assert.equal(parseRouteWeightPercent('100.01'), null)
  assert.equal(parseRouteWeightPercent('-1'), null)
  assert.equal(parseRouteWeightPercent('101'), null)
  assert.equal(formatRouteWeightPercent(-1), '')
  assert.equal(formatRouteWeightPercent(10001), '')
})

test('equal adjustment uses water-filling for bounded participants', () => {
  assert.deepEqual(
    adjustRouteWeights([5000, 100, 100, 4800], 0, 9000),
    [9000, 0, 0, 1000]
  )
})

test('equal adjustment respects locked rows and rejects an impossible target', () => {
  assert.deepEqual(
    adjustRouteWeights([4000, 3000, 3000], 0, 5000, 'equal_adjust', [
      false,
      true,
      false,
    ]),
    [5000, 3000, 2000]
  )
  assert.equal(
    adjustRouteWeights([5000, 5000], 0, 6000, 'equal_adjust', [false, true]),
    null
  )
  assert.equal(
    adjustRouteWeights([5000, 5000], 0, 6000, 'equal_adjust', [true, false]),
    null
  )
})

test('proportional adjustment preserves exact total and stable rounding', () => {
  const result = adjustRouteWeights(
    [5000, 3000, 2000],
    0,
    4000,
    'proportional_adjust'
  )
  assert.deepEqual(result, [4000, 3600, 2400])
  assert.equal(
    result?.reduce((sum, value) => sum + value, 0),
    ROUTE_WEIGHT_TOTAL_BPS
  )
})
