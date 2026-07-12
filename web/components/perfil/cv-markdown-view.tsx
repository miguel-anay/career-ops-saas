// ponytail: no markdown renderer exists yet anywhere in web/ — plain <pre>
// keeps this read-only view honest about the raw content without pulling in
// a new dependency for one page.
type CvMarkdownViewProps = {
  cvMarkdown: string
}

export function CvMarkdownView({ cvMarkdown }: CvMarkdownViewProps) {
  return (
    <div className="rounded-md border p-4">
      <h2 className="mb-2 text-sm font-medium text-muted-foreground">CV</h2>
      {cvMarkdown ? (
        <pre className="whitespace-pre-wrap text-sm">{cvMarkdown}</pre>
      ) : (
        <p className="text-sm text-muted-foreground">No CV ingested yet.</p>
      )}
    </div>
  )
}
