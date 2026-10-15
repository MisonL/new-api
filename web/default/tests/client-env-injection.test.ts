import { describe, expect, test } from 'bun:test'
import rsbuildConfig from '../rsbuild.config'

type ConfigFactory = (env: {
  envMode: string
  command: string
}) => Promise<Record<string, unknown>> | Record<string, unknown>

const ENV_KEYS = {
  VITE_REACT_APP_READONLY_MODE: 'true',
  VITE_REACT_APP_SERVER_URL: 'http://127.0.0.1:3300',
} as const

/**
 * Resolve the config factory the way rsbuild does. The factory reads
 * process.env, so the caller sets the variables before invoking this.
 */
async function resolveConfig() {
  const factory = rsbuildConfig as unknown as ConfigFactory
  return (await factory({ envMode: 'production', command: 'build' })) as {
    source?: { define?: Record<string, string> }
  }
}

function withEnv<T>(vars: Record<string, string>, run: () => Promise<T>): Promise<T> {
  const previous: Record<string, string | undefined> = {}
  for (const [key, value] of Object.entries(vars)) {
    previous[key] = process.env[key]
    process.env[key] = value
  }
  return run().finally(() => {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) {
        delete process.env[key]
      } else {
        process.env[key] = value
      }
    }
  })
}

describe('VITE_ variables reach the client bundle', () => {
  // The rsbuild CLI loads env with the default `PUBLIC_` prefix, so a VITE_*
  // variable is not substituted into import.meta.env by itself. Without the
  // source.define entries asserted here, the read-only guard's flag compiles to
  // `void 0` and the guard silently never activates.
  test('injects each VITE_ variable into import.meta.env', async () => {
    const config = await withEnv(ENV_KEYS, resolveConfig)
    const define = config.source?.define ?? {}

    for (const [key, value] of Object.entries(ENV_KEYS)) {
      expect(define[`import.meta.env.${key}`]).toBe(JSON.stringify(value))
    }
  })

  test('omits the read-only flag when the variable is unset', async () => {
    const config = await withEnv({ VITE_REACT_APP_READONLY_MODE: '' }, async () => {
      delete process.env.VITE_REACT_APP_READONLY_MODE
      return resolveConfig()
    })
    const define = config.source?.define ?? {}

    expect(define['import.meta.env.VITE_REACT_APP_READONLY_MODE']).toBeUndefined()
  })

  test('folds the injected env into the build cache digest', async () => {
    const config = await withEnv(ENV_KEYS, resolveConfig)
    const performance = config.performance as {
      buildCache?: { cacheDigest?: string[] }
    }
    const digest = performance.buildCache?.cacheDigest ?? []

    // Without this a rebuild after flipping the read-only flag reused the
    // previous bundle, which read as "the flag has no effect".
    expect(digest.join('|')).toContain('VITE_REACT_APP_READONLY_MODE')
    expect(digest.join('|')).toContain('true')
  })
})
