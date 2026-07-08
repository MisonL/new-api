import fs from 'node:fs/promises';
import path from 'node:path';
import { AUTH_FILE, CATALOG_FILE, CONFIG_FILE, ENV_KEY, RECEIPT_FILE } from './constants.js';
import { runDoctor } from './doctor.js';
import { restoreBackup } from './files.js';
import { defaultCodexHome, findWindowsCodexHomes, isWsl } from './paths.js';
import { askConfirm, askHidden, askLine, parseModelSelection } from './prompt.js';
import { refreshFromReceipt } from './refresh.js';
import { createSetupPlan, writeSetupPlan } from './setup.js';
import { discoverModels } from './models.js';

export async function runCli(argv, io) {
  const command = argv[0] && !argv[0].startsWith('-') ? argv[0] : 'setup';
  const args = command === 'setup' ? argv : argv.slice(1);
  const options = parseArgs(args);

  if (options.help || command === 'help') {
    io.stdout.write(helpText());
    return;
  }
  if (options.version || command === 'version') {
    const pkg = JSON.parse(await fs.readFile(new URL('../package.json', import.meta.url), 'utf8'));
    io.stdout.write(`${pkg.version}\n`);
    return;
  }

  if (command === 'setup') {
    await setupCommand({ io, options, dryRun: Boolean(options.dryRun) });
    return;
  }
  if (command === 'print') {
    await setupCommand({ io, options, dryRun: true });
    return;
  }
  if (command === 'doctor') {
    const codexHome = path.resolve(options.codexHome || defaultCodexHome({ env: io.env }));
    const result = await runDoctor(codexHome);
    if (result.ok) {
      io.stdout.write(`Codex bridge 配置检查通过：${codexHome}\n`);
      return;
    }
    io.stderr.write(result.problems.map((problem) => `- ${problem}`).join('\n') + '\n');
    throw new Error('doctor 检查发现配置问题');
  }
  if (command === 'refresh') {
    const codexHome = path.resolve(options.codexHome || defaultCodexHome({ env: io.env }));
    const { plan, backupId } = await refreshFromReceipt({
      codexHome,
      env: io.env,
      fetchImpl: io.fetchImpl,
    });
    io.stdout.write(`已刷新 ${plan.selectedModels.length} 个模型。备份：${backupId}\n`);
    return;
  }
  if (command === 'restore') {
    const codexHome = path.resolve(options.codexHome || defaultCodexHome({ env: io.env }));
    if (!options.backup) {
      throw new Error('restore 需要 --backup <backup-id>');
    }
    await restoreBackup(codexHome, options.backup);
    io.stdout.write(`已恢复备份 ${options.backup}\n`);
    return;
  }

  throw new Error(`未知命令：${command}`);
}

async function setupCommand({ io, options, dryRun }) {
  const interactive = shouldPrompt(io, options);
  const base = await collectSetupInput({ io, options, interactive });
  const apiKey = resolveApiKey({
    options,
    env: io.env,
    interactive,
    stdin: io.stdin,
    stdout: io.stdout,
  });
  const key = await apiKey;

  if (interactive && !options.model?.length && !options.allModels && !options.defaultModel) {
    const discovery = await discoverModels({
      baseUrl: base.baseUrl,
      apiKey: key,
      fetchImpl: io.fetchImpl,
    });
    io.stdout.write(`已发现 ${discovery.modelIds.length} 个模型。\n`);
    const selection = await askLine({
      stdin: io.stdin,
      stdout: io.stdout,
      question: '要展示的模型，多个模型用逗号分隔，或输入 all',
      defaultValue: 'all',
    });
    base.selectedModels = parseModelSelection(selection, discovery.modelIds);
    base.defaultModel = await askLine({
      stdin: io.stdin,
      stdout: io.stdout,
      question: '默认模型',
      defaultValue: base.selectedModels[0],
    });
    base.discovery = discovery;
  }

  const plan = await createSetupPlan({
    codexHome: base.codexHome,
    baseUrl: base.baseUrl,
    apiKey: key,
    authMode: base.authMode,
    envKey: base.envKey,
    defaultModel: base.defaultModel,
    selectedModels: base.selectedModels,
    fetchImpl: io.fetchImpl,
    discovery: base.discovery,
  });

  printPlanSummary(io.stdout, plan, dryRun);
  if (dryRun) {
    printGeneratedFiles(io.stdout, plan);
    return;
  }

  if (interactive && !options.yes) {
    const ok = await askConfirm({
      stdin: io.stdin,
      stdout: io.stdout,
      question: '写入这些文件',
      defaultValue: false,
    });
    if (!ok) {
      throw new Error('已取消');
    }
  } else if (!options.yes && !interactive) {
    throw new Error('非交互 setup 需要 --yes');
  }

  const backupId = await writeSetupPlan(plan);
  io.stdout.write(`已写入 Codex bridge 配置。备份：${backupId}\n`);
  io.stdout.write('请重启 Codex App，让它重新加载 model_catalog_json。\n');
}

