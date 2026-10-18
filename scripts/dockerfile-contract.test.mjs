import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const dockerfile = readFileSync(resolve(repoRoot, "Dockerfile"), "utf8");
const releaseMetadataScript = readRepoFile(
  "scripts/write-frontend-release-metadata.sh"
);
const composeFiles = ["docker-compose.yml", "deploy/compose/dev-isolated.yml"];

function readRepoFile(path) {
  return readFileSync(resolve(repoRoot, path), "utf8");
}

test("Dockerfile artifact builds require an explicit effective version", () => {
  assert.match(dockerfile, /ARG APP_VERSION=unknown/);
  assert.match(dockerfile, /ARG EFFECTIVE_APP_VERSION=\$\{APP_VERSION\}/);
  assert.match(
    dockerfile,
    /VERSION_VALUE="\$\{EFFECTIVE_APP_VERSION\}"; \\\s+if \[ -z "\$VERSION_VALUE" \] \|\| \[ "\$VERSION_VALUE" = "unknown" \]; then VERSION_VALUE="\$\{APP_VERSION\}"; fi; \\\s+if \[ -z "\$VERSION_VALUE" \] \|\| \[ "\$VERSION_VALUE" = "unknown" \]; then echo "EFFECTIVE_APP_VERSION or APP_VERSION build arg is required" >&2; exit 1; fi;/
  );
  assert.doesNotMatch(dockerfile, /VERSION_VALUE="\$\(cat VERSION\)"/);
});

test("Dockerfile image version label uses the effective artifact version", () => {
  assert.match(
    dockerfile,
    /org\.opencontainers\.image\.version="\$\{EFFECTIVE_APP_VERSION\}"/
  );
});

test("Docker build entrypoints pass EFFECTIVE_APP_VERSION explicitly", () => {
  const entrypoints = [
    ".github/workflows/docker-build.yml",
    ".github/workflows/docker-image-alpha.yml",
    "scripts/build-docker-local.sh",
  ];

  for (const entrypoint of entrypoints) {
    assert.match(
      readRepoFile(entrypoint),
      /EFFECTIVE_APP_VERSION\s*=/,
      `${entrypoint} must pass EFFECTIVE_APP_VERSION because Docker LABEL and build artifacts share the same version arg`
    );
  }
});

test("Dockerfile writes release metadata for both embedded frontends", () => {
  assert.match(
    dockerfile,
    /write-frontend-release-metadata\.sh default dist "\$VERSION_VALUE" "\$VCS_REF" "\$BUILD_DATE"/
  );
  assert.match(
    dockerfile,
    /write-frontend-release-metadata\.sh classic dist "\$VERSION_VALUE" "\$VCS_REF" "\$BUILD_DATE"/
  );
});

test("Dockerfile verifies bundled dependency versions for both frontends", () => {
  // 镜像里的前端产物不经过 repo-gates；门禁必须在构建阶段执行，否则
  // 打包了旧依赖副本的镜像可以正常发布。
  assert.match(
    dockerfile,
    /bun \/scripts\/frontend-bundle-versions\.mjs --root \/build --only web\/default/
  );
  assert.match(
    dockerfile,
    /bun \/scripts\/frontend-bundle-versions\.mjs --root \/build --only web\/classic/
  );
  assert.match(
    dockerfile,
    /COPY \.\/scripts\/frontend-bundle-versions\.mjs \/scripts\/frontend-bundle-versions\.mjs/
  );
});

