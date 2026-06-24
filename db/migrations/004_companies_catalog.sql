-- 004_companies_catalog.sql
-- Global, install-wide catalog of known companies users can pick from.
-- REFERENCE data, not tenant data: no user_id, no RLS. The runtime role
-- (app_user) gets read-only SELECT; the catalog is seeded/maintained by the
-- migration owner. Users select entries here to populate their own
-- watched_companies (per-tenant, RLS-protected).
--
-- The 001 grant (GRANT ... ON ALL TABLES) only covered tables existing at
-- that time, so app_user needs an explicit grant on this new table.

CREATE TABLE IF NOT EXISTS companies_catalog (
  id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
  name        text        NOT NULL,
  careers_url text        NOT NULL UNIQUE,
  provider_id text        NOT NULL,
  ats_api_url text,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_companies_catalog_name ON companies_catalog(name);

GRANT SELECT ON companies_catalog TO app_user;

-- Seed: companies ported from career-ops (original CLI project) portals.yml
-- `tracked_companies`, filtered to entries whose careers_url resolves to a
-- provider this SaaS actually scrapes (Greenhouse, Ashby, Lever, Workable —
-- see api/internal/companies/service.go DetectProvider). Companies that relied
-- on generic websearch over their own domain are excluded: ATS-only scanner.
INSERT INTO companies_catalog (name, careers_url, provider_id, ats_api_url)
VALUES
  -- Greenhouse
  ('Anthropic', 'https://job-boards.greenhouse.io/anthropic', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/anthropic/jobs'),
  ('PolyAI', 'https://job-boards.eu.greenhouse.io/polyai', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/polyai/jobs'),
  ('Parloa', 'https://job-boards.eu.greenhouse.io/parloa', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/parloa/jobs'),
  ('Intercom', 'https://job-boards.greenhouse.io/intercom', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/intercom/jobs'),
  ('Hume AI', 'https://job-boards.greenhouse.io/humeai', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/humeai/jobs'),
  ('Airtable', 'https://job-boards.greenhouse.io/airtable', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/airtable/jobs'),
  ('Vercel', 'https://job-boards.greenhouse.io/vercel', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/vercel/jobs'),
  ('Temporal', 'https://job-boards.greenhouse.io/temporal', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/temporal/jobs'),
  ('Arize AI', 'https://job-boards.greenhouse.io/arizeai', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/arizeai/jobs'),
  ('RunPod', 'https://job-boards.greenhouse.io/runpod', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/runpod/jobs'),
  ('Glean', 'https://job-boards.greenhouse.io/gleanwork', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/gleanwork/jobs'),
  ('Ada', 'https://job-boards.greenhouse.io/ada', 'greenhouse', NULL),
  ('Later', 'https://job-boards.greenhouse.io/later', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/later/jobs'),
  ('Safari AI', 'https://job-boards.greenhouse.io/safariai', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/safariai/jobs'),
  ('Hootsuite', 'https://job-boards.greenhouse.io/hootsuite', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/hootsuite/jobs'),
  ('Factorial', 'https://job-boards.greenhouse.io/factorial', 'greenhouse', NULL),
  ('Black Forest Labs', 'https://job-boards.greenhouse.io/blackforestlabs', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/blackforestlabs/jobs'),
  ('Helsing', 'https://job-boards.greenhouse.io/helsing', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/helsing/jobs'),
  ('Celonis', 'https://job-boards.greenhouse.io/celonis', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/celonis/jobs'),
  ('Contentful', 'https://job-boards.greenhouse.io/contentful', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/contentful/jobs'),
  ('GetYourGuide', 'https://job-boards.greenhouse.io/getyourguide', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/getyourguide/jobs'),
  ('HelloFresh', 'https://job-boards.greenhouse.io/hellofresh', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/hellofresh/jobs'),
  ('N26', 'https://job-boards.greenhouse.io/n26', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/n26/jobs'),
  ('Trade Republic', 'https://job-boards.greenhouse.io/traderepublicbank', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/traderepublicbank/jobs'),
  ('SumUp', 'https://job-boards.greenhouse.io/sumup', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/sumup/jobs'),
  ('Scandit', 'https://job-boards.greenhouse.io/scandit', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/scandit/jobs'),
  ('Wayve', 'https://job-boards.greenhouse.io/wayve', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/wayve/jobs'),
  ('Isomorphic Labs', 'https://job-boards.greenhouse.io/isomorphiclabs', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/isomorphiclabs/jobs'),
  ('PhysicsX', 'https://job-boards.greenhouse.io/physicsx', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/physicsx/jobs'),
  ('Stability AI', 'https://job-boards.greenhouse.io/stabilityai', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/stabilityai/jobs'),
  ('Amplemarket', 'https://job-boards.greenhouse.io/amplemarket', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/amplemarket/jobs'),
  ('Runway', 'https://job-boards.greenhouse.io/runwayml', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/runwayml/jobs'),
  ('Hightouch', 'https://job-boards.greenhouse.io/hightouch', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/hightouch/jobs'),
  ('PlanetScale', 'https://job-boards.greenhouse.io/planetscale', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/planetscale/jobs'),
  ('Speechmatics', 'https://job-boards.greenhouse.io/speechmatics', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/speechmatics/jobs'),
  ('Boomi', 'https://job-boards.greenhouse.io/boomilp', 'greenhouse', 'https://boards-api.greenhouse.io/v1/boards/boomilp/jobs'),

  -- Ashby
  ('ElevenLabs', 'https://jobs.ashbyhq.com/elevenlabs', 'ashby', NULL),
  ('Deepgram', 'https://jobs.ashbyhq.com/deepgram', 'ashby', NULL),
  ('Vapi', 'https://jobs.ashbyhq.com/vapi', 'ashby', NULL),
  ('Bland AI', 'https://jobs.ashbyhq.com/bland', 'ashby', NULL),
  ('Sierra', 'https://jobs.ashbyhq.com/sierra', 'ashby', NULL),
  ('Decagon', 'https://jobs.ashbyhq.com/decagon', 'ashby', NULL),
  ('Lindy', 'https://jobs.ashbyhq.com/lindy', 'ashby', NULL),
  ('Cohere', 'https://jobs.ashbyhq.com/cohere', 'ashby', NULL),
  ('LangChain', 'https://jobs.ashbyhq.com/langchain', 'ashby', NULL),
  ('Pinecone', 'https://jobs.ashbyhq.com/pinecone', 'ashby', NULL),
  ('Klue', 'https://jobs.ashbyhq.com/klue', 'ashby', NULL),
  ('Glacis AI', 'https://jobs.ashbyhq.com/glacis-ai', 'ashby', NULL),
  ('Attio', 'https://jobs.ashbyhq.com/attio', 'ashby', NULL),
  ('Tinybird', 'https://jobs.ashbyhq.com/tinybird', 'ashby', NULL),
  ('Travelperk', 'https://jobs.ashbyhq.com/travelperk', 'ashby', NULL),
  ('Aleph Alpha', 'https://jobs.ashbyhq.com/AlephAlpha', 'ashby', NULL),
  ('DeepL', 'https://jobs.ashbyhq.com/DeepL', 'ashby', NULL),
  ('Lakera', 'https://jobs.ashbyhq.com/lakera.ai', 'ashby', NULL),
  ('Cradle', 'https://jobs.ashbyhq.com/cradlebio', 'ashby', NULL),
  ('Photoroom', 'https://jobs.ashbyhq.com/photoroom', 'ashby', NULL),
  ('Synthesia', 'https://jobs.ashbyhq.com/synthesia', 'ashby', NULL),
  ('Faculty', 'https://jobs.ashbyhq.com/faculty', 'ashby', NULL),
  ('Causaly', 'https://jobs.ashbyhq.com/causaly', 'ashby', NULL),
  ('Lovable', 'https://jobs.ashbyhq.com/lovable', 'ashby', NULL),
  ('Legora', 'https://jobs.ashbyhq.com/legora', 'ashby', NULL),
  ('Perplexity', 'https://jobs.ashbyhq.com/perplexity', 'ashby', NULL),
  ('Clay Labs', 'https://jobs.ashbyhq.com/claylabs', 'ashby', NULL),
  ('WorkOS', 'https://jobs.ashbyhq.com/workos', 'ashby', NULL),
  ('Supabase', 'https://jobs.ashbyhq.com/supabase', 'ashby', NULL),
  ('Resend', 'https://jobs.ashbyhq.com/resend', 'ashby', NULL),
  ('Clerk', 'https://jobs.ashbyhq.com/clerk', 'ashby', NULL),
  ('Inngest', 'https://jobs.ashbyhq.com/inngest', 'ashby', NULL),
  ('n8n', 'https://jobs.ashbyhq.com/n8n', 'ashby', NULL),
  ('Zapier', 'https://jobs.ashbyhq.com/zapier', 'ashby', NULL),

  -- Lever
  ('Mistral AI', 'https://jobs.lever.co/mistral', 'lever', NULL),
  ('Weights & Biases', 'https://jobs.lever.co/wandb', 'lever', NULL),
  ('Palantir', 'https://jobs.lever.co/palantir', 'lever', NULL),
  ('Sanctuary AI', 'https://jobs.lever.co/sanctuary', 'lever', NULL),
  ('Qonto', 'https://jobs.lever.co/qonto', 'lever', NULL),
  ('Forto', 'https://jobs.lever.co/forto', 'lever', NULL),
  ('Pigment', 'https://jobs.lever.co/pigment', 'lever', NULL),
  ('Spotify', 'https://jobs.lever.co/spotify', 'lever', NULL),
  ('Vinted', 'https://jobs.lever.co/vinted', 'lever', NULL),
  ('Clarity AI', 'https://jobs.lever.co/clarity-ai', 'lever', NULL),

  -- Workable
  ('Hugging Face', 'https://apply.workable.com/huggingface/', 'workable', NULL),
  ('Semios', 'https://apply.workable.com/semios/', 'workable', NULL)
ON CONFLICT (careers_url) DO NOTHING;
