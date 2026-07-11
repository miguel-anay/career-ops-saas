import { EmailIngestButtons } from '@/features/email-ingest/EmailIngestButtons'

export default function ConfiguracionPage() {
  return (
    <div className="container mx-auto p-6 space-y-6">
      <h1 className="text-2xl font-bold">Configuración</h1>
      <section className="space-y-3">
        <div>
          <h2 className="font-semibold">Gmail</h2>
          <p className="text-sm text-muted-foreground">
            Conectá tu Gmail para importar alertas de empleo y sincronizá para traer nuevos jobs.
          </p>
        </div>
        <EmailIngestButtons />
      </section>
    </div>
  )
}
