'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import {
  LayoutDashboard,
  Briefcase,
  Building2,
  FileText,
  User,
  Newspaper,
  Settings,
  type LucideIcon,
} from 'lucide-react'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuBadge,
} from '@/components/ui/sidebar'

type NavItem = {
  title: string
  href: string
  icon: LucideIcon
  soon?: boolean
}

type NavSection = {
  label: string
  items: NavItem[]
}

// Add a future feature (Training, LinkedIn assistant, …) by dropping a line
// here and creating its route under app/(app)/. Nothing else to touch.
const NAV: NavSection[] = [
  {
    label: 'Pipeline',
    items: [
      { title: 'Tracker', href: '/', icon: LayoutDashboard },
      { title: 'Jobs', href: '/jobs', icon: Briefcase },
      { title: 'Compañías', href: '/companies', icon: Building2 },
    ],
  },
  {
    label: 'Mi Perfil',
    items: [
      { title: 'CV', href: '/cv/ingest', icon: FileText },
      { title: 'Perfil', href: '/perfil', icon: User },
      { title: 'Article Digest', href: '/article-digest', icon: Newspaper },
    ],
  },
  {
    label: 'Configuración',
    items: [
      { title: 'Gmail', href: '/configuracion', icon: Settings },
    ],
  },
]

export function AppSidebar() {
  const pathname = usePathname()

  return (
    <Sidebar>
      <SidebarHeader className="px-4 py-3">
        <span className="text-lg font-bold">Career Ops</span>
      </SidebarHeader>
      <SidebarContent>
        {NAV.map(section => (
          <SidebarGroup key={section.label}>
            <SidebarGroupLabel>{section.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {section.items.map(item => (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                      render={<Link href={item.href} />}
                      isActive={pathname === item.href}
                    >
                      <item.icon />
                      <span>{item.title}</span>
                    </SidebarMenuButton>
                    {item.soon && <SidebarMenuBadge>Pronto</SidebarMenuBadge>}
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>
    </Sidebar>
  )
}
