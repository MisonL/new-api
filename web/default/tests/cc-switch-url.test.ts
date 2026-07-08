import { describe, expect, test } from 'bun:test'
import {
  buildCCSwitchURL,
  ensureCCSwitchApiKey,
} from '../src/features/keys/lib/cc-switch'

describe('cc switch import url', () => {
  test('builds Codex provider import with OpenAI-compatible v1 endpoint', () => {
    const url = new URL(
      buildCCSwitchURL({
        app: 'codex',
        name: 'dev-new-api',
        models: {
          model: 'gpt-5.5',
        },
        apiKey: 'sk-local',
        serverAddress: 'http://127.0.0.1:3001/',
      })
    )

    expect(url.protocol).toBe('ccswitch:')
    expect(url.hostname).toBe('v1')
    expect(url.pathname).toBe('/import')
    expect(url.searchParams.get('resource')).toBe('provider')
    expect(url.searchParams.get('app')).toBe('codex')
    expect(url.searchParams.get('name')).toBe('dev-new-api')
    expect(url.searchParams.get('endpoint')).toBe('http://127.0.0.1:3001/v1')
    expect(url.searchParams.get('homepage')).toBe('http://127.0.0.1:3001')
    expect(url.searchParams.get('apiKey')).toBe('sk-local')
    expect(url.searchParams.get('model')).toBe('gpt-5.5')
    expect(url.searchParams.get('enabled')).toBe('true')
  })

  test('normalizes server address that already ends with /v1', () => {
    const url = new URL(
      buildCCSwitchURL({
        app: 'codex',
        name: 'dev-new-api',
        models: {
          model: 'gpt-5.5',
        },
        apiKey: 'sk-local',
        serverAddress: 'http://127.0.0.1:3001/v1',
      })
    )

    expect(url.searchParams.get('endpoint')).toBe('http://127.0.0.1:3001/v1')
    expect(url.searchParams.get('homepage')).toBe('http://127.0.0.1:3001')
  })

  test('keeps non-Codex providers on the server root endpoint', () => {
    const url = new URL(
      buildCCSwitchURL({
        app: 'claude',
        name: 'dev-new-api',
        models: {
          model: 'claude-sonnet-4',
          haikuModel: '',
        },
        apiKey: 'sk-local',
        serverAddress: 'https://api.example.test/',
      })
    )

    expect(url.searchParams.get('endpoint')).toBe('https://api.example.test')
    expect(url.searchParams.get('homepage')).toBe('https://api.example.test')
    expect(url.searchParams.get('model')).toBe('claude-sonnet-4')
    expect(url.searchParams.has('haikuModel')).toBe(false)
  })

  test('preserves /v1 suffix for non-Codex providers', () => {
    const url = new URL(
      buildCCSwitchURL({
        app: 'claude',
        name: 'dev-new-api',
        models: {
          model: 'claude-sonnet-4',
        },
        apiKey: 'sk-local',
        serverAddress: 'https://api.example.test/v1/',
      })
    )

    expect(url.searchParams.get('endpoint')).toBe(
      'https://api.example.test/v1'
    )
    expect(url.searchParams.get('homepage')).toBe(
      'https://api.example.test/v1'
    )
  })

  test('preserves empty key instead of creating a bare sk prefix', () => {
    expect(ensureCCSwitchApiKey('   ')).toBe('')
  })

  test('rejects blank api keys when building import url', () => {
    expect(() =>
      buildCCSwitchURL({
        app: 'claude',
        name: 'dev-new-api',
        models: {
          model: 'claude-sonnet-4',
        },
        apiKey: '   ',
        serverAddress: 'https://api.example.test/',
      })
    ).toThrow(/API key is required/)
  })

  test('rejects blank server address when building import url', () => {
    expect(() =>
      buildCCSwitchURL({
        app: 'claude',
        name: 'dev-new-api',
        models: {
          model: 'claude-sonnet-4',
        },
        apiKey: 'sk-local',
        serverAddress: '   ',
      })
    ).toThrow(/Server address is required/)
  })

  test('normalizes key prefix for CC Switch import', () => {
    const url = new URL(
      buildCCSwitchURL({
        app: 'claude',
        name: 'dev-new-api',
        models: {
          model: 'claude-sonnet-4',
        },
        apiKey: 'abc',
        serverAddress: 'https://api.example.test/',
      })
    )

    expect(ensureCCSwitchApiKey('abc')).toBe('sk-abc')
    expect(ensureCCSwitchApiKey(' sk-abc ')).toBe('sk-abc')
    expect(url.searchParams.get('apiKey')).toBe('sk-abc')
  })
})
