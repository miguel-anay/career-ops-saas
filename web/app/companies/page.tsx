'use client'

import { useEffect, useState, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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

interface Company {
  id: string
  name: string
  careers_url: string
  provider_id: string
  enabled: boolean
}

interface CompaniesResponse {
  companies: Company[]
}

const PROVIDERS = [
  { value: 'greenhouse', label: 'Greenhouse' },
  { value: 'ashby', label: 'Ashby' },
  { value: 'lever', label: 'Lever' },
  { value: 'recruitee', label: 'Recruitee' },
  { value: 'smartrecruiters', label: 'SmartRecruiters' },
  { value: 'workable', label: 'Workable' },
]

export default function CompaniesPage() {
  const router = useRouter()
  const [companies, setCompanies] = useState<Company[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [deleteTarget, setDeleteTarget] = useState<Company | null>(null)

  // Add form state
  const [newName, setNewName] = useState('')
  const [newCareersUrl, setNewCareersUrl] = useState('')
  const [newProviderId, setNewProviderId] = useState('')
  const [isAdding, setIsAdding] = useState(false)

  if (!isAuthenticated()) {
    router.replace('/login')
    return null
  }

  const loadCompanies = useCallback(async () => {
    setIsLoading(true)
    try {
      const data = await apiGet<CompaniesResponse>('/api/companies')
      setCompanies(data.companies)
    } catch {
      toast.error('Failed to load companies')
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadCompanies()
  }, [loadCompanies])

  const handleAddCompany = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newName.trim() || !newCareersUrl.trim() || !newProviderId) return
    setIsAdding(true)
    try {
      await apiPost('/api/companies', {
        name: newName.trim(),
        careers_url: newCareersUrl.trim(),
        provider_id: newProviderId,
      })
      setNewName('')
      setNewCareersUrl('')
      setNewProviderId('')
      toast.success(`${newName} added`)
      loadCompanies()
    } catch {
      toast.error('Failed to add company')
    } finally {
      setIsAdding(false)
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
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Watched Companies</h1>
        <Link href="/">
          <Button variant="outline">← Pipeline</Button>
        </Link>
      </div>

      {/* Add Company form */}
      <form onSubmit={handleAddCompany} className="flex gap-2 flex-wrap">
        <Input
          placeholder="Company name"
          value={newName}
          onChange={e => setNewName(e.target.value)}
          className="w-48"
        />
        <Input
          type="url"
          placeholder="Careers URL"
          value={newCareersUrl}
          onChange={e => setNewCareersUrl(e.target.value)}
          className="flex-1 min-w-[200px]"
        />
        <Select value={newProviderId} onValueChange={setNewProviderId}>
          <SelectTrigger className="w-48">
            <SelectValue placeholder="Provider" />
          </SelectTrigger>
          <SelectContent>
            {PROVIDERS.map(p => (
              <SelectItem key={p.value} value={p.value}>{p.label}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="submit"
          disabled={isAdding || !newName.trim() || !newCareersUrl.trim() || !newProviderId}
        >
          {isAdding ? 'Adding…' : 'Add Company'}
        </Button>
      </form>

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
