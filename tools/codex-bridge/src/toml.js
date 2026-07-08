export function tomlString(value) {
  return JSON.stringify(String(value));
}

export function parseTomlBasicStringContent(content) {
  let result = '';
  for (let index = 0; index < content.length; index += 1) {
    const char = content[index];
    if (char !== '\\') {
      result += char;
      continue;
    }
    index += 1;
    if (index >= content.length) {
      throw new Error('TOML 字符串转义不完整');
    }
    const escaped = content[index];
    if (escaped === 'b') {
      result += '\b';
    } else if (escaped === 't') {
      result += '\t';
    } else if (escaped === 'n') {
      result += '\n';
    } else if (escaped === 'f') {
      result += '\f';
    } else if (escaped === 'r') {
      result += '\r';
    } else if (escaped === '"') {
      result += '"';
    } else if (escaped === '\\') {
      result += '\\';
    } else if (escaped === 'u' || escaped === 'U') {
      const width = escaped === 'u' ? 4 : 8;
      const hex = content.slice(index + 1, index + 1 + width);
      if (!new RegExp(`^[0-9A-Fa-f]{${width}}$`).test(hex)) {
        throw new Error('TOML Unicode 转义无效');
      }
      result += String.fromCodePoint(Number.parseInt(hex, 16));
      index += width;
    } else {
      throw new Error(`不支持的 TOML 字符串转义：\\${escaped}`);
    }
  }
  return result;
}

export function stripManagedBlock(text, start, end) {
  let result = text;
  for (;;) {
    const lines = result.split(/\r?\n/);
    const block = findManagedBlock(lines, start, end);
    const startIndex = block.startIndex;
    if (startIndex < 0) {
      return result;
    }
    const endIndex = block.endIndex;
    const before = lines.slice(0, startIndex);
    const after = lines.slice(endIndex + 1);
    if (before.at(-1) === '' && after[0] === '') {
      after.shift();
    }
    result = [...before, ...after].join('\n');
  }
}

export function extractManagedBlock(text, start, end) {
  const lines = text.split(/\r?\n/);
  const block = findManagedBlock(lines, start, end);
  const startIndex = block.startIndex;
  if (startIndex < 0) {
    return '';
  }
  const endIndex = block.endIndex;
  return lines.slice(startIndex + 1, endIndex).join('\n').trim();
}

function findManagedBlock(lines, start, end) {
  const state = createTomlScanState();
  let startIndex = -1;
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (startIndex < 0 && isMarkerLine(state, line, start)) {
      startIndex = index;
      advanceTomlScanState(line, state);
      continue;
    }
    if (startIndex >= 0 && isMarkerLine(state, line, end)) {
      return { startIndex, endIndex: index };
    }
    advanceTomlScanState(line, state);
  }
  if (startIndex >= 0) {
    throw new Error(`已有受管理块不完整：${start}`);
  }
  return { startIndex: -1, endIndex: -1 };
}

function isMarkerLine(state, line, marker) {
  return isHeaderScanPoint(state) && line.trim() === marker;
}

export function removeTopLevelAssignments(text, keys) {
  const lines = text.split(/\r?\n/);
  const state = createTomlScanState();
  let inTable = false;
  let skippingAssignment = false;
  const result = [];
  for (const line of lines) {
    const trimmed = line.trim();
    const header = isHeaderScanPoint(state) ? parseTomlHeader(trimmed) : null;

    if (header) {
      inTable = true;
      skippingAssignment = false;
    }

    if (skippingAssignment) {
      advanceTomlScanState(line, state);
      skippingAssignment = !isHeaderScanPoint(state);
      continue;
    }

    if (inTable || !trimmed || trimmed.startsWith('#') || !isHeaderScanPoint(state)) {
      result.push(line);
      advanceTomlScanState(line, state);
      continue;
    }

    const assignmentKey = parseTomlAssignmentKey(trimmed);
    if (assignmentKey && keys.includes(assignmentKey)) {
      advanceTomlScanState(line, state);
      skippingAssignment = !isHeaderScanPoint(state);
      continue;
    }

    result.push(line);
    advanceTomlScanState(line, state);
  }
  return result.join('\n');
}