async function collectSetupInput({ io, options, interactive }) {
  const codexHome = path.resolve(options.codexHome || await promptCodexHome(io, interactive));
  const baseUrl = options.baseUrl || await requiredPrompt(io, interactive, 'new-api 基础 URL');
  const selectedModels = options.model?.length ? options.model : undefined;
  return {
    codexHome,
    baseUrl,
    selectedModels,
    authMode: options.authMode || 'provider-token',
    envKey: options.envKey || options.apiKeyEnv,
    defaultModel: options.defaultModel,
  };
}

async function promptCodexHome(io, interactive) {
  const fallback = defaultCodexHome({ env: io.env });
  if (!interactive) {
    return fallback;
  }
  if (isWsl({ env: io.env })) {
    const windowsHomes = findWindowsCodexHomes();
    if (windowsHomes.length) {
      io.stdout.write('检测到 WSL，请确认要写入的 Codex 目标。\n');
      io.stdout.write(`1. WSL Codex：${fallback}\n`);
      io.stdout.write(`2. Windows Codex App：${windowsHomes[0]}\n`);
      const choice = await askLine({ stdin: io.stdin, stdout: io.stdout, question: '目标', defaultValue: '1' });
      if (choice === '2') {
        return windowsHomes[0];
      }
    }
  }
  return askLine({ stdin: io.stdin, stdout: io.stdout, question: 'Codex home', defaultValue: fallback });
}

async function requiredPrompt(io, interactive, label) {
  if (!interactive) {
    throw new Error(`需要 ${label}`);
  }
  const value = await askLine({ stdin: io.stdin, stdout: io.stdout, question: label });
  if (!value) {
    throw new Error(`需要 ${label}`);
  }
  return value;
}

async function resolveApiKey({ options, env, interactive, stdin, stdout }) {
  if (options.apiKeyEnv) {
    const value = env[options.apiKeyEnv];
    if (!value) {
      throw new Error(`环境变量 ${options.apiKeyEnv} 为空`);
    }
    return value;
  }
  if (options.apiKey) {
    return options.apiKey;
  }
  if (options.authMode === 'env') {
    const keyName = options.envKey || ENV_KEY;
    if (env[keyName]) {
      return env[keyName];
    }
    throw new Error(`auth-mode=env 需要环境变量 ${keyName} 有值`);
  }
  if (!interactive) {
    throw new Error('需要 API Key；可使用 --api-key-env <名称>');
  }
  const value = await askHidden({ stdin, stdout, question: 'new-api API Key' });
  if (!value) {
    throw new Error('需要 API Key');
  }
  return value;
}

function shouldPrompt(io, options) {
  return !options.yes && Boolean(io.stdin.isTTY && io.stdout.isTTY);
}