test("bun version is consistent across .bun-version, Dockerfile and packageManager", () => {
  // 三处独立声明：CI 用 .bun-version 装 bun，镜像用 FROM oven/bun，前端用
  // packageManager 约束本地版本。只改其中一处时其余两处会静默停留在旧版本，
  // 表现为"本地能构建、镜像里构建失败"或反过来。
  const declared = readRepoFile(".bun-version").trim();
  assert.match(declared, /^\d+\.\d+\.\d+$/, ".bun-version 必须是精确版本号");

  const fromTags = [...dockerfile.matchAll(/FROM oven\/bun:(\d+\.\d+\.\d+)/g)].map(
    (m) => m[1]
  );
  assert.ok(fromTags.length > 0, "Dockerfile 必须使用 oven/bun 基础镜像");
  for (const tag of fromTags) {
    assert.equal(tag, declared, `Dockerfile 的 oven/bun:${tag} 与 .bun-version ${declared} 不一致`);
  }

  for (const manifest of [
    "web/default/package.json",
    "web/classic/package.json",
    "desktop/tauri-app/package.json",
  ]) {
    const pkg = JSON.parse(readRepoFile(manifest));
    assert.equal(
      pkg.packageManager,
      `bun@${declared}`,
      `${manifest} 的 packageManager 与 .bun-version ${declared} 不一致`
    );
  }
});

test("workflows install bun from .bun-version instead of a hardcoded version", () => {
  // 硬编码 bun-version 会让 workflow 与 .bun-version 各自漂移。
  for (const file of readdirSync(resolve(repoRoot, ".github/workflows"))) {
    if (!file.endsWith(".yml") && !file.endsWith(".yaml")) continue;
    const text = readRepoFile(`.github/workflows/${file}`);
    if (!text.includes("oven-sh/setup-bun")) continue;
    assert.match(
      text,
      /bun-version-file:\s*\.bun-version/,
      `.github/workflows/${file} 必须用 bun-version-file: .bun-version`
    );
    assert.doesNotMatch(
      text,
      /^\s*bun-version:\s*\d/m,
      `.github/workflows/${file} 不得硬编码 bun-version`
    );
  }
});

test("workflows pin every action to a full commit SHA", () => {
  // dtolnay/rust-toolchain 曾被认为无法钉版，实测可以：钉 master 历史内的
  // 提交（stable 分支 tip 是 master 的后代，会被 GC）。回归这条规则。
  for (const file of readdirSync(resolve(repoRoot, ".github/workflows"))) {
    if (!file.endsWith(".yml") && !file.endsWith(".yaml")) continue;
    const text = readRepoFile(`.github/workflows/${file}`);
    const uses = [...text.matchAll(/uses:\s*(\S+)/g)].map((m) => m[1]);
    for (const ref of uses) {
      if (ref.startsWith("./")) continue; // 本仓库复用步骤不经过外部 action
      assert.match(
        ref,
        /@[a-f0-9]{40}$/,
        `.github/workflows/${file} 的 action 未钉完整 SHA: ${ref}`
      );
    }
  }
});

test("every workflow job declares a timeout", () => {
  // 没有 timeout 的 job 卡住时会一直占用 runner 直到 GitHub 默认的 6 小时上限，
  // 矩阵构建（tauri 三平台、docker 双架构）会成倍放大。桌面构建实测 15-24 分钟，
  // 因此给到 90 分钟即可覆盖正常波动又能在真挂起时快速失败。
  for (const file of readdirSync(resolve(repoRoot, ".github/workflows"))) {
    if (!file.endsWith(".yml") && !file.endsWith(".yaml")) continue;
    const lines = readRepoFile(`.github/workflows/${file}`).split("\n");
    const inJobs = lines.findIndex((line) => /^jobs:$/.test(line));
    if (inJobs === -1) continue;
    let current = null;
    let hasTimeout = false;
    const flush = () => {
      if (current !== null) {
        assert.ok(
          hasTimeout,
          `.github/workflows/${file} 的 job ${current} 未声明 timeout-minutes`
        );
      }
    };
    for (const line of lines.slice(inJobs + 1)) {
      const job = line.match(/^  ([A-Za-z0-9_-]+):\s*$/);
      if (job) {
        flush();
        current = job[1];
        hasTimeout = false;
        continue;
      }
      if (/^    timeout-minutes:\s*\d+/.test(line)) hasTimeout = true;
    }
    flush();
  }
});

