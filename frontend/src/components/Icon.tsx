import {
  ArrowDownAZ,
  ArrowUpAZ,
  Check,
  CheckCheck,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  Code2,
  Columns3,
  Copy,
  Database,
  Eye,
  EyeOff,
  GitCompareArrows,
  KeyRound,
  LayoutGrid,
  Link2,
  ListFilter,
  LogOut,
  Network,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  Save,
  ScrollText,
  Search,
  Table2,
  Trash2,
  UserRoundPlus,
  Users,
  X,
  type LucideIcon,
} from 'lucide-react'

export type IconName =
  | 'grid'
  | 'database'
  | 'table'
  | 'code'
  | 'schema'
  | 'migration'
  | 'users'
  | 'groups'
  | 'logs'
  | 'search'
  | 'filter'
  | 'sort'
  | 'sort-asc'
  | 'sort-desc'
  | 'plus'
  | 'play'
  | 'save'
  | 'logout'
  | 'chevron'
  | 'chevron-left'
  | 'chevron-down'
  | 'edit'
  | 'check'
  | 'check-check'
  | 'copy'
  | 'columns'
  | 'refresh'
  | 'close'
  | 'eye'
  | 'eye-off'
  | 'trash'
  | 'key'
  | 'link'

const icons: Record<IconName, LucideIcon> = {
  grid: LayoutGrid,
  database: Database,
  table: Table2,
  code: Code2,
  schema: Network,
  migration: GitCompareArrows,
  users: Users,
  groups: UserRoundPlus,
  logs: ScrollText,
  search: Search,
  filter: ListFilter,
  sort: ArrowDownAZ,
  'sort-asc': ArrowUpAZ,
  'sort-desc': ArrowDownAZ,
  plus: Plus,
  play: Play,
  save: Save,
  logout: LogOut,
  chevron: ChevronRight,
  'chevron-left': ChevronLeft,
  'chevron-down': ChevronDown,
  edit: Pencil,
  check: Check,
  'check-check': CheckCheck,
  copy: Copy,
  columns: Columns3,
  refresh: RefreshCw,
  close: X,
  eye: Eye,
  'eye-off': EyeOff,
  trash: Trash2,
  key: KeyRound,
  link: Link2,
}

export function Icon({
  name,
  className,
}: {
  name: IconName
  className?: string
}) {
  const LucideIconComponent = icons[name]
  if (!LucideIconComponent) return null
  return (
    <LucideIconComponent
      className={className ?? 'icon size-[18px] shrink-0'}
      aria-hidden="true"
      strokeWidth={1.75}
    />
  )
}