function printPlanSummary(stdout, plan, dryRun) {
  stdout.write(`${dryRun ? '预览' : '配置'} 目标：${plan.codexHome}\n`);
  stdout.write(`提供方：${plan.providerBaseUrl}\n`);
  stdout.write(`认证模式：${plan.authMode}\n`);
  stdout.write(`默认模型：${plan.defaultModel}\n`);
  stdout.write(`模型数量：${plan.selectedModels.length}\n`);
  stdout.write(`文件：${[...Object.keys(plan.files), RECEIPT_FILE].join(', ')}\n`);
}

function printGeneratedFiles(stdout, plan) {
  for (const [fileName, content] of Object.entries(plan.files)) {
    stdout.write(`\n# ${fileName}\n`);
    if (fileName === AUTH_FILE || (fileName === CONFIG_FILE && plan.sensitiveConfig)) {
      stdout.write('[已脱敏]\n');
    } else {
      stdout.write(content);
    }
  }
}

function parseArgs(args) {
  const options = { model: [] };
  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    if (!arg.startsWith('-')) {
      continue;
    }
    const separatorIndex = arg.indexOf('=');
    const name = separatorIndex >= 0 ? arg.slice(0, separatorIndex) : arg;
    const inline = separatorIndex >= 0 ? arg.slice(separatorIndex + 1) : undefined;
    const readValue = () => {
      if (inline !== undefined) {
        if (!inline) {
          throw new Error(`${name} 需要参数`);
        }
        return inline;
      }
      const value = args[++index];
      if (!value || value.startsWith('-')) {
        throw new Error(`${name} 需要参数`);
      }
      return value;
    };
    switch (name) {
      case '--help':
      case '-h':
        options.help = true;
        break;
      case '--version':
        options.version = true;
        break;
      case '--base-url':
        options.baseUrl = readValue();
        break;
      case '--api-key':
        options.apiKey = readValue();
        break;
      case '--api-key-env':
        options.apiKeyEnv = readValue();
        break;
      case '--auth-mode':
        options.authMode = readValue();
        if (!['provider-token', 'auth-json', 'env'].includes(options.authMode)) {
          throw new Error('--auth-mode 必须是 provider-token、auth-json 或 env');
        }
        break;
      case '--env-key':
        options.envKey = readValue();
        break;
      case '--codex-home':
        options.codexHome = readValue();
        break;
      case '--default-model':
        options.defaultModel = readValue();
        break;
      case '--model':
        options.model.push(...readValue().split(',').map((item) => item.trim()).filter(Boolean));
        break;
      case '--all-models':
        options.allModels = true;
        break;
      case '--yes':
      case '-y':
        options.yes = true;
        break;
      case '--dry-run':
        options.dryRun = true;
        break;
      case '--backup':
        options.backup = readValue();
        break;
      default:
        throw new Error(`未知选项：${name}`);
    }
  }
  return options;
}

function helpText() {
  return `用法：new-api-codex-bridge [命令] [选项]

命令：
  setup      配置 Codex 以使用 new-api 模型，默认命令
  print      预览将生成的 Codex 文件，不写入
  doctor     检查 Codex bridge 配置
  refresh    根据回执和 auth.json 刷新模型目录
  restore    从配置或刷新生成的备份恢复
  version    显示版本号

选项：
  --base-url <url>       new-api 基础 URL，可带或不带 /v1
  --api-key-env <name>   从环境变量读取 API Key
  --api-key <key>        直接传入 API Key，仅建议受控脚本使用
  --auth-mode <mode>     provider-token、auth-json 或 env，默认 provider-token
  --env-key <name>       auth-mode=env 时写入 provider 的环境变量名
  --codex-home <path>    目标 Codex home 目录
  --default-model <id>   默认模型 slug
  --model <id[,id]>      要包含的模型，可重复
  --all-models           包含 /v1/models 返回的所有模型
  --yes, -y              允许非交互写入
  --dry-run              只预览 setup，不写入
  --backup <id>          restore 使用的备份 id
  --version              显示版本号
  --help, -h             显示帮助
`;
}