test("Dockerfile runs the bundle gate before writing release metadata", () => {
  // 顺序反转会让不合格的产物也写出 new-api-release.json，
  // 中止的构建留下看似有效的发布描述。
  for (const frontend of ["default", "classic"]) {
    const gate = dockerfile.indexOf(
      `bun /scripts/frontend-bundle-versions.mjs --root /build --only web/${frontend}`
    );
    const metadata = dockerfile.indexOf(
      `sh /scripts/write-frontend-release-metadata.sh ${frontend}`
    );
    assert.ok(gate !== -1, `Dockerfile must run the bundle gate for ${frontend}`);
    assert.ok(metadata !== -1, `Dockerfile must write release metadata for ${frontend}`);
    assert.ok(
      gate < metadata,
      `Dockerfile must run the bundle gate before release metadata for ${frontend}`
    );
  }
});

test("release frontend build runs the bundle gate before writing metadata", () => {
  const script = readRepoFile("scripts/build-release-frontends.sh");
  const gate = script.indexOf(
    'bun scripts/frontend-bundle-versions.mjs --only "$frontend_dir"'
  );
  const metadata = script.indexOf("sh scripts/write-frontend-release-metadata.sh");
  assert.ok(gate !== -1, "release script must run the bundle version gate");
  assert.ok(metadata !== -1, "release script must write frontend release metadata");
  assert.ok(
    gate < metadata,
    "release script must run the bundle gate before release metadata is written"
  );
});

test("desktop sidecar build runs the bundle gate for both embedded frontends", () => {
  // prepare-sidecar 直接构建并嵌入两个前端 dist，是 repo-gates/Dockerfile/
  // build-release-frontends 之外的第四个构建入口；漏掉它会绕过产物版本门禁。
  const script = readRepoFile("desktop/tauri-app/scripts/prepare-sidecar.mjs");
  assert.match(
    script,
    /scripts\/frontend-bundle-versions\.mjs/,
    "prepare-sidecar must run the bundle version gate"
  );
  assert.match(script, /`web\/\$\{name\}`/);
  const gate = script.indexOf("scripts/frontend-bundle-versions.mjs");
  const metadata = script.indexOf("write-frontend-release-metadata.sh");
  assert.ok(gate !== -1, "prepare-sidecar must run the bundle gate");
  assert.ok(metadata !== -1, "prepare-sidecar must write release metadata");
  assert.ok(
    gate < metadata,
    "prepare-sidecar must run the bundle gate before release metadata is written"
  );
});

test("tauri-build path filters include the scripts desktop builds invoke", () => {
  // tauri-build 有 paths 过滤，只监听 desktop/、web/ 等。prepare:web 与
  // prepare:sidecar 会调用 scripts 下的门禁与 metadata 脚本，改这些脚本
  // 不会触发桌面构建，等于绕过验证。
  const workflow = readRepoFile(".github/workflows/tauri-build.yml");
  for (const script of [
    "scripts/frontend-bundle-versions.mjs",
    "scripts/write-frontend-release-metadata.sh",
  ]) {
    assert.ok(
      workflow.includes(`- "${script}"`),
      `.github/workflows/tauri-build.yml 的 paths 必须包含 ${script}`
    );
  }
});

test("Dockerfile runs the container as a non-root user via the entrypoint", () => {
  // 直接写 USER 1000 会让既有部署在升级后写不了 root 属主的绑定挂载，
  // 因此改由 entrypoint 在启动时交接属主再降权。这里同时固定住三件事：
  // 镜像装了 su-exec、入口指向 entrypoint、以及入口脚本本身执行降权。
  const entrypoint = readRepoFile("deploy/docker/entrypoint.sh");

  assert.match(dockerfile, /apk add --no-cache ca-certificates tzdata su-exec/);
  assert.match(dockerfile, /COPY deploy\/docker\/entrypoint\.sh \/entrypoint\.sh/);
  assert.match(dockerfile, /ENTRYPOINT \["\/entrypoint\.sh"\]/);
  assert.doesNotMatch(
    dockerfile,
    /ENTRYPOINT \["\/new-api"\]/,
    "ENTRYPOINT 必须经过 entrypoint.sh，否则降权逻辑不会执行"
  );
  assert.match(dockerfile, /adduser -D -u 1000/, "镜像必须创建非 root 运行账户");

  assert.match(entrypoint, /exec su-exec "\$APP_UID:\$APP_GID" \/new-api/,
    "entrypoint 必须以非 root 身份启动服务");
  assert.match(entrypoint, /NEW_API_RUN_AS_ROOT/, "必须保留显式放弃降权的开关");
  // 降权不能变成静默降级：属主改不动时要留下明确原因。
  assert.match(entrypoint, /无法把 \$dir 的属主改为/);
});

