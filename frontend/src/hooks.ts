import { useCallback, useEffect, useRef, useState } from 'react'

export function useResource<T>(
  loader: () => Promise<T>,
  dependencies: readonly unknown[],
) {
  const [data, setData] = useState<T | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)

  // Keep a stable ref to the latest loader function
  const loaderRef = useRef(loader)
  useEffect(() => {
    loaderRef.current = loader
  })

  // Trigger reload by incrementing counter
  const [reloadKey, setReloadKey] = useState(0)
  const reload = useCallback(() => {
    setLoading(true)
    setReloadKey((prev) => prev + 1)
  }, [])

  // Serialize dependencies to avoid unnecessary re-triggers
  const depKey = JSON.stringify(dependencies)

  useEffect(() => {
    let active = true

    queueMicrotask(async () => {
      setLoading(true)
      setError('')

      try {
        const result = await loaderRef.current()
        if (active) {
          setData(result)
          setError('')
        }
      } catch (caught: unknown) {
        if (active) {
          setError(
            caught instanceof Error ? caught.message : 'Unable to load data',
          )
        }
      } finally {
        if (active) {
          setLoading(false)
        }
      }
    })

    return () => {
      active = false
    }
  }, [depKey, reloadKey])

  return { data, error, loading, reload }
}
