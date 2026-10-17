import type { ResellerProductImagesData } from '../api/types'

export const PRODUCT_IMAGE_MAX_SIZE = 500 * 1024
export const PRODUCT_IMAGE_MAX_COUNT = 5

export function validateProductImage(file: Pick<File, 'name' | 'size' | 'type'>): string {
  if (file.size <= 0 || file.size > PRODUCT_IMAGE_MAX_SIZE) return 'sizeError'
  if (!/\.(png|jpe?g|gif|webp)$/i.test(file.name) ||
      !['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(file.type)) return 'formatError'
  return ''
}

export function moveProductImage(images: string[], from: number, to: number): string[] {
  const result = [...images]
  if (from < 0 || from >= result.length || to < 0 || to >= result.length) return result
  result.splice(to, 0, result.splice(from, 1)[0]!)
  return result
}

// 不自动重试写入。读取操作编号才能确认超时的请求已保存，不能将其他窗口的写入报为成功。
export async function saveProductImagesWithRecovery(
  operationId: string,
  save: () => Promise<ResellerProductImagesData>,
  read: () => Promise<ResellerProductImagesData>,
): Promise<{ state: ResellerProductImagesData; confirmed: boolean; error?: unknown }> {
  try {
    return { state: await save(), confirmed: true }
  } catch (error) {
    const state = await read()
    return { state, confirmed: state.operation_id === operationId, error }
  }
}