export function removeTableBlock(text, tableName) {
  const lines = text.split(/\r?\n/);
  const result = [];
  let skipping = false;
  const state = createTomlScanState();
  const targetTableName = canonicalTomlPath(tableName);
  for (const line of lines) {
    const trimmed = line.trim();
    const header = isHeaderScanPoint(state) ? parseTomlHeader(trimmed) : null;
    if (header) {
      const isTarget = tomlPathMatchesOrDescends(header.path, targetTableName);
      if (skipping && isTarget) {
        advanceTomlScanState(line, state);
        continue;
      }
      if (skipping && !isTarget) {
        skipping = false;
      }
      if (isTarget) {
        skipping = true;
        advanceTomlScanState(line, state);
        continue;
      }
    }
    if (!skipping) {
      result.push(line);
    }
    advanceTomlScanState(line, state);
  }
  return result.join('\n');
}

function parseTomlHeader(trimmed) {
  const header = parseBracketedTomlHeader(trimmed);
  if (!header) {
    return null;
  }
  const path = parseTomlPathSegments(header.pathText);
  const name = path.length ? path.join('.') : '';
  if (!name) {
    throw new Error('TOML 表头名称不能为空');
  }
  return { name, path, isArray: header.isArray };
}

function parseBracketedTomlHeader(trimmed) {
  if (!trimmed.startsWith('[')) {
    return null;
  }
  const isArray = trimmed.startsWith('[[');
  const startIndex = isArray ? 2 : 1;
  const endIndex = findTomlHeaderEnd(trimmed, startIndex, isArray);
  if (endIndex < 0) {
    return null;
  }
  const rest = trimmed.slice(endIndex + (isArray ? 2 : 1)).trim();
  if (rest && !rest.startsWith('#')) {
    return null;
  }
  return {
    isArray,
    pathText: trimmed.slice(startIndex, endIndex),
  };
}

