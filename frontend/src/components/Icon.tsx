import {
  AlertTriangle,
  ArrowDownAZ,
  ArrowUpAZ,
  Check,
  CheckCheck,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronsUpDown,
  Clock,
  Code2,
  Columns3,
  Copy,
  Crown,
  Database,
  Eye,
  EyeOff,
  GitCompareArrows,
  KeyRound,
  LayoutGrid,
  Link2,
  ListFilter,
  Lock,
  LockOpen,
  LogOut,
  Network,
  Pencil,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  RotateCw,
  Save,
  ScrollText,
  Search,
  Send,
  ShieldCheck,
  Sparkles,
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
  | 'chevrons-up-down'
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
  | 'shield'
  | 'lock'
  | 'unlock'
  | 'crown'
  | 'undo'
  | 'redo'
  | 'clock'
  | 'alert'
  | 'sparkles'
  | 'send'

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
  'chevrons-up-down': ChevronsUpDown,
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
  shield: ShieldCheck,
  lock: Lock,
  unlock: LockOpen,
  crown: Crown,
  undo: RotateCcw,
  redo: RotateCw,
  clock: Clock,
  alert: AlertTriangle,
  sparkles: Sparkles,
  send: Send,
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

export function DatabaseMark({
  connected = false,
  className,
}: {
  connected?: boolean
  className?: string
}) {
  return (
    <span
      className={
        className ??
        'relative grid size-[38px] shrink-0 place-items-center rounded-xl bg-violet-soft text-violet shadow-xs'
      }
      aria-hidden="true"
    >
      <Database className="size-4.5" strokeWidth={1.8} />
      {connected && (
        <span className="absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full border-2 border-white bg-success" />
      )}
    </span>
  )
}
