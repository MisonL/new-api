import { syncDesktopVersionFiles } from './lib/project.mjs';

const { rawVersion, semver } = syncDesktopVersionFiles();
console.log(`synced desktop version from ${rawVersion} to ${semver}`);
