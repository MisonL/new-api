import { execFileSync } from "child_process";
import fs from "fs";
import os from "os";
import path from "path";
import { pathToFileURL } from "url";

const DEFAULT_URL = "http://localhost:3000/console";
const DEFAULT_TIMEOUT_MS = 30000;

async function importPlaywright() {
  try {
    const playwright = await import("playwright");
    return playwright.default ?? playwright;
  } catch (localError) {
    try {
      const npmRoot = execFileSync("npm", ["root", "-g"], {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      }).trim();
      const playwright = await import(
        pathToFileURL(path.join(npmRoot, "playwright", "index.js")).href
      );
      return playwright.default ?? playwright;
    } catch (globalError) {
      const error = new Error(
        "Playwright is required. Install it locally or globally before running browser-cross-verify.mjs.",
      );
      error.cause = { localError, globalError };
      throw error;
    }
  }
}

function parseArgs(argv) {
  const options = {
    url: DEFAULT_URL,
    timeoutMs: DEFAULT_TIMEOUT_MS,
    screenshotsDir: fs.mkdtempSync(
      path.join(os.tmpdir(), "new-api-browser-compat-"),
    ),
  };

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--url") {
      options.url = argv[index + 1] || options.url;
      index += 1;
      continue;
    }
    if (arg === "--timeout-ms") {
      options.timeoutMs = Number(argv[index + 1]) || options.timeoutMs;
      index += 1;
      continue;
    }
    if (arg === "--screenshots-dir") {
      options.screenshotsDir = path.resolve(
        argv[index + 1] || options.screenshotsDir,
      );
      index += 1;
      continue;
    }
    if (arg === "--help" || arg === "-h") {
      console.log(
        "Usage: node browser-cross-verify.mjs [--url URL] [--timeout-ms MS] [--screenshots-dir DIR]",
      );
      process.exit(0);
    }
  }

  fs.mkdirSync(options.screenshotsDir, { recursive: true });
  return options;
}

function isVisibleRect(rect) {
  return rect && rect.width > 0 && rect.height > 0;
}

async function firstVisibleText(page, patterns) {
  for (const pattern of patterns) {
    const locator = page.getByText(pattern).first();
    if ((await locator.count()) === 0) continue;
    const box = await locator.boundingBox().catch(() => null);
    if (isVisibleRect(box)) return locator;
  }
  return null;
}

async function verifyRoute(page, url, timeoutMs) {
  const consoleMessages = [];
  const pageErrors = [];
  const failedRequests = [];

  page.on("console", (message) => {
    if (message.type() === "error") {
      consoleMessages.push(message.text());
    }
  });
  page.on("pageerror", (error) => {
    pageErrors.push(error.message);
  });
  page.on("requestfailed", (request) => {
    if (["document", "script", "stylesheet"].includes(request.resourceType())) {
      const failure = request.failure()?.errorText || "";
      if (/aborted|cancelled|NS_BINDING_ABORTED/i.test(failure)) return;
      failedRequests.push(
        `${request.resourceType()} ${request.url()} ${failure}`.trim(),
      );
    }
  });

  await page.goto(url, { waitUntil: "domcontentloaded", timeout: timeoutMs });
  await page
    .waitForLoadState("networkidle", { timeout: 8000 })
    .catch(() => undefined);
  await page.waitForTimeout(500);

  const state = await page.evaluate(() => {
    const bodyText = document.body?.innerText || "";
    const compat = window.__NEW_API_BROWSER_COMPATIBILITY__ || null;
    const moduleScripts = Array.from(document.scripts)
      .filter((script) => script.type === "module")
      .map((script) => script.src);

    return {
      bodyLength: bodyText.trim().length,
      compat,
      hasConsoleText:
        bodyText.includes("控制台") ||
        bodyText.includes("数据看板") ||
        bodyText.includes("Console") ||
        bodyText.includes("Dashboard") ||
        bodyText.includes("登录") ||
        bodyText.includes("Sign in"),
      hasRenderError:
        bodyText.includes("页面渲染出错") ||
        bodyText.includes("Page rendering failed") ||
        bodyText.includes("Importing a module script failed"),
      moduleScripts,
      title: document.title,
    };
  });

  const homeLink = await firstVisibleText(page, [/首页|Home/]);
  let interaction = "home-link-not-found";
  if (homeLink) {
    await homeLink.click();
    await page.waitForTimeout(300);
    interaction = "home-link-clicked";
  }

  return {
    consoleMessages,
    failedRequests,
    interaction,
    pageErrors,
    state,
  };
}

