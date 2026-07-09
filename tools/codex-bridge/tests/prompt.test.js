import test from 'node:test';
import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { askHidden } from '../src/prompt.js';

test('askHidden restores raw mode and paused stdin state', async () => {
  const stdin = new EventEmitter();
  stdin.isTTY = true;
  stdin.isRaw = false;
  stdin.readableFlowing = null;
  const rawModes = [];
  let paused = false;
  stdin.setRawMode = (value) => {
    stdin.isRaw = value;
    rawModes.push(value);
  };
  stdin.resume = () => {
    stdin.readableFlowing = true;
  };
  stdin.pause = () => {
    paused = true;
    stdin.readableFlowing = false;
  };

  const output = [];
  const promise = askHidden({
    stdin,
    stdout: { write: (chunk) => output.push(String(chunk)) },
    question: 'key',
  });
  stdin.emit('data', Buffer.from('secret\n'));

  assert.equal(await promise, 'secret');
  assert.deepEqual(rawModes, [true, false]);
  assert.equal(paused, true);
  assert.equal(stdin.readableFlowing, false);
  assert.equal(output.join(''), 'key: \n');
});

test('askHidden ignores unhandled control characters in TTY input', async () => {
  const stdin = new EventEmitter();
  stdin.isTTY = true;
  stdin.isRaw = false;
  stdin.readableFlowing = null;
  stdin.setRawMode = (value) => {
    stdin.isRaw = value;
  };
  stdin.resume = () => {
    stdin.readableFlowing = true;
  };
  stdin.pause = () => {
    stdin.readableFlowing = false;
  };

  const promise = askHidden({
    stdin,
    stdout: { write: () => {} },
    question: 'key',
  });
  stdin.emit('data', Buffer.from('abc\u001b[Zdef\n'));

  assert.equal(await promise, 'abcdef');
});

test('askHidden consumes non-CSI escape sequences without leaking following bytes', async () => {
  const stdin = new EventEmitter();
  stdin.isTTY = true;
  stdin.isRaw = false;
  stdin.readableFlowing = null;
  stdin.setRawMode = (value) => {
    stdin.isRaw = value;
  };
  stdin.resume = () => {
    stdin.readableFlowing = true;
  };
  stdin.pause = () => {
    stdin.readableFlowing = false;
  };

  const promise = askHidden({
    stdin,
    stdout: { write: () => {} },
    question: 'key',
  });
  stdin.emit('data', Buffer.from('abc\u001bbd\u001bOQef\n'));

  assert.equal(await promise, 'abcdef');
});

test('askHidden rejects TTYs that cannot hide input', async () => {
  const stdin = new EventEmitter();
  stdin.isTTY = true;
  const output = [];

  await assert.rejects(
    () =>
      askHidden({
        stdin,
        stdout: { write: (chunk) => output.push(String(chunk)) },
        question: 'key',
      }),
    /不支持隐藏输入/,
  );
  assert.equal(output.join(''), 'key: \n');
});
