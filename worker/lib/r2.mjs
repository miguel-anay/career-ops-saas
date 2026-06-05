import { S3Client, PutObjectCommand } from '@aws-sdk/client-s3'
import 'dotenv/config'

const s3 = new S3Client({
  region: 'auto',
  endpoint: process.env.R2_ENDPOINT,
  credentials: {
    accessKeyId: process.env.R2_ACCESS_KEY_ID,
    secretAccessKey: process.env.R2_SECRET_ACCESS_KEY,
  },
})

const BUCKET = process.env.R2_BUCKET

/**
 * Upload a buffer to Cloudflare R2 (S3-compatible).
 *
 * @param {string} key - Object key (path within the bucket)
 * @param {Buffer} buffer - File content as a Buffer
 * @param {string} mimeType - MIME type (e.g., 'application/pdf')
 * @returns {Promise<{key: string, etag: string}>}
 */
export async function uploadBuffer(key, buffer, mimeType) {
  const command = new PutObjectCommand({
    Bucket: BUCKET,
    Key: key,
    Body: buffer,
    ContentType: mimeType,
  })

  const result = await s3.send(command)

  return {
    key,
    etag: result.ETag || '',
  }
}
