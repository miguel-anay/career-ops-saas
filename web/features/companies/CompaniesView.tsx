'use client'

import { useEffect, useState, useCallback } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { apiGet, apiPost, apiDelete } from '@/lib/api'
import { isAuthenticated } from '@/lib/auth'
import type { Company, CatalogCompany, CompaniesResponse, CatalogResponse } from './types'

export function CompaniesView() {
  const [companies, setCompanies] = useState<Company[]>([])
  const [catalog, setCatalog] = useState<CatalogCompany[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [deleteTarget, setDeleteTarget] = useState<Company | null>(null)

  // Add-from-catalog state
  const [query, setQuery] = useState('')
  const [showResults, setShowResults] = useState(false)
  const [addingId, setAddingId] = useState<string | null>(null)

  const loadCompanies = useCallback(async () => {
    setIsLoading(true)
    try {
      const data = await apiGet<CompaniesResponse>('/api/companies')
      setCompanies(data.companies ?? [])
    } catch {
      toast.error('Failed to load companies')
    } finally {
      setIsLoading(false)
    }
  }, [])

  const loadCatalog = useCallback(async () => {
    try {
      const data = await apiGet<CatalogResponse>('/api/companies/catalog')
      setCatalog(data.catalog ?? [])
    } catch {
      toast.error('Failed to load company catalog')
    }
  }, [])

  useEffect(() => {
    // Preserve original behavior: no data fetch when unauthenticated (the
    // route composer handles the redirect).
    if (!isAuthenticated()) return
    loadCompanies()
    loadCatalog()
  }, [loadCompanies, loadCatalog])

  // Catalog entries the user is not already watching, filtered by the search box.
  const watchedUrls = new Set(companies.map(c => c.careers_url))
  const availableCatalog = catalog.filter(c => !watchedUrls.has(c.careers_url))
  const filteredCatalog = query.trim()
    ? availableCatalog.filter(c => c.name.toLowerCase().includes(query.trim().toLowerCase()))
    : availableCatalog

  const handleAddFromCatalog = async (entry: CatalogCompany) => {
    setAddingId(entry.id)
    try {
      await apiPost('/api/companies', { catalog_id: entry.id })
      toast.success(`${entry.name} added`)
      setQuery('')
      setShowResults(false)
      loadCompanies()
    } catch {
      toast.error(`Failed to add ${entry.name}`)
    } finally {
      setAddingId(null)
    }
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    try {
      await apiDelete(`/api/companies/${deleteTarget.id}`)
      setCompanies(prev => prev.filter(c => c.id !== deleteTarget.id))
      toast.success(`${deleteTarget.name} removed`)
      setDeleteTarget(null)
    } catch {
      toast.error('Failed to remove company')
    }
  }

  return (
    <div className="container mx-auto p-6 space-y-6">
      <h1 className="text-2xl font-bold">Watched Companies</h1>

      {/* Add company from the global catalog */}
      <div className="relative max-w-xl">
        <Input
          placeholder="Search companies to watch…"
          value={query}
          onChange={e => { setQuery(e.target.value); setShowResults(true) }}
          onFocus={() => setShowResults(true)}
          onBlur={() => setTimeout(() => setShowResults(false), 150)}
        />
        {showResults && (
          <div className="absolute z-10 mt-1 w-full max-h-80 overflow-y-auto rounded-md border bg-white shadow-md">
            {filteredCatalog.length === 0 ? (
              <div className="px-3 py-4 text-sm text-muted-foreground text-center">
                {availableCatalog.length === 0
                  ? 'All catalog companies are already watched.'
                  : 'No matching companies.'}
              </div>
            ) : (
              filteredCatalog.map(entry => (
                <button
                  key={entry.id}
                  type="button"
                  disabled={addingId === entry.id}
                  onMouseDown={e => e.preventDefault()}
                  onClick={() => handleAddFromCatalog(entry)}
                  className="flex w-full items-center justify-between px-3 py-2 text-left text-sm hover:bg-gray-50 disabled:opacity-50"
                >
                  <span className="font-medium">{entry.name}</span>
                  <span className="text-xs text-muted-foreground capitalize">
                    {addingId === entry.id ? 'Adding…' : entry.provider_id}
                  </span>
                </button>
              ))
            )}
          </div>
        )}
      </div>

      {/* Companies table */}
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Company</TableHead>
              <TableHead>Careers URL</TableHead>
              <TableHead>Provider</TableHead>
              <TableHead className="w-[100px]">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={4} className="text-center py-8 text-muted-foreground">
                  Loading…
                </TableCell>
              </TableRow>
            ) : companies.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="text-center py-8 text-muted-foreground">
                  No companies watched yet.
                </TableCell>
              </TableRow>
            ) : (
              companies.map(company => (
                <TableRow key={company.id}>
                  <TableCell className="font-medium">{company.name}</TableCell>
                  <TableCell>
                    <a
                      href={company.careers_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-blue-600 hover:underline text-sm truncate max-w-[300px] block"
                    >
                      {company.careers_url}
                    </a>
                  </TableCell>
                  <TableCell>
                    <span className="capitalize">{company.provider_id}</span>
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => setDeleteTarget(company)}
                    >
                      Remove
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {/* Confirm delete dialog */}
      <Dialog open={!!deleteTarget} onOpenChange={open => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove {deleteTarget?.name}?</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            This will remove the company from your watch list. Existing jobs from this company will not be deleted.
          </p>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDeleteConfirm}>
              Remove
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
