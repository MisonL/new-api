export const HEADER_PROFILE_GROUPS = [
  {
    key: 'browser',
    name: 'Browser',
  },
  {
    key: 'ai_coding_cli',
    name: 'AI Coding CLI',
  },
  {
    key: 'api_sdk',
    name: 'API SDK / Debug',
  },
];

const browserProfiles = {
  'chrome-macos': {
    key: 'chrome-macos',
    name: 'Chrome macOS',
    group: 'browser',
    headers: {
      Accept: 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
      'Accept-Language': 'en-US,en;q=0.9',
      'Sec-CH-UA':
        '"Google Chrome";v="135", "Chromium";v="135", "Not.A/Brand";v="24"',
      'Sec-CH-UA-Mobile': '?0',
      'Sec-CH-UA-Platform': '"macOS"',
      'User-Agent':
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36',
    },
  },
};

const aiCodingCliProfiles = {
  'codex-cli': {
    key: 'codex-cli',
    name: 'Codex CLI',
    group: 'ai_coding_cli',
    headers: {
      'User-Agent': 'OpenAI Codex CLI/0.1',
      'X-Client-Name': 'codex-cli',
      'X-Client-Platform': 'terminal',
    },
  },
  'claude-code': {
    key: 'claude-code',
    name: 'Claude Code',
    group: 'ai_coding_cli',
    headers: {
      'User-Agent': 'Claude-Code/1.0',
      'X-Client-Name': 'claude-code',
      'X-Client-Platform': 'terminal',
    },
  },
  'gemini-cli': {
    key: 'gemini-cli',
    name: 'Gemini CLI',
    group: 'ai_coding_cli',
    headers: {
      'User-Agent': 'GeminiCLI/1.0',
      'X-Client-Name': 'gemini-cli',
      'X-Client-Platform': 'terminal',
    },
  },
  'qwen-code': {
    key: 'qwen-code',
    name: 'Qwen Code',
    group: 'ai_coding_cli',
    headers: {
      'User-Agent': 'Qwen-Code/1.0',
      'X-Client-Name': 'qwen-code',
      'X-Client-Platform': 'terminal',
    },
  },
  opencode: {
    key: 'opencode',
    name: 'OpenCode',
    group: 'ai_coding_cli',
    headers: {
      'User-Agent': 'OpenCode/1.0',
      'X-Client-Name': 'opencode',
      'X-Client-Platform': 'terminal',
    },
  },
  droid: {
    key: 'droid',
    name: 'Droid',
    group: 'ai_coding_cli',
    headers: {
      'User-Agent': 'Droid/1.0',
      'X-Client-Name': 'droid',
      'X-Client-Platform': 'terminal',
    },
  },
  amp: {
    key: 'amp',
    name: 'Amp',
    group: 'ai_coding_cli',
    headers: {
      'User-Agent': 'AmpCLI/1.0',
      'X-Client-Name': 'amp',
      'X-Client-Platform': 'terminal',
    },
  },
};

const apiSdkProfiles = {
  'postman-runtime': {
    key: 'postman-runtime',
    name: 'Postman Runtime',
    group: 'api_sdk',
    headers: {
      Accept: '*/*',
      'Cache-Control': 'no-cache',
      'Postman-Token': '00000000-0000-0000-0000-000000000000',
      'User-Agent': 'PostmanRuntime/7.43.0',
    },
  },
};

export const HEADER_PROFILE_PRESETS = {
  ...browserProfiles,
  ...aiCodingCliProfiles,
  ...apiSdkProfiles,
};

