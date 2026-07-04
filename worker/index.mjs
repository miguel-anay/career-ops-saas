import dotenv from 'dotenv'
import path from 'path'
import { fileURLToPath } from 'url'
const __dirname = path.dirname(fileURLToPath(import.meta.url))
dotenv.config({ path: path.resolve(__dirname, '..', '.env') })

import express from 'express'
import { start, registerWorker } from './lib/queue.mjs'
import { handleScanCompany } from './jobs/scan.mjs'
import { handleEvaluateJob } from './jobs/evaluate.mjs'
import { handleGeneratePDF } from './jobs/pdf.mjs'
import { handleIngestCV } from './jobs/ingest-cv.mjs'
import { handleIngestEmail } from './jobs/ingest-email.mjs'

const PORT = process.env.WORKER_PORT || 3002

async function main() {
  console.log('[worker] Starting...')

  // Boot pg-boss
  await start()
  console.log('[worker] pg-boss started')

  // Register job workers
  await registerWorker('scan-company', handleScanCompany, {
    teamSize: 10,  // NFR-02: cap=10 concurrent provider calls
    teamConcurrency: 10,
  })
  console.log('[worker] Registered handler: scan-company')

  await registerWorker('evaluate-job', handleEvaluateJob, {
    teamSize: 5,
  })
  console.log('[worker] Registered handler: evaluate-job')

  await registerWorker('generate-pdf', handleGeneratePDF, {
    teamSize: 3,
  })
  console.log('[worker] Registered handler: generate-pdf')

  await registerWorker('ingest-cv', handleIngestCV, {
    teamSize: 5,
  })
  console.log('[worker] Registered handler: ingest-cv')

  await registerWorker('ingest-email', handleIngestEmail, {
    teamSize: 5,
  })
  console.log('[worker] Registered handler: ingest-email')

  // Express health endpoint
  const app = express()

  app.get('/health', (_req, res) => {
    res.status(200).json({ status: 'ok', uptime: process.uptime() })
  })

  app.listen(PORT, () => {
    console.log(`[worker] Health endpoint listening on :${PORT}`)
  })

  console.log('[worker] Ready — waiting for jobs')
}

main().catch((err) => {
  console.error('[worker] Fatal error during startup:', err)
  process.exit(1)
})

// Graceful shutdown
process.on('SIGTERM', async () => {
  console.log('[worker] SIGTERM received — shutting down gracefully')
  process.exit(0)
})

process.on('SIGINT', async () => {
  console.log('[worker] SIGINT received — shutting down gracefully')
  process.exit(0)
})
