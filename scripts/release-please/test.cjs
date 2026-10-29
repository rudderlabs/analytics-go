const assert = require('node:assert/strict');
const {test} = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const {generate} = require('./index.cjs');

function fixture({unpublished = true, pending = false} = {}) {
  const config = fs.readFileSync(path.join(__dirname, '../../release-please-config.json'), 'utf8');
  const sha = letter => letter.repeat(40);
  const state = {
    created: [], updated: [], open: [],
    commits: unpublished
      ? [['b', 'fix: repair delivery'], ['a', 'chore: release 4.5.0'], ['c', 'feat: add feature']]
      : [['b', 'fix: repair delivery']],
  };
  const contents = {
    'release-please-config.json': config,
    '.release-please-manifest.json': JSON.stringify({'.': unpublished ? '4.5.0' : '4.4.0'}),
    'go.mod': 'module github.com/rudderlabs/analytics-go/v4\n\ngo 1.21\n',
    'analytics.go': `package analytics\nconst Version = "${unpublished ? '4.5.0' : '4.4.0'}" // x-release-please-version\n`,
    'CHANGELOG.md': '# Changelog\n\n' + (unpublished ? '## [4.5.0](unpublished)\n\nObsolete candidate notes.\n\n' : '') +
      '## [4.4.0](published)\n\nPreviously published notes.\n',
  };
  function render(updates) {
    return Object.fromEntries(updates.map(update => [update.path,
      update.updater.updateContent(contents[update.path])]));
  }
  const github = {
    repository: {owner: 'rudderlabs', repo: 'analytics-go', defaultBranch: 'master'},
    async getFileContentsOnBranch(file, branch) {
      assert.equal(branch, 'master');
      assert.ok(file in contents, file);
      return {parsedContent: contents[file], content: Buffer.from(contents[file]).toString('base64'), sha: sha('f')};
    },
    async getFileJson(file, branch) { return JSON.parse((await this.getFileContentsOnBranch(file, branch)).parsedContent); },
    async *releaseIterator() { yield {tagName: 'v4.4.0', name: 'v4.4.0', sha: sha('d'), notes: 'released'}; },
    async *tagIterator() { yield {name: 'v4.4.0', sha: sha('d')}; },
    async *mergeCommitIterator() {
      for (const [letter, message] of [...state.commits,
        ['d', 'chore: release 4.4.0'], ['e', 'fix: already published']]) {
        yield {sha: sha(letter), message, files: ['analytics.go']};
      }
    },
    async *pullRequestIterator(branch, status) {
      assert.equal(branch, 'master');
      if (status === 'OPEN') yield* state.open;
      if (status === 'MERGED' && pending) yield {number: 42, labels: ['autorelease: pending'],
        body: ':robot: I have created a release *beep* *boop*\n---\n\n## [4.5.0](https://github.com/rudderlabs/analytics-go/compare/v4.4.0...v4.5.0)\n\nnotes\n\n---\n'};
    },
    async createPullRequest(pr, branch, message, updates) {
      assert.equal(branch, 'master');
      const result = {...pr, number: 43};
      state.created.push({pr: result, files: render(updates)});
      return result;
    },
    async updatePullRequest(number, pr, branch) {
      assert.equal(branch, 'master');
      const result = {...state.open[0], number, body: pr.body.toString()};
      state.updated.push({pr: result, files: render(pr.updates)});
      return result;
    },
  };
  return {github, state};
}

test('a fix after an unpublished merge creates a fresh native release PR', async () => {
  const {github, state} = fixture();
  const prs = await generate(github, '4.4.0', true);
  assert.equal(prs.length, 1);
  const {pr, files} = state.created[0];
  assert.equal(pr.title, 'chore: release 4.5.0');
  assert.ok(pr.body.includes('repair delivery'));
  assert.ok(pr.body.includes('add feature'));
  assert.ok(!pr.body.includes('already published'));
  assert.ok(pr.body.includes('v4.4.0...v4.5.0'));
  assert.equal(JSON.parse(files['.release-please-manifest.json'])['.'], '4.5.0');
  assert.ok(files['analytics.go'].includes('Version = "4.5.0"'));
  assert.equal((files['CHANGELOG.md'].match(/^## \[4\.5\.0\]/gm) || []).length, 1);
  assert.ok(files['CHANGELOG.md'].includes('Previously published notes.'));
  assert.ok(!files['CHANGELOG.md'].includes('Obsolete candidate notes.'));
});

test('an SDK fix refreshes an open release PR and keeps native version calculation', async () => {
  const {github, state} = fixture({unpublished: false});
  const [first] = await generate(github, '4.4.0', false);
  assert.equal(first.title, 'chore: release 4.4.1');
  state.open = [first];
  state.commits.unshift(['c', 'fix: repair retries']);
  await generate(github, '4.4.0', false);
  assert.equal(state.updated.length, 1);
  assert.ok(state.updated[0].pr.body.includes('repair retries'));
});

test('a base-only CI fix still updates the open release branch', async () => {
  const {github, state} = fixture({unpublished: false});
  state.open = await generate(github, '4.4.0', false);
  state.commits.unshift(['c', 'ci: repair test setup']);
  await generate(github, '4.4.0', false);
  assert.equal(state.updated.length, 1, 'always-update must rebuild even when release notes are unchanged');
});

test('generation fails if a required replacement is still blocked by a pending release', async () => {
  const {github, state} = fixture({pending: true});
  await assert.rejects(generate(github, '4.4.0', true), /did not create a replacement/);
  assert.equal(state.created.length, 0);
});

test('reject invalid published baselines before GitHub access', async () => {
  for (const baseline of ['', '4.4.0-beta.1', 'latest', '5.0.0']) {
    await assert.rejects(generate({}, baseline, true), /verified published/);
  }
});