test("entrypoint script is executable in git", () => {
  // 镜像里还会 chmod，但仓库内不可执行的入口脚本在本地直接运行、
  // 以及在不重建镜像的场景下都会失败。
  const mode = execFileSync("git", ["ls-files", "-s", "deploy/docker/entrypoint.sh"], {
    cwd: repoRoot,
    encoding: "utf8",
  }).trim();
  assert.match(mode, /^100755 /, "deploy/docker/entrypoint.sh 必须以可执行权限入库");
});

test("frontend release metadata script emits parseable JSON", () => {
  const distDir = mkdtempSync(resolve(tmpdir(), "new-api-release-"));
  try {
    execFileSync(
      "sh",
      [
        resolve(repoRoot, "scripts/write-frontend-release-metadata.sh"),
        "default",
        distDir,
        "1.2.3",
        "abc123",
        "2026-06-03T00:00:00Z",
      ],
      { cwd: repoRoot }
    );
    const metadata = JSON.parse(
      readFileSync(resolve(distDir, "new-api-release.json"), "utf8")
    );
    assert.deepEqual(metadata, {
      schema: 1,
      app: "new-api",
      frontend: "default",
      version: "1.2.3",
      build_commit: "abc123",
      build_date: "2026-06-03T00:00:00Z",
    });
  } finally {
    rmSync(distDir, { recursive: true, force: true });
  }
});

test("frontend release metadata script keeps full JSON string escaping", () => {
  assert.match(releaseMetadataScript, /gsub\(\/\\\\\/, "\\\\\\\\", value\)/);
  assert.match(releaseMetadataScript, /gsub\(\/"\/, "\\\\\\""/);
  assert.match(releaseMetadataScript, /gsub\(\/\\r\/, "\\\\r", value\)/);
  assert.match(releaseMetadataScript, /gsub\(\/\\t\/, "\\\\t", value\)/);
  assert.match(releaseMetadataScript, /gsub\(\/\\n\/, "\\\\n", value\)/);
});

test("Compose health checks use a direct lightweight liveness probe", () => {
  for (const composeFile of composeFiles) {
    const compose = readRepoFile(composeFile);
    assert.match(compose, /\n\s+init: true\n/);
    // The probe may be a direct argv or a shell string wrapping the same
    // command; the root compose file uses the shell form to honor the
    // configured PORT, dev-isolated the direct form. Both must point at
    // /api/healthz with a wget timeout of at least 10 seconds.
    const probe = compose.match(/test: \["CMD", "sh", "-c", "([^"]+)"\]/)
      ?? compose.match(/test: \["CMD", "((?:[^"\\]|\\.)*)"\]/);
    assert.ok(probe, `${composeFile}: no CMD healthcheck found`);
    const command = probe[1];
    assert.match(command, /wget\b/, `${composeFile}: healthcheck must use wget`);
    assert.match(command, /-T\s+([1-9][0-9]+)\b/, `${composeFile}: wget timeout must be >= 10`);
    assert.match(command, /\/api\/healthz\b/, `${composeFile}: probe must target /api/healthz`);
    assert.match(compose, /timeout: 15s/);
    assert.match(compose, /retries: 5/);
    assert.match(compose, /start_period: 60s/);
    assert.doesNotMatch(compose, /CMD-SHELL.*api\/status/);
    assert.doesNotMatch(compose, /wget[^\n]*\|\s*grep/);
  }
});
