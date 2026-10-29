// Use the same Release Please version as the previous pinned action. Generation
// always starts at a published version, even when an unpublished PR was merged.
const {GitHub, Manifest} = require('release-please');
const {Version} = require('release-please/build/src/version');
const {ManifestPlugin} = require('release-please/build/src/plugin');

function publishedChangelog(content, baseline) {
  const headers = [...content.matchAll(/^## \[([0-9]+\.[0-9]+\.[0-9]+)\]\(/gm)];
  const published = headers.find(header => header[1] === baseline);
  if (!published) throw new Error('Published baseline is missing from CHANGELOG.md');
  return content.slice(0, headers[0].index) + content.slice(published.index);
}

class PublishedChangelog extends ManifestPlugin {
  constructor(github, config, baseline) {
    super(github, 'master', config);
    this.baseline = baseline;
  }
  async run(candidates) {
    for (const candidate of candidates) {
      for (const update of candidate.pullRequest.updates) {
        if (update.path !== 'CHANGELOG.md') continue;
        const original = update.updater;
        update.updater = {updateContent: (content, logger) =>
          original.updateContent(publishedChangelog(content, this.baseline), logger)};
      }
    }
    return candidates;
  }
}

async function generate(github, baseline, requirePR) {
  if (!/^4\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/.test(baseline)) {
    throw new Error('A verified published Go v4 baseline is required');
  }
  const manifest = await Manifest.fromManifest(
    github, 'master', 'release-please-config.json', '.release-please-manifest.json'
  );
  manifest.releasedVersions['.'] = Version.parse(baseline);
  manifest.plugins.push(new PublishedChangelog(github, manifest.repositoryConfig, baseline));
  // This entry point cannot call createReleases or write a tag.
  const prs = (await manifest.createPullRequests()).filter(Boolean);
  if (requirePR && prs.length === 0) {
    throw new Error('Release Please did not create a replacement release PR');
  }
  return prs;
}

async function main() {
  if (process.env.GITHUB_REPOSITORY !== 'rudderlabs/analytics-go' ||
      process.env.GITHUB_REF !== 'refs/heads/master' ||
      process.env.GITHUB_EVENT_NAME !== 'push') {
    throw new Error('Release PR generation requires a trusted master push');
  }
  const github = await GitHub.create({
    owner: 'rudderlabs', repo: 'analytics-go', defaultBranch: 'master',
    token: process.env.GITHUB_TOKEN,
  });
  const prs = await generate(github, process.env.PUBLISHED_BASELINE,
    Number(process.env.SUPERSEDED_RELEASES) > 0);
  for (const pr of prs) console.log(`Created or updated release PR #${pr.number}`);
}

module.exports = {generate};
if (require.main === module) {
  main().catch(error => { console.error(error.message); process.exitCode = 1; });
}
