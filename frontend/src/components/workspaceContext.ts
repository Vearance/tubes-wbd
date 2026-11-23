import { useOutletContext } from 'react-router'
import type { AccessibleDatabase, MigrationExecution } from '../api'

export type WorkspaceContextValue = {
  databases: AccessibleDatabase[]
  pendingApprovals: MigrationExecution[]
  reloadPendingApprovals: () => void
}

export function useWorkspaceContext() {
  return useOutletContext<WorkspaceContextValue>()
}
