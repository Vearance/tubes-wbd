import {
  ArrowDownAZ,
  Check,
  ChevronRight,
  Code2,
  Columns3,
  Database,
  GitCompareArrows,
  LayoutGrid,
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
  UserRoundPlus,
  Users,
  X,
  type LucideIcon,
} from 'lucide-react'

type IconName =
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
  | 'plus'
  | 'play'
  | 'save'
  | 'logout'
  | 'chevron'
  | 'edit'
  | 'check'
  | 'columns'
  | 'refresh'
  | 'close'

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
  plus: Plus,
  play: Play,
  save: Save,
  logout: LogOut,
  chevron: ChevronRight,
  edit: Pencil,
  check: Check,
  columns: Columns3,
  refresh: RefreshCw,
  close: X,
}

export function Icon({ name }: { name: IconName }) {
  const LucideIconComponent = icons[name]
  return <LucideIconComponent className="icon" aria-hidden="true" strokeWidth={1.75} />
}