function findTomlHeaderEnd(text, startIndex, isArray) {
  let quote = '';
  let escaped = false;
  for (let index = startIndex; index < text.length; index += 1) {
    const char = text[index];
    if (quote === '"') {
      if (escaped) {
        escaped = false;
      } else if (char === '\\') {
        escaped = true;
      } else if (char === '"') {
        quote = '';
      }
      continue;
    }
    if (quote === "'") {
      if (char === "'") {
        quote = '';
      }
      continue;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (isArray && char === ']' && text[index + 1] === ']') {
      return index;
    }
    if (!isArray && char === ']') {
      return index;
    }
  }
  return -1;
}

function parseTomlAssignmentKey(trimmed) {
  const equalIndex = findTopLevelEqual(trimmed);
  if (equalIndex < 0) {
    return '';
  }
  return canonicalTomlPath(trimmed.slice(0, equalIndex));
}

function findTopLevelEqual(text) {
  let quote = '';
  let escaped = false;
  for (let index = 0; index < text.length; index += 1) {
    const char = text[index];
    if (quote === '"') {
      if (escaped) {
        escaped = false;
      } else if (char === '\\') {
        escaped = true;
      } else if (char === '"') {
        quote = '';
      }
      continue;
    }
    if (quote === "'") {
      if (char === "'") {
        quote = '';
      }
      continue;
    }
    if (char === '#') {
      return -1;
    }
    if (char === '"' || char === "'") {
      quote = char;
      continue;
    }
    if (char === '=') {
      return index;
    }
  }
  return -1;
}

function canonicalTomlPath(rawPath) {
  const segments = parseTomlPathSegments(rawPath);
  return segments.length ? segments.join('.') : '';
}

function tomlPathMatchesOrDescends(path, rawTargetPath) {
  const target = parseTomlPathSegments(rawTargetPath);
  if (!target.length || path.length < target.length) {
    return false;
  }
  return target.every((segment, index) => path[index] === segment);
}

function parseTomlPathSegments(rawPath) {
  const segments = [];
  const text = String(rawPath || '');
  let index = 0;
  for (;;) {
    index = skipSpaces(text, index);
    if (index >= text.length) {
      break;
    }
    let segment = '';
    if (text[index] === '"') {
      const endIndex = findBasicStringEnd(text, index + 1);
      if (endIndex < 0) {
        return [];
      }
      segment = parseTomlBasicStringContent(text.slice(index + 1, endIndex));
      index = endIndex + 1;
    } else if (text[index] === "'") {
      const endIndex = text.indexOf("'", index + 1);
      if (endIndex < 0) {
        return [];
      }
      segment = text.slice(index + 1, endIndex);
      index = endIndex + 1;
    } else {
      const start = index;
      while (index < text.length && text[index] !== '.' && !/\s/.test(text[index])) {
        index += 1;
      }
      segment = text.slice(start, index);
    }
    segment = segment.trim();
    if (!segment) {
      return [];
    }
    segments.push(segment);
    index = skipSpaces(text, index);
    if (index >= text.length) {
      break;
    }
    if (text[index] !== '.') {
      return [];
    }
    index += 1;
  }
  return segments;
}

function skipSpaces(text, index) {
  while (index < text.length && /\s/.test(text[index])) {
    index += 1;
  }
  return index;
}

function createTomlScanState() {
  return {
    multilineQuote: null,
    bracketDepth: 0,
    braceDepth: 0,
  };
}

function isHeaderScanPoint(state) {
  return !state.multilineQuote && state.bracketDepth === 0 && state.braceDepth === 0;
}

function advanceTomlScanState(line, state) {
  if (!state) {
    return;
  }

  let index = 0;
  while (index < line.length) {
    if (state.multilineQuote) {
      const endIndex = findMultilineStringEnd(line, index, state.multilineQuote);
      if (endIndex < 0) {
        return;
      }
      state.multilineQuote = null;
      index = endIndex + 3;
      continue;
    }

    const char = line[index];
    if (char === '#') {
      return;
    }
    if (line.startsWith('"""', index)) {
      state.multilineQuote = '"""';
      index += 3;
      continue;
    }
    if (line.startsWith("'''", index)) {
      state.multilineQuote = "'''";
      index += 3;
      continue;
    }
    if (char === '"') {
      const endIndex = findBasicStringEnd(line, index + 1);
      if (endIndex < 0) {
        return;
      }
      index = endIndex + 1;
      continue;
    }
    if (char === "'") {
      const endIndex = line.indexOf("'", index + 1);
      if (endIndex < 0) {
        return;
      }
      index = endIndex + 1;
      continue;
    }
    updateCollectionDepth(char, state);
    index += 1;
  }
}

function updateCollectionDepth(char, state) {
  if (char === '[') {
    state.bracketDepth += 1;
    return;
  }
  if (char === ']' && state.bracketDepth > 0) {
    state.bracketDepth -= 1;
    return;
  }
  if (char === '{') {
    state.braceDepth += 1;
    return;
  }
  if (char === '}' && state.braceDepth > 0) {
    state.braceDepth -= 1;
  }
}

function findBasicStringEnd(line, startIndex) {
  let escaped = false;
  for (let index = startIndex; index < line.length; index += 1) {
    const char = line[index];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (char === '\\') {
      escaped = true;
      continue;
    }
    if (char === '"') {
      return index;
    }
  }
  return -1;
}

function findMultilineStringEnd(line, startIndex, delimiter) {
  if (delimiter === "'''") {
    return line.indexOf(delimiter, startIndex);
  }

  let escaped = false;
  for (let index = startIndex; index < line.length; index += 1) {
    const char = line[index];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (char === '\\') {
      escaped = true;
      continue;
    }
    if (line.startsWith(delimiter, index)) {
      return index;
    }
  }
  return -1;
}
