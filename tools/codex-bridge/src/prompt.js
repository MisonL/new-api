import readline from 'node:readline/promises';

export async function askLine({ stdin, stdout, question, defaultValue }) {
  const suffix = defaultValue ? ` (${defaultValue})` : '';
  const rl = readline.createInterface({ input: stdin, output: stdout });
  try {
    const answer = (await rl.question(`${question}${suffix}: `)).trim();
    return answer || defaultValue || '';
  } finally {
    rl.close();
  }
}

export async function askConfirm({ stdin, stdout, question, defaultValue = false }) {
  const hint = defaultValue ? 'Y/n' : 'y/N';
  const answer = (await askLine({ stdin, stdout, question: `${question} (${hint})` })).toLowerCase();
  if (!answer) {
    return defaultValue;
  }
  return answer === 'y' || answer === 'yes';
}

export async function askHidden({ stdin, stdout, question }) {
  stdout.write(`${question}: `);
  if (stdin.isTTY && typeof stdin.setRawMode !== 'function') {
    stdout.write('\n');
    throw new Error('当前终端不支持隐藏输入，请使用 --api-key-env');
  }
  if (!stdin.isTTY) {
    const rl = readline.createInterface({ input: stdin, output: undefined });
    try {
      const value = await rl.question('');
      stdout.write('\n');
      return value.trim();
    } finally {
      rl.close();
    }
  }

  const previousRawMode = Boolean(stdin.isRaw);
  const previousFlowing = stdin.readableFlowing;
  stdin.setRawMode(true);
  stdin.resume();
  return new Promise((resolve, reject) => {
    let value = '';
    let settled = false;
    const cleanup = () => {
      stdin.off('data', onData);
      stdin.off('close', onClose);
      stdin.off('end', onEnd);
      stdin.off('error', onError);
      if (typeof stdin.setRawMode === 'function') {
        stdin.setRawMode(previousRawMode);
      }
      if (previousFlowing !== true && typeof stdin.pause === 'function') {
        stdin.pause();
      }
      stdout.write('\n');
    };
    const finish = (callback, result) => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      callback(result);
    };
    const onData = (buffer) => {
      const text = buffer.toString('utf8');
      for (let index = 0; index < text.length; index += 1) {
        const char = text[index];
        if (char === '\u0003') {
          finish(reject, new Error('已取消'));
          return;
        }
        if (char === '\u001b') {
          index = skipEscapeSequence(text, index);
          continue;
        }
        if (char === '\r' || char === '\n') {
          finish(resolve, value.trim());
          return;
        }
        if (char === '\u007f' || char === '\b') {
          value = value.slice(0, -1);
          continue;
        }
        if (isControlCharacter(char)) {
          continue;
        }
        value += char;
      }
    };
    const onClose = () => finish(reject, new Error('输入流已关闭'));
    const onEnd = () => finish(reject, new Error('输入流已结束'));
    const onError = (error) => finish(reject, error);
    stdin.on('data', onData);
    stdin.on('close', onClose);
    stdin.on('end', onEnd);
    stdin.on('error', onError);
  });
}

function isControlCharacter(char) {
  const codePoint = char.codePointAt(0);
  return codePoint !== undefined && codePoint < 0x20;
}

function skipEscapeSequence(text, startIndex) {
  const next = text[startIndex + 1];
  if (!next) {
    return startIndex;
  }
  if (next === 'O') {
    return Math.min(startIndex + 2, text.length - 1);
  }
  if (next !== '[') {
    return Math.min(startIndex + 1, text.length - 1);
  }
  for (let index = startIndex + 2; index < text.length; index += 1) {
    const codePoint = text.codePointAt(index);
    if (codePoint !== undefined && codePoint >= 0x40 && codePoint <= 0x7e) {
      return index;
    }
  }
  return text.length - 1;
}

export function parseModelSelection(input, availableModels) {
  const raw = String(input || '').trim();
  if (!raw || raw.toLowerCase() === 'all') {
    return availableModels;
  }
  const selected = [...new Set(raw.split(',').map((item) => item.trim()).filter(Boolean))];
  const missing = selected.filter((model) => !availableModels.includes(model));
  if (missing.length) {
    throw new Error(`所选模型不在 new-api 返回列表中：${missing.join(', ')}`);
  }
  return selected;
}
