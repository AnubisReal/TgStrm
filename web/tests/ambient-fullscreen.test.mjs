import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const source = await readFile(new URL('../src/features/ambient/fullscreen.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2020 } });
const { ownsFullscreen, supportsFullscreen, exitOwnedFullscreen } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('iOS without Fullscreen API can mount and dismiss safely', async () => {
  for (const doc of [{}, { fullscreenElement: null }]) {
    assert.equal(ownsFullscreen(doc, null), false);
    assert.equal(supportsFullscreen(doc, null), false);
    await assert.doesNotReject(exitOwnedFullscreen(doc, null));
    await assert.doesNotReject(exitOwnedFullscreen(doc, {}));
  }
});

test('does not exit an unrelated fullscreen element', async () => {
  let exits = 0;
  const owned = {};
  const doc = { fullscreenElement: {}, exitFullscreen: async () => { exits++; } };
  await exitOwnedFullscreen(doc, owned);
  assert.equal(exits, 0);
});

test('supported fullscreen exits only its own panel', async () => {
  let exits = 0;
  const panel = { requestFullscreen() {} };
  const doc = { fullscreenEnabled: true, fullscreenElement: panel, exitFullscreen: async () => { exits++; } };
  assert.equal(supportsFullscreen(doc, panel), true);
  await exitOwnedFullscreen(doc, panel);
  assert.equal(exits, 1);
});

test('partial Fullscreen API is not offered as a control', async () => {
  const panel = { requestFullscreen() {} };
  const doc = { fullscreenEnabled: true, fullscreenElement: panel };
  assert.equal(supportsFullscreen(doc, panel), false);
  await assert.doesNotReject(exitOwnedFullscreen(doc, panel));
});
