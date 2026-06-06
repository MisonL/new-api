import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const dockerfile = readFileSync(resolve(repoRoot, "Dockerfile"), "utf8");
const releaseMetadataScript = readRepoFile(
  "scripts/write-frontend-release-metadata.sh"
);

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
    ".github/workflows/docker-image-nightly.yml",
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
