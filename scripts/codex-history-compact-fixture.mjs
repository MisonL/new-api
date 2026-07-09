import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import readline from 'node:readline';

const homeDir = process.env.HOME || os.homedir();
if (!path.isAbsolute(homeDir)) {
  throw new Error('HOME must resolve to an absolute path');
}
const defaultRoot = path.join(homeDir, '.codex', 'sessions');
const sessionsRoot = path.resolve(process.env.CODEX_HISTORY_ROOT || defaultRoot);
const relativeSessionsRoot = path.relative(homeDir, sessionsRoot);
if (relativeSessionsRoot.startsWith('..') || path.isAbsolute(relativeSessionsRoot)) {
  throw new Error('CODEX_HISTORY_ROOT must stay under HOME');
}
const outputPath = process.argv[2] || '';
const minItems = Number.parseInt(process.env.CODEX_HISTORY_MIN_ITEMS || '8', 10);
const maxItems = Number.parseInt(process.env.CODEX_HISTORY_MAX_ITEMS || '12', 10);
const maxFiles = Number.parseInt(process.env.CODEX_HISTORY_MAX_FILES || '400', 10);

function walk(dir, out = []) {
	if (!fs.existsSync(dir)) return out;
	let entries = [];
	try {
		entries = fs.readdirSync(dir, { withFileTypes: true });
	} catch {
		return out;
	}
	for (const entry of entries) {
		const file = path.join(dir, entry.name);
		if (entry.isSymbolicLink()) continue;
		if (entry.isDirectory()) walk(file, out);
		else if (entry.isFile() && file.endsWith('.jsonl')) out.push(file);
	}
	return out;
}

function textLength(value) {
  if (typeof value === 'string') return value.length;
  if (Array.isArray(value)) return value.reduce((sum, item) => sum + textLength(item?.text || item?.content || ''), 0);
  return 0;
}

function redactedMessage(item, index) {
  const content = Array.isArray(item.content) ? item.content : [];
  const len = textLength(content);
  const digest = crypto
    .createHash('sha256')
    .update(JSON.stringify(content))
    .digest('hex')
    .slice(0, 16);
  return {
    type: 'message',
    role: item.role === 'assistant' ? 'assistant' : 'user',
    content: [
      {
        type: item.role === 'assistant' ? 'output_text' : 'input_text',
        text: `codex history item ${index + 1}: role=${item.role || 'unknown'} text_chars=${len} sha256=${digest}`,
      },
    ],
  };
}

async function findFixture() {
  const files = walk(sessionsRoot).sort().slice(-maxFiles).reverse();
  for (const file of files) {
    let lineNo = 0;
    const rl = readline.createInterface({ input: fs.createReadStream(file), crlfDelay: Infinity });
    try {
      for await (const line of rl) {
        lineNo += 1;
        if (!line.includes('"type":"compacted"')) continue;
        let event;
        try {
          event = JSON.parse(line);
        } catch {
          continue;
        }
        const history = event.payload?.replacement_history;
        if (!Array.isArray(history) || history.length < minItems) continue;
        const messages = history
          .filter((item) => item?.type === 'message' && (item.role === 'user' || item.role === 'assistant'))
          .slice(0, maxItems)
          .map(redactedMessage);
        if (messages.length < minItems) continue;
        return {
          source: { file: path.relative(sessionsRoot, file), line: lineNo, history_items: history.length },
          model: 'gpt-5.5-openai-compact',
          followup_model: 'gpt-5.5',
          input: messages,
          followup: {
            type: 'message',
            role: 'user',
            content: [{ type: 'input_text', text: 'continue after real codex history compact fixture' }],
          },
        };
      }
    } finally {
      rl.close();
    }
  }
  throw new Error(`no compacted Codex history fixture found under ${sessionsRoot}`);
}

const fixture = await findFixture();
const body = JSON.stringify(fixture, null, 2);
if (outputPath) {
  fs.writeFileSync(outputPath, `${body}\n`, { mode: 0o600 });
} else {
  console.log(body);
}
