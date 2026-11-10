/* eslint-disable react-hooks/refs, react-hooks/set-state-in-effect, react-hooks/exhaustive-deps */
import { useCallback, useEffect, useRef, useState } from 'react'

export function useResource<T>(loader: () => Promise<T>, dependencies: readonly unknown[]) {
  const loaderRef = useRef(loader)
  loaderRef.current = loader
  const dependencyKey = JSON.stringify(dependencies)
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try { setData(await loaderRef.current()) }
    catch (caught) { setError(caught instanceof Error ? caught.message : 'Unable to load data') }
    finally { setLoading(false) }
  }, [dependencyKey])
  useEffect(() => { void load() }, [load])
  return { data, error, loading, reload: load }
}
