import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

// 只读的 i18n 覆盖度门禁：检查代码里用到的每个翻译键是否都已存在于各语言
// 文件中。此前 CI 的做法是先跑 `i18next-cli extract`（把缺失键写进工作区），
// 再比较第一次与第二次 extract 的输出——那只能证明 extract 幂等，仓库与代码
// 漂移时永远通过（2026-11-06 实测：7 个语言各缺 11 个键，门禁仍全绿）。
//
// 这里绝不写入仓库：extract 在本仓库的输出是**扁平**结构（`nsSeparator: false`
// 且只有一个默认命名空间时，写入路径把键并到顶层，见 i18next-cli 的
// translation-manager `isTopLevel` 判定），而运行时要的是 `{"translation": …}`
// 嵌套结构（i18n.js 以 `resources: { en: enTranslation }` 加载）。把 extract
// 的输出提交进仓库会让所有翻译失效（实测：扁平资源下 t('绑定成功') 返回键本身
// 而不是译文）。因此漂移只能靠这个只读检查发现，修复时手工补语言文件。

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const webRoot = path.resolve(scriptDir, '..');
const currentWorkingDir = process.cwd();
const classicRoot = fs.existsSync(path.join(currentWorkingDir, 'i18next.config.js'))
  ? currentWorkingDir
  : path.join(webRoot, 'classic');
const localesDir = path.join(classicRoot, 'src', 'i18n', 'locales');
const isWindows = process.platform === 'win32';
const bunCommand = isWindows ? 'bun.exe' : 'bun';

const localeFiles = fs
  .readdirSync(localesDir)
  .filter((name) => name.endsWith('.json'))
  .sort();
if (localeFiles.length === 0) {
  console.error(`未在 ${localesDir} 找到语言文件，覆盖度检查无法执行。`);
  process.exit(1);
}

const configText = fs.readFileSync(
  path.join(classicRoot, 'i18next.config.js'),
  'utf8',
);
const declared = configText.match(/locales:\s*\[([^\]]*)\]/)?.[1] ?? '';
const declaredLocales = [...declared.matchAll(/['"]([^'"]+)['"]/g)].map(
  (match) => `${match[1]}.json`,
);

const failures = [];
for (const file of localeFiles) {
  const locale = file.replace(/\.json$/, '');
  const result = spawnSync(
    bunCommand,
    ['x', 'i18next-cli', 'status', locale],
    { cwd: classicRoot, encoding: 'utf8', shell: false },
  );
  const output = `${result.stdout || ''}${result.stderr || ''}`;
  const missing =
    result.status !== 0 || /\babsent\b|used in code but absent/i.test(output);
  if (missing) {
    const summary = output
      .split(/\r?\n/)
      .filter((line) => /absent|Summary/i.test(line))
      .slice(0, 3)
      .join(' / ');
    failures.push(`${locale}: ${summary || `退出码 ${result.status}`}`);
  }
}

const missingFromConfig = localeFiles.filter(
  (file) => declaredLocales.length > 0 && !declaredLocales.includes(file),
);
if (missingFromConfig.length > 0) {
  failures.push(`i18next.config.js 的 locales 未声明：${missingFromConfig.join(', ')}`);
}

if (failures.length > 0) {
  console.error('i18n 覆盖度检查失败，以下语言存在代码使用但文件缺失的键：');
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  console.error(
    '请在对应语言文件中补齐这些键。不要运行 `i18next-cli extract`：它会把语言文件改写成扁平结构，运行时将失去全部翻译。',
  );
  process.exit(1);
}

console.log(
  `i18n 覆盖度检查通过：${localeFiles.length} 个语言文件均包含代码中使用到的全部键。`,
);
process.exit(0);
