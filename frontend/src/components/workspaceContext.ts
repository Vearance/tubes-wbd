import { useOutletContext } from 'react-router'
import type { AccessibleDatabase } from '../api'

export function useWorkspaceContext() {
  return useOutletContext<{ databases: AccessibleDatabase[] }>()
}
