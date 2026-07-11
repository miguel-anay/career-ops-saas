export function ComingSoon({ title }: { title: string }) {
  return (
    <div className="container mx-auto p-6">
      <h1 className="text-2xl font-bold">{title}</h1>
      <p className="mt-4 text-muted-foreground">
        Próximamente. Esta sección está en construcción.
      </p>
    </div>
  )
}