async function runBrowserMatrix(playwright, options) {
  const browsers = [
    { name: "chromium", launcher: playwright.chromium },
    { name: "firefox", launcher: playwright.firefox },
    { name: "webkit", launcher: playwright.webkit },
  ];
  const viewports = [
    { name: "desktop", viewport: { width: 1440, height: 900 } },
    {
      name: "mobile",
      viewport: { width: 390, height: 844 },
      isMobile: true,
      hasTouch: true,
    },
  ];
  const results = [];

  for (const browserConfig of browsers) {
    const browser = await browserConfig.launcher.launch({ headless: true });
    try {
      for (const viewportConfig of viewports) {
        const contextOptions = {
          hasTouch: Boolean(viewportConfig.hasTouch),
          viewport: viewportConfig.viewport,
        };
        if (browserConfig.name !== "firefox") {
          contextOptions.isMobile = Boolean(viewportConfig.isMobile);
        }
        const context = await browser.newContext(contextOptions);
        const page = await context.newPage();
        const screenshotPath = path.join(
          options.screenshotsDir,
          `${browserConfig.name}-${viewportConfig.name}.png`,
        );
        let result;

        try {
          result = await verifyRoute(page, options.url, options.timeoutMs);
          await page.screenshot({ path: screenshotPath, fullPage: false });
        } finally {
          await context.close();
        }

        results.push({
          browser: browserConfig.name,
          screenshot: screenshotPath,
          viewport: viewportConfig.name,
          ...result,
        });
      }
    } finally {
      await browser.close();
    }
  }

  return results;
}

function validateResults(results) {
  const failures = [];
  const compatKeys = [
    "intersectionObserver",
    "matchMedia",
    "resizeObserver",
    "structuredClone",
  ];

  for (const result of results) {
    const prefix = `${result.browser}/${result.viewport}`;
    const allErrorMessages = [
      ...result.consoleMessages,
      ...result.pageErrors,
    ].join(" | ");
    const hasAuthProbeFailure =
      /401|Unauthorized|Not logged in|login has expired|未登录|登录已过期|\/api\/user\/self/i.test(
        allErrorMessages,
      );
    const unexpectedPageErrors = result.pageErrors.filter((message) => {
      if (/\/api\/user\/self.*access control/i.test(message)) return false;
      return true;
    });
    const unexpectedConsoleMessages = result.consoleMessages.filter(
      (message) => {
        if (
          /401|Unauthorized|Not logged in|login has expired|未登录|登录已过期/i.test(
            message,
          )
        ) {
          return false;
        }
        if (hasAuthProbeFailure && /AxiosError: Network Error/i.test(message)) {
          return false;
        }
        return true;
      },
    );

    if (!result.state.title) failures.push(`${prefix}: empty document title`);
    if (result.state.bodyLength < 20) failures.push(`${prefix}: blank body`);
    if (!result.state.hasConsoleText)
      failures.push(`${prefix}: expected app text not found`);
    if (result.state.hasRenderError)
      failures.push(`${prefix}: render error text found`);
    if (!result.state.compat) {
      failures.push(`${prefix}: browser compatibility marker not found`);
    } else {
      for (const key of compatKeys) {
        if (result.state.compat[key] !== true) {
          failures.push(`${prefix}: browser compatibility ${key} is not ready`);
        }
      }
    }
    if (unexpectedPageErrors.length) {
      failures.push(
        `${prefix}: page errors: ${unexpectedPageErrors.join(" | ")}`,
      );
    }
    if (unexpectedConsoleMessages.length) {
      failures.push(
        `${prefix}: console errors: ${unexpectedConsoleMessages.join(" | ")}`,
      );
    }
    if (result.failedRequests.length) {
      failures.push(
        `${prefix}: failed critical requests: ${result.failedRequests.join(" | ")}`,
      );
    }
  }

  return failures;
}

const options = parseArgs(process.argv.slice(2));
const playwright = await importPlaywright();
const results = await runBrowserMatrix(playwright, options);
const failures = validateResults(results);

console.log(
  JSON.stringify(
    {
      failures,
      results: results.map((result) => ({
        browser: result.browser,
        viewport: result.viewport,
        title: result.state.title,
        bodyLength: result.state.bodyLength,
        compat: result.state.compat,
        hasConsoleText: result.state.hasConsoleText,
        interaction: result.interaction,
        screenshot: result.screenshot,
      })),
      screenshotsDir: options.screenshotsDir,
      url: options.url,
    },
    null,
    2,
  ),
);

if (failures.length > 0) process.exit(1);
