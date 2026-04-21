import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

const repoRoot = process.cwd();
const baselinePath = path.join(repoRoot, 'tests', 'i18nLintBaseline.json');
const isWindows = process.platform === 'win32';
const bunxCommand = isWindows ? 'bunx.cmd' : 'bunx';

function parseIssues(output) {
  const lines = output.split(/\r?\n/);
  const issues = [];
  let currentFile = '';

  for (const line of lines) {
    if (line.startsWith('src/')) {
      currentFile = line.trim();
      continue;
    }

    const match = line.match(
      /^\s*(\d+):\s+Error:\s+Found hardcoded string:\s+"([\s\S]*)"$/,
    );
    if (!match || !currentFile) {
      continue;
    }

    issues.push({
      file: currentFile,
      line: Number(match[1]),
      text: match[2],
    });
  }

  return issues.sort((a, b) => {
    if (a.file !== b.file) {
      return a.file.localeCompare(b.file);
    }
    if (a.line !== b.line) {
      return a.line - b.line;
    }
    return a.text.localeCompare(b.text);
  });
}

function issueKey(issue) {
  return `${issue.file}:${issue.line}:${issue.text}`;
}

function loadBaseline() {
  if (!fs.existsSync(baselinePath)) {
    return [];
  }
  const content = fs.readFileSync(baselinePath, 'utf8');
  return JSON.parse(content);
}

function saveBaseline(issues) {
  fs.writeFileSync(baselinePath, `${JSON.stringify(issues, null, 2)}\n`);
}

function printIssues(title, issues) {
  if (issues.length === 0) {
    return;
  }
  console.log(title);
  for (const issue of issues) {
    console.log(`- ${issue.file}:${issue.line} "${issue.text}"`);
  }
}

const args = process.argv.slice(2);
const writeBaseline = args.includes('--write-baseline');

const lintResult = spawnSync(bunxCommand, ['i18next-cli', 'lint'], {
  cwd: repoRoot,
  encoding: 'utf8',
  shell: false,
});

const combinedOutput = `${lintResult.stdout || ''}${lintResult.stderr || ''}`;
const issues = parseIssues(combinedOutput);

if (writeBaseline) {
  saveBaseline(issues);
  console.log(
    `已更新 i18n lint 基线，共记录 ${issues.length} 条历史问题：${path.relative(repoRoot, baselinePath)}`,
  );
  process.exit(0);
}

if (lintResult.status === 0 && issues.length === 0) {
  console.log('i18n lint 通过，未发现问题。');
  process.exit(0);
}

if (!combinedOutput.includes('Linter found') && issues.length === 0) {
  process.stdout.write(lintResult.stdout || '');
  process.stderr.write(lintResult.stderr || '');
  process.exit(lintResult.status ?? 1);
}

const baseline = loadBaseline();
const baselineKeys = new Set(baseline.map(issueKey));
const currentKeys = new Set(issues.map(issueKey));

const newIssues = issues.filter((issue) => !baselineKeys.has(issueKey(issue)));
const resolvedIssues = baseline.filter(
  (issue) => !currentKeys.has(issueKey(issue)),
);

console.log(
  `i18n lint 当前共 ${issues.length} 条，基线 ${baseline.length} 条，新增 ${newIssues.length} 条，已解决 ${resolvedIssues.length} 条。`,
);

printIssues('新增问题：', newIssues);
printIssues('已解决问题：', resolvedIssues);

if (newIssues.length > 0) {
  process.exit(1);
}

console.log('未发现超出基线的 i18n lint 新问题。');
process.exit(0);
