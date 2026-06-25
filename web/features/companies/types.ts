export interface Company {
  id: string
  name: string
  careers_url: string
  provider_id: string
  enabled: boolean
}

export interface CatalogCompany {
  id: string
  name: string
  careers_url: string
  provider_id: string
  ats_api_url: string
}

export interface CompaniesResponse {
  companies: Company[]
}

export interface CatalogResponse {
  catalog: CatalogCompany[]
}
