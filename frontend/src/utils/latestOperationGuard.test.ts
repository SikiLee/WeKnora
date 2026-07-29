import assert from 'node:assert/strict';
import test from 'node:test';

import { createLatestOperationGuard } from './latestOperationGuard';

test('only the latest feedback operation may commit its response', () => {
  const guard = createLatestOperationGuard();
  const slowLike = guard.begin();
  const fastDislike = guard.begin();

  assert.equal(guard.isCurrent(slowLike), false);
  assert.equal(guard.isCurrent(fastDislike), true);
});

test('message changes invalidate all pending feedback responses', () => {
  const guard = createLatestOperationGuard();
  const pending = guard.begin();
  guard.invalidate();
  assert.equal(guard.isCurrent(pending), false);
});
