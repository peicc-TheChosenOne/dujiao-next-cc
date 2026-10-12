import test from 'node:test'
import assert from 'node:assert/strict'
import { moveProductImage, saveProductImagesWithRecovery, validateProductImage } from '../src/utils/resellerProductImages.ts'
import type { ResellerProductImagesData } from '../src/api/types'

test('product image limits include exactly 500KiB and supported file formats', () => {
  for (const [name, type] of [['a.PNG', 'image/png'], ['a.jpg', 'image/jpeg'], ['a.jpeg', 'image/jpeg'], ['a.gif', 'image/gif'], ['a.webp', 'image/webp']]) {
    assert.equal(validateProductImage({ name: name!, type: type!, size: 512000 }), '')
  }
  assert.equal(validateProductImage({ name: 'a.png', type: 'image/png', size: 512001 }), 'sizeError')
  assert.equal(validateProductImage({ name: 'a.png', type: 'image/png', size: 0 }), 'sizeError')
  assert.equal(validateProductImage({ name: 'a.svg', type: 'image/svg+xml', size: 50 }), 'formatError')
  assert.equal(validateProductImage({ name: 'a.png', type: 'text/plain', size: 50 }), 'formatError')
})

test('moving images changes only order and the first image is the cover', () => {
  const images = ['one', 'two', 'three']
  assert.deepEqual(moveProductImage(images, 2, 0), ['three', 'one', 'two'])
  assert.deepEqual(moveProductImage(images, 0, 1), ['two', 'one', 'three'])
  assert.deepEqual(images, ['one', 'two', 'three'])
  assert.deepEqual(moveProductImage(images, 0, -1), images)
})

const state = (operationId: string, revision: number): ResellerProductImagesData => ({
  product: { id: 1, title: { 'en-US': 'Product' }, slug: 'product' },
  platform_images: ['platform'], custom_images: ['own'], effective_images: ['own'], image_source: 'custom',
  custom_detail_image: '', detail_image_source: 'platform',
  revision, operation_id: operationId,
})

test('a committed timed-out upload is confirmed by operation ID without retrying the upload', async () => {
  let writes = 0
  const result = await saveProductImagesWithRecovery('ours', async () => { writes++; throw new Error('timeout') }, async () => state('ours', 3))
  assert.equal(writes, 1)
  assert.equal(result.confirmed, true)
  assert.equal(result.state.revision, 3)
})

test('a conflicting window refreshes the state but is never marked as our saved operation', async () => {
  const result = await saveProductImagesWithRecovery('ours', async () => { throw new Error('conflict') }, async () => state('another-window', 4))
  assert.equal(result.confirmed, false)
  assert.equal(result.state.operation_id, 'another-window')
  assert.equal((result.error as Error).message, 'conflict')
})

test('an uncommitted timeout does not report success and unreadable state rejects recovery', async () => {
  const result = await saveProductImagesWithRecovery('ours', async () => { throw new Error('timeout') }, async () => state('old', 1))
  assert.equal(result.confirmed, false)
  await assert.rejects(saveProductImagesWithRecovery('ours', async () => { throw new Error('timeout') }, async () => { throw new Error('offline') }), /offline/)
})
